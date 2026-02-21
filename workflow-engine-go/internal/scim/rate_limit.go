package scim

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type scimRateLimitMetadata struct {
	MaxRequestsPerMinute       int `json:"max_requests_per_minute"`
	MaxBulkOperationsPerMinute int `json:"max_bulk_operations_per_minute"`
}

const (
	defaultSCIMMaxRequestsPerMinute = 300
	defaultSCIMMaxBulkPerMinute     = 5000
)

// EnforceSCIMRateLimit enforces token-level per-minute SCIM request limits.
func EnforceSCIMRateLimit(
	ctx context.Context,
	db *sqlx.DB,
	tokenID string,
	cost int,
) error {
	if db == nil {
		return fmt.Errorf("EnforceSCIMRateLimit: db is nil")
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return fmt.Errorf("EnforceSCIMRateLimit: tokenID is required")
	}
	if cost <= 0 {
		cost = 1
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("EnforceSCIMRateLimit: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var status string
	var metadataRaw json.RawMessage
	if err := tx.QueryRowxContext(ctx, `
		SELECT status, metadata
		FROM scim_tokens
		WHERE token_id = $1::uuid
		FOR UPDATE
	`, tokenID).Scan(&status, &metadataRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("EnforceSCIMRateLimit: %w", ErrSCIMTokenInvalid)
		}
		return fmt.Errorf("EnforceSCIMRateLimit: load token metadata: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(status), string(SCIMTokenStatusActive)) {
		return fmt.Errorf("EnforceSCIMRateLimit: %w", ErrSCIMTokenInvalid)
	}

	limitCfg := scimRateLimitMetadata{}
	if len(metadataRaw) > 0 {
		_ = json.Unmarshal(metadataRaw, &limitCfg)
	}
	maxReq := limitCfg.MaxRequestsPerMinute
	if maxReq <= 0 {
		maxReq = defaultSCIMMaxRequestsPerMinute
	}
	maxBulk := limitCfg.MaxBulkOperationsPerMinute
	if maxBulk <= 0 {
		maxBulk = defaultSCIMMaxBulkPerMinute
	}
	limit := maxReq
	if cost > 1 {
		limit = maxBulk
	}

	windowStart := time.Now().UTC().Truncate(time.Minute)
	var requestCount int
	if err := tx.GetContext(ctx, &requestCount, `
		INSERT INTO scim_token_rate_limit_counters (token_id, window_start, request_count)
		VALUES ($1::uuid, $2, $3)
		ON CONFLICT (token_id, window_start)
		DO UPDATE SET
			request_count = scim_token_rate_limit_counters.request_count + EXCLUDED.request_count,
			updated_at = now()
		RETURNING request_count
	`, tokenID, windowStart, cost); err != nil {
		return fmt.Errorf("EnforceSCIMRateLimit: increment counter: %w", err)
	}

	if requestCount > limit {
		retryAfter := int(windowStart.Add(time.Minute).Sub(time.Now().UTC()).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		return fmt.Errorf("EnforceSCIMRateLimit: %w", scimRateLimitError(retryAfter))
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("EnforceSCIMRateLimit: commit: %w", err)
	}
	return nil
}

// SCIMRateLimitMiddleware applies per-token request limiting (cost=1).
func SCIMRateLimitMiddleware(
	db *sqlx.DB,
	next http.Handler,
) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeSCIMError(w, http.StatusNotFound, "", "not found")
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if scimAuthBypassFromContext(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.EqualFold(strings.TrimSpace(r.URL.Path), "/scim/v2/Bulk") {
			next.ServeHTTP(w, r)
			return
		}
		claims, ok := SCIMClaimsFromContext(r.Context())
		if !ok {
			writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
			return
		}
		err := EnforceSCIMRateLimit(r.Context(), db, claims.TokenID, 1)
		if err != nil {
			if errors.Is(err, ErrSCIMRateLimitExceeded) {
				retryAfter := parseStatusReasonFromRateLimitError(err)
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				writeSCIMError(w, http.StatusTooManyRequests, "", "rate limit exceeded")
				return
			}
			writeSCIMError(w, http.StatusInternalServerError, "", "rate limit evaluation failed")
			return
		}
		next.ServeHTTP(w, r)
	})
}
