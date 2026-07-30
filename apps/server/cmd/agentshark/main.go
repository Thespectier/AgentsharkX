// Command agentshark runs the AgentsharkX management-plane BFF.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/aggregate"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/api"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/audit"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/auth"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/config"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/connect"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/demo"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/gateway"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/guard"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/protect"
	storagepostgres "github.com/Thespectier/AgentsharkX/apps/server/internal/storage/postgres"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
	tracequery "github.com/Thespectier/AgentsharkX/apps/server/internal/trace"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/trust"
	webconsole "github.com/Thespectier/AgentsharkX/apps/server/internal/web"
)

var (
	version  = "development"
	revision = "unknown"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		logger.Error("configuration rejected", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("configuration loaded", "version", version, "revision", revision, "summary", cfg.SafeSummary())
	rootContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	databaseStore, err := storagepostgres.Open(rootContext, cfg.Database.URL.Value(), storagepostgres.Options{
		MaxConnections: int32(cfg.Database.MaxConnections), MinConnections: int32(cfg.Database.MinConnections),
		ConnectTimeout: cfg.Database.ConnectTimeout, EventRetention: cfg.Database.EventRetention,
		TraceRetention: cfg.Database.TraceRetention, PayloadRetention: cfg.Database.PayloadRetention,
		OutboxRetention: cfg.Database.OutboxRetention,
	})
	if err != nil {
		logger.Error("database configuration rejected", "error", err.Error())
		os.Exit(1)
	}
	defer databaseStore.Close()

	gatewayHTTP := &http.Client{Timeout: cfg.UpstreamTimeout}
	guardHTTP := &http.Client{Timeout: cfg.UpstreamTimeout}
	guardOperationHTTP := &http.Client{}
	gatewayClient, err := gateway.New(cfg.Gateway.BaseURL, gatewayHTTP, cfg.UpstreamRetryMax)
	if err != nil {
		logger.Error("gateway adapter rejected", "error", err.Error())
		os.Exit(1)
	}
	guardClient, err := guard.NewWithOperationClient(
		cfg.Guard.BaseURL, cfg.Guard.AdminToken.Value(), cfg.GuardRelease,
		guardHTTP, guardOperationHTTP, cfg.UpstreamRetryMax,
	)
	if err != nil {
		logger.Error("guard adapter rejected", "error", err.Error())
		os.Exit(1)
	}
	protectGuardClient, err := guard.NewWithOperationClient(
		cfg.Guard.BaseURL, cfg.Guard.AdminToken.Value(), cfg.GuardRelease,
		guardHTTP, &http.Client{Timeout: cfg.UpstreamTimeout}, cfg.UpstreamRetryMax,
	)
	if err != nil {
		logger.Error("protect adapter rejected", "error", err.Error())
		os.Exit(1)
	}

	aggregator := aggregate.New(cfg.Environment, gatewayClient, guardClient)
	connectService := connect.New(gatewayClient, cfg.Gateway.ConsoleURL)
	trustService := trust.New(rootContext, guardClient, cfg.ScanTimeout)
	consoleLinks := connectService.Links()
	consoleLinks.AgentGuardConsole = strings.TrimRight(cfg.Guard.ConsoleURL, "/")
	hub := stream.NewHub()
	auditService := audit.NewPersistent(gatewayClient, guardClient, hub, databaseStore, consoleLinks)
	traceService := tracequery.New(databaseStore)
	protectService := protect.New(gatewayClient, protectGuardClient, consoleLinks, auditService)
	var demoRunner demo.Runner
	var demoGatewayLogs demo.GatewayLogReader
	if cfg.Demo.Enabled {
		runnerClient, err := demo.NewRunnerClient(cfg.Demo.RunnerURL, cfg.Demo.RunnerToken.Value(), &http.Client{Timeout: cfg.UpstreamTimeout})
		if err != nil {
			logger.Error("demo runner configuration rejected", "error", err.Error())
			os.Exit(1)
		}
		demoRunner = runnerClient
		demoGatewayClient, err := gateway.New(cfg.Demo.GatewayAdminURL, &http.Client{Timeout: cfg.UpstreamTimeout}, cfg.UpstreamRetryMax)
		if err != nil {
			logger.Error("demo gateway adapter rejected", "error", err.Error())
			os.Exit(1)
		}
		demoGatewayLogs = demoGatewayClient
	}
	demoService := demo.New(databaseStore, databaseStore, demoRunner, protectService, databaseStore, hub, demo.Options{
		Enabled: cfg.Demo.Enabled, DefaultDelayMS: cfg.Demo.DefaultDelayMS,
		MaxConcurrency: cfg.Demo.MaxConcurrency, RunTimeout: cfg.Demo.RunTimeout,
		MonitorInterval: cfg.Demo.MonitorInterval,
		RunnerLostAfter: cfg.UpstreamTimeout + 2*cfg.Demo.MonitorInterval,
		GatewayLogs:     demoGatewayLogs, GatewayConsoleURL: cfg.Demo.GatewayConsoleURL,
		Probes: []demo.ComponentProbe{
			{ID: "collector", Label: "Trace Collector", Required: true, Remediation: "Start agentshark-collector and verify its database migrations.", Check: httpStatusProbe(cfg.Demo.CollectorURL)},
			{ID: "agentgateway-demo-route", Label: "agentgateway Demo route", Required: true, Remediation: "Apply the namespaced Demo listener and verify its OpenAI-compatible model route.", Check: demoModelProbe(cfg.Demo.LLMBaseURL, cfg.Demo.LLMModel)},
			{ID: "agentguard", Label: "AgentGuard", Required: true, Remediation: "Start AgentGuard and verify its management health endpoint.", Check: func(ctx context.Context) error {
				health := guardClient.Health(ctx)
				if health.Status != model.HealthHealthy {
					return errors.New("AgentGuard is not healthy")
				}
				return nil
			}},
			{ID: "llm-fixture", Label: "Deterministic LLM fixture", Required: true, Remediation: "Start demo-fixtures and verify its health endpoint.", Check: httpStatusProbe(fixtureHealthURL(cfg.Demo.MCPURL))},
			{ID: "mcp-fixture", Label: "Deterministic MCP fixture", Required: true, Remediation: "Start demo-fixtures and verify its health endpoint.", Check: httpStatusProbe(fixtureHealthURL(cfg.Demo.MCPURL))},
		},
	})
	aggregator.SetOperational(auditService)
	sessions := auth.New(cfg.AdminToken.Value(), auth.Options{CookieSecure: cfg.CookieSecure, TTL: 8 * time.Hour})
	apiHandler := api.New(api.ServerConfig{
		Sessions: sessions, Aggregate: aggregator, Connect: connectService, Trust: trustService, Protect: protectService,
		Audit: auditService, Traces: traceService, Demo: demoService, Stream: hub, Logger: logger, AuthEnabled: !cfg.AuthDisabled,
	})
	handler := webconsole.New(apiHandler)

	health := aggregator.Refresh(rootContext)
	persistenceReady := initializePersistence(rootContext, databaseStore, auditService, health, logger, cfg.Database.AutoMigrate)
	go monitorHealth(rootContext, aggregator, auditService, logger, cfg.PollInterval)
	if persistenceReady {
		go monitorAudit(rootContext, auditService, cfg.PollInterval)
	} else {
		go recoverPersistence(rootContext, databaseStore, auditService, aggregator, logger, cfg.PollInterval, cfg.Database.AutoMigrate)
	}
	go maintainPersistence(rootContext, databaseStore, logger)
	go demoService.Monitor(rootContext)

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	go func() {
		<-rootContext.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownContext); err != nil {
			logger.Error("server shutdown failed", "error", err.Error())
		}
	}()

	logger.Info("AgentsharkX BFF listening", "address", cfg.ListenAddr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped unexpectedly", "error", err.Error())
		os.Exit(1)
	}
}

