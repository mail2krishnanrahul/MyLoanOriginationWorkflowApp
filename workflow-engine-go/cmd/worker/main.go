package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"workflow-engine/internal/database"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load("../../.env", "../.env", ".env")

	logLevel := os.Getenv("LOG_LEVEL")
	var level slog.Level
	if err := level.UnmarshalText([]byte(logLevel)); err != nil {
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	slog.Info("Starting Workflow Task Worker process...")

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	dbCfg := database.Config{
		MaxConns: 20,
		MinConns: 2,
	}
	db, err := database.NewConnectionFromURL(dbURL, dbCfg)
	if err != nil {
		slog.Error("Failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := database.HealthCheck(context.Background(), db); err != nil {
		slog.Error("Database health check failed during startup", "err", err)
		os.Exit(1)
	}

	// Setup Worker dependencies and queue consumers
	// TODO: Wire internal/task queue runner

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Simulate worker event loop
	go func(ctx context.Context) {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				slog.Debug("Polling for unassigned background system tasks...")
			}
		}
	}(ctx)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down Task Worker gracefully...")
	cancel()

	slog.Info("Worker process exiting")
}
