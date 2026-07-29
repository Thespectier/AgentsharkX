// Command agentshark-collector receives bounded OTLP/HTTP protobuf Trace batches.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	storagepostgres "github.com/Thespectier/AgentsharkX/apps/server/internal/storage/postgres"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry/normalize"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry/receiver"
)

var (
	version  = "development"
	revision = "unknown"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config, err := loadCollectorConfig(os.LookupEnv)
	if err != nil {
		logger.Error("collector configuration rejected", "error", err.Error())
		os.Exit(1)
	}
	rootContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := runCollector(rootContext, config, logger); err != nil {
		logger.Error("collector stopped", "error", err.Error())
		os.Exit(1)
	}
}

func runCollector(ctx context.Context, config collectorConfig, logger *slog.Logger) error {
	databaseStore, err := storagepostgres.Open(ctx, config.databaseURL, storagepostgres.Options{
		MaxConnections: int32(config.databaseMaxConns), MinConnections: int32(config.databaseMinConns),
		ConnectTimeout: config.databaseConnect, PayloadRetention: config.payloadRetention,
		TraceRetention: config.traceRetention,
	})
	if err != nil {
		return errors.New("open collector database")
	}
	defer databaseStore.Close()

	metrics := &receiver.Metrics{}
	traceHandler, err := receiver.New(databaseStore, receiver.Options{
		IngestToken: config.ingestToken, MaxCompressedBytes: config.maxCompressedBytes,
		MaxDecompressedBytes: config.maxDecompressedBytes, RequestTimeout: config.requestTimeout,
		MaxSpansPerRequest: config.maxSpansPerRequest,
		Normalize: normalize.Options{
			ContentMode: config.contentMode, PayloadLimit: config.payloadLimitBytes,
			PayloadRetention: config.payloadRetention, TraceRetention: config.traceRetention,
		},
		Metrics: metrics, Logger: logger,
	})
	if err != nil {
		return errors.New("initialize trace receiver")
	}
	handler := newCollectorHandler(traceHandler, metrics, databaseStore, config.requestTimeout)
	go maintainTracePersistence(ctx, databaseStore, logger)

	httpServer := &http.Server{
		Addr: config.listenAddress, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: config.requestTimeout,
		WriteTimeout: config.requestTimeout + time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownContext); err != nil {
			logger.Error("collector shutdown failed")
		}
	}()

	logger.Info("AgentsharkX Trace Collector listening",
		"version", version, "revision", revision, "address", config.listenAddress,
		"content_mode", config.contentMode, "compressed_limit_bytes", config.maxCompressedBytes,
		"decompressed_limit_bytes", config.maxDecompressedBytes,
		"max_spans_per_request", config.maxSpansPerRequest,
	)
	err = httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errors.New("serve collector HTTP")
	}
	snapshot := metrics.Snapshot()
	logger.Info("collector ingest totals", "requests", snapshot.Requests, "request_rejections", snapshot.RequestRejections,
		"spans_received", snapshot.SpansReceived, "spans_rejected", snapshot.SpansRejected,
		"spans_duplicated", snapshot.SpansDuplicated, "write_failures", snapshot.WriteFailures,
		"write_latency_total_ms", snapshot.WriteLatencyTotal.Milliseconds(),
	)
	return nil
}

type collectorReadiness interface {
	Ready(context.Context) error
}

type traceMaintainer interface {
	PruneTraces(context.Context, time.Time) error
}

func maintainTracePersistence(ctx context.Context, store traceMaintainer, logger *slog.Logger) {
	if err := store.PruneTraces(ctx, time.Now().UTC()); err != nil {
		logger.Warn("trace retention cleanup unavailable", "error", err.Error())
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := store.PruneTraces(ctx, now.UTC()); err != nil {
				logger.Warn("trace retention cleanup unavailable", "error", err.Error())
			}
		}
	}
}

func newCollectorHandler(traceHandler http.Handler, metrics *receiver.Metrics, readiness collectorReadiness, timeout time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/v1/traces", traceHandler)
	mux.Handle("/metrics", metrics.PrometheusHandler())
	mux.HandleFunc("/healthz", func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			response.Header().Set("Allow", http.MethodGet)
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			response.Header().Set("Allow", http.MethodGet)
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), timeout)
		defer cancel()
		response.Header().Set("Cache-Control", "no-store")
		if err := readiness.Ready(ctx); err != nil {
			http.Error(response, "trace storage is not ready", http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ready\n"))
	})
	return mux
}
