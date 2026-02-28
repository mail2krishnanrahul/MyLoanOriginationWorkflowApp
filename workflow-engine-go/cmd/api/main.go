package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"workflow-engine/internal/database"
	"workflow-engine/internal/engine"
	"workflow-engine/internal/integration"
	"workflow-engine/internal/multitenancy"
	"workflow-engine/internal/notification"
	"workflow-engine/internal/repository"
	"workflow-engine/internal/server"

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

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	// 1. Connect to DB
	slog.Info("Initializing database connection pool...")
	dbCfg := database.Config{
		MaxConns: 25,
		MinConns: 5,
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

	pgxDB, err := database.Connect(context.Background(), dbURL)
	if err != nil {
		slog.Error("Failed to connect pgxpool to database", "err", err)
		os.Exit(1)
	}
	defer pgxDB.Close()

	// 2. Run Migrations (optional, usually preferred via cmd/migrate but can run up cleanly on pod start)
	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "db/migrations"
	}
	slog.Info("Running pending database migrations...")
	if err := database.RunMigrations(dbURL, migrationsDir); err != nil {
		slog.Error("Database migration failed on startup", "err", err)
		// We log instead of exit, as the schema might be managed out-of-band by a K8s Job
	}

	// 3. Setup HTTP router
	rootMux := http.NewServeMux()
	apiMux := http.NewServeMux()

	// Health probes
	rootMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := database.HealthCheck(r.Context(), db); err != nil {
			http.Error(w, `{"status":"unhealthy","database":false}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"up","database":true}`))
	})

	// 4. OpenAPI / Swagger Docs
	rootMux.HandleFunc("GET /swagger/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "docs/openapi.yaml")
	})
	rootMux.HandleFunc("GET /swagger/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "docs/swagger.html")
	})

	repo := repository.NewRepository(pgxDB.Pool)
	engine.RegisterCaseHandlers(apiMux, repo)
	engine.RegisterCaseListHandler(apiMux, repo)
	engine.RegisterCaseDetailHandler(apiMux, repo)
	engine.RegisterAuditHandlers(apiMux, repo)
	engine.RegisterWorkbasketHandlers(apiMux, repo)
	notification.RegisterNotificationHandlers(apiMux, db, slog.Default())
	integration.RegisterDealIngestionHandlers(apiMux, db, slog.Default())

	rootMux.Handle("/api/", http.StripPrefix("/api", multitenancy.TenantMiddleware(db, apiMux)))

	// Bind middleware stack
	allowedOrigins := os.Getenv("CORS_ALLOWED_ORIGINS")
	if allowedOrigins == "" {
		allowedOrigins = "http://localhost:5173,http://localhost:3000,http://localhost:80" // Defaults for local UI devs
	}

	handler := server.RequestLogger(rootMux)
	handler = server.CORSMiddleware(allowedOrigins)(handler)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: handler,
	}

	// 4. Start Server with Graceful Shutdown
	go func() {
		slog.Info(fmt.Sprintf("🚀 Listening and serving API on port %s", port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed", "err", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down API server gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "err", err)
		os.Exit(1)
	}

	slog.Info("API server exiting")
}
