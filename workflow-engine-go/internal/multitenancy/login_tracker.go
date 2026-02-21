package multitenancy

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jmoiron/sqlx"
)

var globalLoginTracker atomic.Pointer[LoginTracker]

// SetLoginTracker registers process-level login tracker used by RecordUserLogin.
func SetLoginTracker(lt *LoginTracker) {
	globalLoginTracker.Store(lt)
}

func getLoginTracker() *LoginTracker {
	return globalLoginTracker.Load()
}

// NewLoginTracker creates a buffered login tracker with sane defaults.
func NewLoginTracker(
	db *sqlx.DB,
	interval time.Duration,
	logger *slog.Logger,
) *LoginTracker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &LoginTracker{
		db:       db,
		interval: interval,
		logger:   logger,
	}
}

// Record stores a login timestamp in memory and never blocks caller flow.
func (lt *LoginTracker) Record(
	ctx context.Context,
	userID string,
) {
	_ = ctx
	if lt == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	lt.buffer.Store(userID, time.Now().UTC())
}

func (lt *LoginTracker) flush(ctx context.Context) (int, error) {
	if lt == nil {
		return 0, nil
	}
	if lt.db == nil {
		return 0, fmt.Errorf("LoginTracker.flush: db is nil")
	}

	userIDs := make([]string, 0)
	lt.buffer.Range(func(key, _ interface{}) bool {
		userID, ok := key.(string)
		if ok {
			userID = strings.TrimSpace(userID)
			if userID != "" {
				userIDs = append(userIDs, userID)
			}
		}
		return true
	})
	if len(userIDs) == 0 {
		return 0, nil
	}

	// Single batched write for all currently buffered users.
	_, err := lt.db.ExecContext(ctx, `
		UPDATE users
		SET last_login_at = now(),
		    updated_at = now()
		WHERE user_id = ANY($1::uuid[])
	`, userIDs)
	if err != nil {
		for i := range userIDs {
			lt.buffer.Delete(userIDs[i])
		}
		return 0, fmt.Errorf("LoginTracker.flush: update users: %w", err)
	}

	for i := range userIDs {
		lt.buffer.Delete(userIDs[i])
	}
	return len(userIDs), nil
}

// Run starts periodic flush loop and exits cleanly on context cancellation.
func (lt *LoginTracker) Run(ctx context.Context) error {
	if lt == nil {
		return nil
	}
	if lt.db == nil {
		return fmt.Errorf("LoginTracker.Run: db is nil")
	}
	if lt.interval <= 0 {
		lt.interval = 30 * time.Second
	}
	if lt.logger == nil {
		lt.logger = slog.Default()
	}

	ticker := time.NewTicker(lt.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			flushed, err := lt.flush(context.Background())
			if err != nil {
				lt.logger.Error("login tracker final flush failed", "error", err)
			} else if flushed > 0 {
				lt.logger.Info("login tracker final flush", "flushed_users", flushed)
			}
			return nil
		case <-ticker.C:
			flushed, err := lt.flush(ctx)
			if err != nil {
				lt.logger.Error("login tracker flush failed", "error", err)
				continue
			}
			if flushed > 0 {
				lt.logger.Info("login tracker flush complete", "flushed_users", flushed)
			}
		}
	}
}
