package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"workflow-engine/internal/approval"
	"workflow-engine/internal/database"
	"workflow-engine/internal/document"
	"workflow-engine/internal/engine"
	"workflow-engine/internal/engine/assignment"
	"workflow-engine/internal/integration"
	"workflow-engine/internal/multitenancy"
	"workflow-engine/internal/notification"
	"workflow-engine/internal/reporting"
	"workflow-engine/internal/repository"
	"workflow-engine/internal/sla"

	"github.com/jmoiron/sqlx"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	slog.Info("starting workflow engine")

	// Configuration
	connString := os.Getenv("DB_URL")
	if connString == "" {
		connString = "postgres://myappuser:password@localhost:5432/LoanOriginationDB" // Default for local dev
		slog.Warn("DB_URL not set, using default")
	}

	workerCountStr := os.Getenv("WORKER_COUNT")
	workerCount := 10 // Default
	if workerCountStr != "" {
		if val, err := strconv.Atoi(workerCountStr); err == nil && val > 0 {
			workerCount = val
		}
	}
	slog.Info("configuration", "worker_count", workerCount)

	// Context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Database Connection
	db, err := database.Connect(ctx, connString)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("connected to database")

	// Repository & Engine
	repo := repository.NewRepository(db.Pool)
	assignmentManager := assignment.NewManager(repo)
	workflowEngine := engine.NewEngine(repo, assignmentManager, workerCount)

	// SQLX connection for SLA calendar-aware computations and sweep job.
	stdDB, err := sql.Open("pgx", connString)
	if err != nil {
		slog.Error("failed to open sql driver connection", "error", err)
		os.Exit(1)
	}
	defer stdDB.Close()
	sqlxDB := sqlx.NewDb(stdDB, "pgx")
	repo.SetSQLX(sqlxDB)
	slaSweepJob := sla.NewSLASweepJob(sqlxDB, nil, 5*time.Minute, 5000, slog.Default())
	approvalEvaluator := approval.NewApprovalPolicyEvaluator(sqlxDB, slog.Default(), nil)
	approvalExpirySweepJob := approval.NewApprovalExpirySweepJob(sqlxDB, nil, approvalEvaluator, 1*time.Minute, 500, slog.Default())
	tenantRateLimitCleanupJob := multitenancy.NewTenantRateLimitCleanupJob(sqlxDB, 1*time.Minute, 10000, slog.Default())
	if ttlRaw := strings.TrimSpace(os.Getenv("TENANT_FEATURE_CACHE_TTL_SECONDS")); ttlRaw != "" {
		if ttlSec, parseErr := strconv.Atoi(ttlRaw); parseErr == nil && ttlSec > 0 {
			multitenancy.SetTenantFeatureCacheTTL(time.Duration(ttlSec) * time.Second)
		}
	}
	multitenancy.RegisterTenantMetrics(nil)
	integration.RegisterIntegrationMetrics(nil)

	notificationRenderer := notification.NewTemplateRenderer()
	notificationService := notification.NewNotificationService(
		sqlxDB,
		notificationRenderer,
		&approval.ExpressionEvaluator{},
		nil,
		slog.Default(),
	)
	metricsRefreshJob := reporting.NewMetricsRefreshJob(sqlxDB, 5*time.Minute, 3, slog.Default())
	reportingObserver := reporting.NewEventHintObserver(metricsRefreshJob, slog.Default())
	workflowEngine.SetEventObserver(engine.NewMultiEventObserver(notificationService, reportingObserver))

	notificationChannels := map[string]notification.NotificationChannel{
		"IN_APP": notification.NewInAppChannel(sqlxDB, slog.Default()),
	}

	smtpHost := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	smtpPort := 587
	if smtpPortRaw := strings.TrimSpace(os.Getenv("SMTP_PORT")); smtpPortRaw != "" {
		if parsed, parseErr := strconv.Atoi(smtpPortRaw); parseErr == nil && parsed > 0 {
			smtpPort = parsed
		}
	}
	smtpFrom := strings.TrimSpace(os.Getenv("SMTP_FROM"))
	if smtpHost != "" && smtpFrom != "" {
		notificationChannels["EMAIL"] = notification.NewEmailChannel(
			smtpHost,
			smtpPort,
			smtpFrom,
			strings.TrimSpace(os.Getenv("SMTP_USERNAME")),
			os.Getenv("SMTP_PASSWORD"),
			slog.Default(),
		)
	} else {
		slog.Warn("EMAIL channel adapter disabled; set SMTP_HOST and SMTP_FROM to enable dispatch")
	}

	smsSID := strings.TrimSpace(os.Getenv("TWILIO_ACCOUNT_SID"))
	smsToken := os.Getenv("TWILIO_AUTH_TOKEN")
	smsFrom := strings.TrimSpace(os.Getenv("TWILIO_FROM_NUMBER"))
	if smsSID != "" && smsToken != "" && smsFrom != "" {
		notificationChannels["SMS"] = notification.NewSMSChannel(
			smsSID,
			smsToken,
			smsFrom,
			nil,
			slog.Default(),
		)
	} else {
		slog.Warn("SMS channel adapter disabled; set TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN and TWILIO_FROM_NUMBER to enable dispatch")
	}

	notificationDispatcher := notification.NewNotificationDispatcher(
		sqlxDB,
		notificationRenderer,
		notificationChannels,
		notification.NewCircuitBreaker(sqlxDB, 10, 5*time.Minute, 3, slog.Default(), nil),
		10*time.Second,
		500,
		5,
		slog.Default(),
		nil,
	)

	documentStorageBasePath := strings.TrimSpace(os.Getenv("DOCUMENT_STORAGE_BASE_PATH"))
	if documentStorageBasePath == "" {
		documentStorageBasePath = "/tmp/workflow-documents"
	}
	documentStorage := document.NewLocalStorage(documentStorageBasePath, slog.Default())
	documentRetentionJob := document.NewDocumentRetentionJob(
		sqlxDB,
		documentStorage,
		strings.TrimSpace(os.Getenv("DOCUMENT_ARCHIVE_BUCKET")),
		24*time.Hour,
		10000,
		slog.Default(),
	)
	webhookDispatcher := integration.NewWebhookDispatcher(sqlxDB, &http.Client{Timeout: 10 * time.Second}, 1*time.Second, slog.Default())
	serviceHealthChecker := integration.NewServiceHealthChecker(sqlxDB, &http.Client{Timeout: 10 * time.Second}, 1*time.Minute, slog.Default())
	idempotencyCleanupJob := integration.NewIdempotencyKeyCleanupJob(sqlxDB, 24*time.Hour, 50000, slog.Default())
	ingestedEventKeyCleanupJob := integration.NewIngestedEventKeyCleanupJob(sqlxDB, 24*time.Hour, 50000, slog.Default())

	// Health Check Server
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Pool.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(fmt.Sprintf("DB Unavailable: %v", err)))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Ready"))
	})

	// Case management API
	engine.RegisterCaseHandlers(mux, repo)
	notification.RegisterNotificationHandlers(mux, sqlxDB, slog.Default())

	srv := &http.Server{
		Addr:    ":8080",
		Handler: multitenancy.TenantMiddleware(sqlxDB, mux),
	}

	go func() {
		slog.Info("starting health server", "addr", ":8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("health server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Handle Shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start Engine (in background)
	engineDone := make(chan struct{})
	go func() {
		workflowEngine.Start(ctx)
		close(engineDone)
	}()

	go notificationDispatcher.Start(ctx)
	go metricsRefreshJob.Start(ctx)
	go tenantRateLimitCleanupJob.Start(ctx)
	go func() {
		if err := webhookDispatcher.Run(ctx); err != nil {
			slog.Error("webhook dispatcher failed", "error", err)
		}
	}()
	go func() {
		if err := serviceHealthChecker.Run(ctx); err != nil {
			slog.Error("service health checker failed", "error", err)
		}
	}()
	go idempotencyCleanupJob.Start(ctx)
	go ingestedEventKeyCleanupJob.Start(ctx)

	// Start Sweepers
	go func() {
		slaTicker := time.NewTicker(5 * time.Minute)
		approvalTicker := time.NewTicker(1 * time.Minute)
		capTicker := time.NewTicker(15 * time.Minute)
		expiryTicker := time.NewTicker(10 * time.Minute)
		archivalTicker := time.NewTicker(1 * time.Hour)
		documentRetentionTicker := time.NewTicker(24 * time.Hour)
		defer slaTicker.Stop()
		defer approvalTicker.Stop()
		defer capTicker.Stop()
		defer expiryTicker.Stop()
		defer archivalTicker.Stop()
		defer documentRetentionTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-slaTicker.C:
				if err := slaSweepJob.Run(ctx); err != nil {
					slog.Error("sla sweep failed", "error", err)
				}
			case <-approvalTicker.C:
				if err := approvalExpirySweepJob.Run(ctx); err != nil {
					slog.Error("approval expiry sweep failed", "error", err)
				}
			case <-capTicker.C:
				if err := workflowEngine.RunCapacitySweep(ctx); err != nil {
					slog.Error("capacity sweep failed", "error", err)
				}
			case <-expiryTicker.C:
				if err := workflowEngine.RunExpirySweep(ctx); err != nil {
					slog.Error("expiry sweep failed", "error", err)
				}
			case <-archivalTicker.C:
				if err := workflowEngine.RunArchivalSweep(ctx); err != nil {
					slog.Error("archival sweep failed", "error", err)
				}
			case <-documentRetentionTicker.C:
				if err := documentRetentionJob.Run(ctx); err != nil {
					slog.Error("document retention sweep failed", "error", err)
				}
			}
		}
	}()

	// Wait for Signal
	sig := <-sigChan
	slog.Info("received signal, initiating shutdown", "signal", sig)

	// 1. Cancel context to stop Poller
	cancel()

	// 2. Shutdown Health Server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("health server shutdown error", "error", err)
	}

	// 3. Wait for Engine to drain (Workers to finish)
	// Note: We need to ensure engine.Start() returns when workers are done.
	// Currently engine.Start() waits for wg.Wait(), so this should block until workers finish.
	select {
	case <-engineDone:
		slog.Info("engine stopped gracefully")
	case <-time.After(30 * time.Second): // Hard timeout for pod termination
		slog.Warn("timeout waiting for engine to drain, exiting")
	}
}