func httpStatusProbe(rawURL string) func(context.Context) error {
	return func(ctx context.Context) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return errors.New("readiness endpoint is invalid")
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return errors.New("readiness endpoint is unavailable")
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("readiness endpoint returned status %d", response.StatusCode)
		}
		return nil
	}
}

func demoModelProbe(baseURL, expectedModel string) func(context.Context) error {
	endpoint := strings.TrimRight(baseURL, "/") + "/models"
	return func(ctx context.Context) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return errors.New("Demo model endpoint is invalid")
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return errors.New("Demo model endpoint is unavailable")
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			return fmt.Errorf("Demo model endpoint returned status %d", response.StatusCode)
		}
		var payload struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&payload); err != nil {
			return errors.New("Demo model endpoint returned incompatible JSON")
		}
		for _, item := range payload.Data {
			if item.ID == expectedModel {
				return nil
			}
		}
		return errors.New("deterministic Demo model was not advertised")
	}
}

func fixtureHealthURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parsed.Path = "/healthz"
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func monitorAudit(ctx context.Context, service *audit.Service, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			service.Refresh(ctx)
		}
	}
}

type healthEventRecorder interface {
	RecordHealth(context.Context, model.SourceHealth) error
}

type healthPersistenceFailure struct {
	source model.Source
	err    error
}

