// Command agentshark-migrate applies the embedded PostgreSQL schema without
// starting the BFF or requiring upstream management-plane credentials.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	storagepostgres "github.com/Thespectier/AgentsharkX/apps/server/internal/storage/postgres"
)

var (
	version  = "development"
	revision = "unknown"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	databaseURL := strings.TrimSpace(os.Getenv("AGENTSHARK_DATABASE_URL"))
	if databaseURL == "" {
		logger.Error("AGENTSHARK_DATABASE_URL is required")
		os.Exit(1)
	}
	rootContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(rootContext, 2*time.Minute)
	defer cancel()
	if err := migrate(ctx, databaseURL); err != nil {
		logger.Error("database migration failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("database migration complete", "version", version, "revision", revision)
}

func migrate(ctx context.Context, databaseURL string) error {
	if strings.TrimSpace(databaseURL) == "" {
		return errors.New("database URL is required")
	}
	store, err := storagepostgres.Open(ctx, databaseURL, storagepostgres.Options{
		MaxConnections: 2,
		MinConnections: 0,
		ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return err
	}
	return store.Ready(ctx)
}
