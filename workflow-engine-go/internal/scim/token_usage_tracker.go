package scim

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jmoiron/sqlx"
)

// SCIMTokenUsageTracker buffers last_used_at writes and flushes them in batch.
type SCIMTokenUsageTracker struct {
	db       *sqlx.DB
	buffer   sync.Map // map[string]time.Time
	interval time.Duration
	logger   *slog.Logger
}

var globalTokenUsageTracker atomic.Pointer[SCIMTokenUsageTracker]

func SetSCIMTokenUsageTracker(tracker *SCIMTokenUsageTracker) {
	globalTokenUsageTracker.Store(tracker)
}

func getSCIMTokenUsageTracker() *SCIMTokenUsageTracker {
	return globalTokenUsageTracker.Load()
}

func NewSCIMTokenUsageTracker(db *sqlx.DB, interval time.Duration, logger *slog.Logger) *SCIMTokenUsageTracker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &SCIMTokenUsageTracker{
		db:       db,
		interval: interval,
		logger:   logger,
	}
}

func (t *SCIMTokenUsageTracker) Record(_ context.Context, tokenID string) {
	if t == nil {
		return
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return
	}
	t.buffer.Store(tokenID, time.Now().UTC())
}

func (t *SCIMTokenUsageTracker) flush(ctx context.Context) (int, error) {
	if t == nil {
		return 0, nil
	}
	if t.db == nil {
		return 0, fmt.Errorf("SCIMTokenUsageTracker.flush: db is nil")
	}
	tokenIDs := make([]string, 0)
	t.buffer.Range(func(key, _ interface{}) bool {
		id, ok := key.(string)
		if ok {
			id = strings.TrimSpace(id)
			if id != "" {
				tokenIDs = append(tokenIDs, id)
			}
		}
		return true
	})
	if len(tokenIDs) == 0 {
		return 0, nil
	}
	if _, err := t.db.ExecContext(ctx, `
		UPDATE scim_tokens
		SET last_used_at = now(),
		    updated_at = now()
		WHERE token_id = ANY($1::uuid[])
	`, tokenIDs); err != nil {
		return 0, fmt.Errorf("SCIMTokenUsageTracker.flush: update last_used_at: %w", err)
	}
	for i := range tokenIDs {
		t.buffer.Delete(tokenIDs[i])
	}
	return len(tokenIDs), nil
}

func (t *SCIMTokenUsageTracker) Run(ctx context.Context) error {
	if t == nil {
		return nil
	}
	if t.db == nil {
		return fmt.Errorf("SCIMTokenUsageTracker.Run: db is nil")
	}
	if t.interval <= 0 {
		t.interval = 30 * time.Second
	}
	if t.logger == nil {
		t.logger = slog.Default()
	}

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			flushed, err := t.flush(context.Background())
			if err != nil {
				t.logger.Error("scim token usage final flush failed", "error", err)
			} else if flushed > 0 {
				t.logger.Info("scim token usage final flush", "tokens", flushed)
			}
			return nil
		case <-ticker.C:
			flushed, err := t.flush(ctx)
			if err != nil {
				t.logger.Error("scim token usage flush failed", "error", err)
				continue
			}
			if flushed > 0 {
				t.logger.Info("scim token usage flush complete", "tokens", flushed)
			}
		}
	}
}
