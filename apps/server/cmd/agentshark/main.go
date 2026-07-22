// Command agentshark runs the AgentsharkX management-plane BFF.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/aggregate"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/api"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/auth"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/config"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/connect"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/gateway"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/guard"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/trust"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		logger.Error("configuration rejected", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("configuration loaded", "summary", cfg.SafeSummary())

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

	rootContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	aggregator := aggregate.New(cfg.Environment, gatewayClient, guardClient)
	connectService := connect.New(gatewayClient, cfg.Gateway.ConsoleURL)
	trustService := trust.New(rootContext, guardClient, cfg.ScanTimeout)
	hub := stream.NewHub()
	sessions := auth.New(cfg.AdminToken.Value(), auth.Options{CookieSecure: cfg.CookieSecure, TTL: 8 * time.Hour})
	handler := api.New(api.ServerConfig{
		Sessions: sessions, Aggregate: aggregator, Connect: connectService, Trust: trustService,
		Stream: hub, Logger: logger, AuthEnabled: !cfg.AuthDisabled,
	})

	aggregator.Refresh(rootContext)
	go monitorHealth(rootContext, aggregator, hub, cfg.PollInterval)

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

func monitorHealth(ctx context.Context, aggregator *aggregate.Service, hub *stream.Hub, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	previous := aggregator.Snapshot()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := aggregator.Refresh(ctx)
			for index, health := range current {
				if index >= len(previous) || health.Status != previous[index].Status || health.Version != previous[index].Version {
					hub.Publish(newHealthEvent(health))
				}
			}
			previous = current
		}
	}
}

func newHealthEvent(health model.SourceHealth) model.UnifiedEvent {
	severity := "info"
	if health.Status == model.HealthDown {
		severity = "high"
	} else if health.Status != model.HealthHealthy {
		severity = "medium"
	}
	id := string(health.Source) + "-health-" + health.CheckedAt.Format("20060102T150405.000000000Z")
	return model.UnifiedEvent{
		ID: id, Timestamp: health.CheckedAt, Source: health.Source, Kind: "health", Severity: severity,
		Summary: health.Label + " is " + string(health.Status), RawRef: model.RawRef{Source: health.Source, ID: id},
	}
}
