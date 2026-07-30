// Command agentshark runs the AgentsharkX management-plane BFF.
package main

import (
	"context"
	"log/slog"
	"net/http"
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
	aggregator.SetOperational(auditService)
	sessions := auth.New(cfg.AdminToken.Value(), auth.Options{CookieSecure: cfg.CookieSecure, TTL: 8 * time.Hour})
	apiHandler := api.New(api.ServerConfig{
		Sessions: sessions, Aggregate: aggregator, Connect: connectService, Trust: trustService, Protect: protectService,
		Audit: auditService, Traces: traceService, Stream: hub, Logger: logger, AuthEnabled: !cfg.AuthDisabled,
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