type healthPersistenceState struct {
	persisted map[model.Source]model.SourceHealth
	pending   map[model.Source]model.SourceHealth
}

func newHealthPersistenceState(initial []model.SourceHealth) *healthPersistenceState {
	state := &healthPersistenceState{
		persisted: make(map[model.Source]model.SourceHealth, len(initial)),
		pending:   make(map[model.Source]model.SourceHealth),
	}
	for _, health := range initial {
		state.persisted[health.Source] = health
	}
	return state
}

func (state *healthPersistenceState) persistChanges(ctx context.Context, recorder healthEventRecorder, current []model.SourceHealth) []healthPersistenceFailure {
	failures := make([]healthPersistenceFailure, 0)
	for _, health := range current {
		candidate, queued := state.pending[health.Source]
		if !queued {
			previous, exists := state.persisted[health.Source]
			if exists && sameHealthState(previous, health) {
				continue
			}
			candidate = health
		}
		if err := recorder.RecordHealth(ctx, candidate); err != nil {
			state.pending[health.Source] = candidate
			failures = append(failures, healthPersistenceFailure{source: health.Source, err: err})
			continue
		}
		state.persisted[health.Source] = candidate
		delete(state.pending, health.Source)
		if sameHealthState(candidate, health) {
			continue
		}
		if err := recorder.RecordHealth(ctx, health); err != nil {
			state.pending[health.Source] = health
			failures = append(failures, healthPersistenceFailure{source: health.Source, err: err})
			continue
		}
		state.persisted[health.Source] = health
	}
	return failures
}

func sameHealthState(left, right model.SourceHealth) bool {
	return left.Status == right.Status && left.Version == right.Version
}

func monitorHealth(ctx context.Context, aggregator *aggregate.Service, auditService *audit.Service, logger *slog.Logger, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	state := newHealthPersistenceState(aggregator.Snapshot())
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := aggregator.Refresh(ctx)
			for _, failure := range state.persistChanges(ctx, auditService, current) {
				logger.Warn("health persistence unavailable", "source", failure.source, "error", failure.err.Error())
			}
		}
	}
}

func initializePersistence(ctx context.Context, store *storagepostgres.Store, auditService *audit.Service, health []model.SourceHealth, logger *slog.Logger, autoMigrate bool) bool {
	migrationContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if autoMigrate {
		if err := store.Migrate(migrationContext); err != nil {
			logger.Error("database migration unavailable", "error", err.Error())
			return false
		}
	} else if err := store.Ready(migrationContext); err != nil {
		logger.Error("database schema unavailable", "error", err.Error())
		return false
	}
	if err := auditService.Restore(migrationContext); err != nil {
		logger.Error("audit restore unavailable", "error", err.Error())
		return false
	}
	for _, sourceHealth := range health {
		if err := auditService.RecordHealth(migrationContext, sourceHealth); err != nil {
			logger.Error("health persistence unavailable", "source", sourceHealth.Source, "error", err.Error())
			return false
		}
	}
	auditService.Refresh(migrationContext)
	return true
}

func recoverPersistence(ctx context.Context, store *storagepostgres.Store, auditService *audit.Service, aggregator *aggregate.Service, logger *slog.Logger, pollInterval time.Duration, autoMigrate bool) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if initializePersistence(ctx, store, auditService, aggregator.Snapshot(), logger, autoMigrate) {
			logger.Info("persistent Audit storage recovered")
			monitorAudit(ctx, auditService, pollInterval)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func maintainPersistence(ctx context.Context, store *storagepostgres.Store, logger *slog.Logger) {
	if err := store.PruneAudit(ctx, time.Now().UTC()); err != nil {
		logger.Warn("database retention cleanup unavailable", "error", err.Error())
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := store.PruneAudit(ctx, now.UTC()); err != nil {
				logger.Warn("database retention cleanup unavailable", "error", err.Error())
			}
		}
	}
}
