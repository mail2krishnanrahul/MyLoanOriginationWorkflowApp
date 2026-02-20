package multitenancy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	defaultRateCounterCleanupInterval = 1 * time.Minute
	defaultRateCounterCleanupBatch    = 5000
)

// TenantRateLimitCleanupJob prunes stale tenant minute-window counters.
type TenantRateLimitCleanupJob struct {
	db        *sqlx.DB
	interval  time.Duration
	batchSize int
	logger    *slog.Logger
}

func NewTenantRateLimitCleanupJob(db *sqlx.DB, interval time.Duration, batchSize int, logger *slog.Logger) *TenantRateLimitCleanupJob {
	if interval <= 0 {
		interval = defaultRateCounterCleanupInterval
	}
	if batchSize <= 0 {
		batchSize = defaultRateCounterCleanupBatch
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &TenantRateLimitCleanupJob{
		db:        db,
		interval:  interval,
		batchSize: batchSize,
		logger:    logger,
	}
}

// Run executes one cleanup pass.
func (j *TenantRateLimitCleanupJob) Run(ctx context.Context) error {
	if j == nil {
		return fmt.Errorf("TenantRateLimitCleanupJob.Run: job is nil")
	}
	if j.db == nil {
		return fmt.Errorf("TenantRateLimitCleanupJob.Run: db is nil")
	}

	cutoff := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Minute)
	result, err := j.db.ExecContext(ctx, `
		WITH doomed AS (
			SELECT tenant_id, window_start
			FROM tenant_rate_limit_counters
			WHERE window_start < $1
			ORDER BY window_start ASC
			LIMIT $2
		)
		DELETE FROM tenant_rate_limit_counters c
		USING doomed d
		WHERE c.tenant_id = d.tenant_id
		  AND c.window_start = d.window_start
	`, cutoff, j.batchSize)
	if err != nil {
		return fmt.Errorf("TenantRateLimitCleanupJob.Run: delete stale windows: %w", err)
	}

	affected, _ := result.RowsAffected()
	j.logger.Info("tenant rate-limit counter cleanup completed",
		"cutoff", cutoff,
		"rows_deleted", affected)
	return nil
}

// Start runs cleanup on a ticker until context cancellation.
func (j *TenantRateLimitCleanupJob) Start(ctx context.Context) {
	if j == nil {
		return
	}
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			j.logger.Info("tenant rate-limit cleanup job stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			if err := j.Run(ctx); err != nil {
				j.logger.Error("tenant rate-limit cleanup failed", "error", err)
			}
		}
	}
}
