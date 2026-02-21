package scim

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"workflow-engine/internal/multitenancy"
	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

func resolveTenantID(ctx context.Context, tenantID string, fn string) (string, error) {
	fromCtx, ctxErr := multitenancy.TenantFromContext(ctx)
	tenantID = strings.TrimSpace(tenantID)
	if ctxErr == nil {
		if tenantID == "" {
			tenantID = fromCtx
		} else if tenantID != fromCtx {
			return "", fmt.Errorf("%s: tenant mismatch context=%s input=%s", fn, fromCtx, tenantID)
		}
	}
	if tenantID == "" {
		if ctxErr != nil {
			return "", fmt.Errorf("%s: %w", fn, ctxErr)
		}
		return "", fmt.Errorf("%s: %w", fn, multitenancy.ErrTenantNotFound)
	}
	return tenantID, nil
}

func normalizeScopes(scopes []string) []string {
	uniq := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for i := range scopes {
		s := strings.ToLower(strings.TrimSpace(scopes[i]))
		if s == "" {
			continue
		}
		if _, exists := uniq[s]; exists {
			continue
		}
		uniq[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func generateSCIMRawToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generateSCIMRawToken: random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func tokenHash(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

func toPGTextArray(values []string) string {
	if len(values) == 0 {
		return "{}"
	}
	escaped := make([]string, 0, len(values))
	for i := range values {
		v := strings.ReplaceAll(values[i], `\`, `\\`)
		v = strings.ReplaceAll(v, `"`, `\"`)
		escaped = append(escaped, `"`+v+`"`)
	}
	return "{" + strings.Join(escaped, ",") + "}"
}

// CreateSCIMToken creates and persists a new SCIM bearer token hash and returns the raw token once.
func CreateSCIMToken(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	description string,
	scopes []string,
	expiresAt *time.Time,
	createdBy string,
) (rawToken string, err error) {
	if db == nil {
		return "", fmt.Errorf("CreateSCIMToken: db is nil")
	}
	resolvedTenantID, err := resolveTenantID(ctx, tenantID, "CreateSCIMToken")
	if err != nil {
		return "", fmt.Errorf("CreateSCIMToken: %w", err)
	}
	description = strings.TrimSpace(description)
	if description == "" {
		description = "SCIM token"
	}
	createdBy = strings.TrimSpace(createdBy)
	if createdBy == "" {
		createdBy = "system"
	}
	scopes = normalizeScopes(scopes)
	if len(scopes) == 0 {
		return "", fmt.Errorf("CreateSCIMToken: at least one scope is required")
	}
	if expiresAt != nil {
		exp := expiresAt.UTC()
		expiresAt = &exp
		if exp.Before(time.Now().UTC()) {
			return "", fmt.Errorf("CreateSCIMToken: expiresAt cannot be in the past")
		}
	}

	rawToken, err = generateSCIMRawToken()
	if err != nil {
		return "", fmt.Errorf("CreateSCIMToken: %w", err)
	}
	hash := tokenHash(rawToken)

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return "", fmt.Errorf("CreateSCIMToken: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var tokenID string
	if err := tx.GetContext(ctx, &tokenID, `
		INSERT INTO scim_tokens (
			tenant_id,
			token_hash,
			description,
			scopes,
			status,
			metadata,
			expires_at,
			created_by
		)
		VALUES (
			$1::uuid,
			$2,
			$3,
			$4::text[],
			'ACTIVE',
			'{}'::jsonb,
			$5,
			$6
		)
		RETURNING token_id::text
	`, resolvedTenantID, hash, description, toPGTextArray(scopes), expiresAt, createdBy); err != nil {
		return "", fmt.Errorf("CreateSCIMToken: insert token: %w", err)
	}

	payload, marshalErr := json.Marshal(map[string]interface{}{
		"tenant_id":   resolvedTenantID,
		"token_id":    tokenID,
		"description": description,
		"scopes":      scopes,
		"created_by":  createdBy,
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
	})
	if marshalErr != nil {
		return "", fmt.Errorf("CreateSCIMToken: marshal event payload: %w", marshalErr)
	}
	if err := multitenancy.PublishEvent(ctx, tx, model.Event{
		TenantID:      resolvedTenantID,
		EventType:     model.EventSCIMTokenCreated,
		Payload:       payload,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
		MaxAttempts:   5,
	}); err != nil {
		return "", fmt.Errorf("CreateSCIMToken: publish SCIM_TOKEN_CREATED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("CreateSCIMToken: commit: %w", err)
	}
	slog.Info("scim token created", "tenant_id", resolvedTenantID, "token_id", tokenID)
	return rawToken, nil
}

// RevokeSCIMToken marks a token REVOKED and emits SCIM_TOKEN_REVOKED.
func RevokeSCIMToken(
	ctx context.Context,
	db *sqlx.DB,
	tokenID string,
	tenantID string,
) error {
	if db == nil {
		return fmt.Errorf("RevokeSCIMToken: db is nil")
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return fmt.Errorf("RevokeSCIMToken: tokenID is required")
	}
	resolvedTenantID, err := resolveTenantID(ctx, tenantID, "RevokeSCIMToken")
	if err != nil {
		return fmt.Errorf("RevokeSCIMToken: %w", err)
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("RevokeSCIMToken: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	result, err := tx.ExecContext(ctx, `
		UPDATE scim_tokens
		SET status = 'REVOKED',
		    updated_at = now()
		WHERE tenant_id = $1::uuid
		  AND token_id = $2::uuid
		  AND status <> 'REVOKED'
	`, resolvedTenantID, tokenID)
	if err != nil {
		return fmt.Errorf("RevokeSCIMToken: update status: %w", err)
	}
	rows, _ := result.RowsAffected()

	payload, marshalErr := json.Marshal(map[string]interface{}{
		"tenant_id":   resolvedTenantID,
		"token_id":    tokenID,
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
	})
	if marshalErr != nil {
		return fmt.Errorf("RevokeSCIMToken: marshal event payload: %w", marshalErr)
	}
	if err := multitenancy.PublishEvent(ctx, tx, model.Event{
		TenantID:      resolvedTenantID,
		EventType:     model.EventSCIMTokenRevoked,
		Payload:       payload,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
		MaxAttempts:   5,
	}); err != nil {
		return fmt.Errorf("RevokeSCIMToken: publish SCIM_TOKEN_REVOKED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("RevokeSCIMToken: commit: %w", err)
	}

	_ = rows
	slog.Info("scim token revoked", "tenant_id", resolvedTenantID, "token_id", tokenID)
	return nil
}

// ValidateSCIMToken validates bearer token and returns tenant-scoped claims.
func ValidateSCIMToken(
	ctx context.Context,
	db *sqlx.DB,
	rawToken string,
) (SCIMTokenClaims, error) {
	if db == nil {
		return SCIMTokenClaims{}, fmt.Errorf("ValidateSCIMToken: db is nil")
	}
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return SCIMTokenClaims{}, fmt.Errorf("ValidateSCIMToken: %w", ErrSCIMTokenInvalid)
	}
	hash := tokenHash(rawToken)

	type tokenRow struct {
		TokenID    string       `db:"token_id"`
		TenantID   string       `db:"tenant_id"`
		ScopesJSON string       `db:"scopes_json"`
		Status     string       `db:"status"`
		ExpiresAt  sql.NullTime `db:"expires_at"`
	}
	var row tokenRow
	err := db.GetContext(ctx, &row, `
		SELECT token_id::text AS token_id,
		       tenant_id::text AS tenant_id,
		       COALESCE(array_to_json(scopes)::text, '[]') AS scopes_json,
		       status,
		       expires_at
		FROM scim_tokens
		WHERE token_hash = $1
	`, hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SCIMTokenClaims{}, fmt.Errorf("ValidateSCIMToken: %w", ErrSCIMTokenInvalid)
		}
		return SCIMTokenClaims{}, fmt.Errorf("ValidateSCIMToken: query token: %w", err)
	}

	if !strings.EqualFold(strings.TrimSpace(row.Status), string(SCIMTokenStatusActive)) {
		return SCIMTokenClaims{}, fmt.Errorf("ValidateSCIMToken: %w", ErrSCIMTokenInvalid)
	}
	if row.ExpiresAt.Valid && row.ExpiresAt.Time.UTC().Before(time.Now().UTC()) {
		return SCIMTokenClaims{}, fmt.Errorf("ValidateSCIMToken: %w", ErrSCIMTokenInvalid)
	}

	if tracker := getSCIMTokenUsageTracker(); tracker != nil {
		tracker.Record(ctx, row.TokenID)
	} else {
		go func(tokenID string) {
			_, err := db.ExecContext(context.Background(), `
				UPDATE scim_tokens
				SET last_used_at = now(),
				    updated_at = now()
				WHERE token_id = $1::uuid
			`, tokenID)
			if err != nil {
				slog.Error("scim token last_used_at async update failed", "token_id", tokenID, "error", err)
			}
		}(row.TokenID)
	}
	scopes := make([]string, 0)
	if strings.TrimSpace(row.ScopesJSON) != "" {
		_ = json.Unmarshal([]byte(row.ScopesJSON), &scopes)
	}
	slog.Debug("scim token validated", "tenant_id", strings.TrimSpace(row.TenantID), "token_id", strings.TrimSpace(row.TokenID))

	return SCIMTokenClaims{
		TenantID: strings.TrimSpace(row.TenantID),
		Scopes:   normalizeScopes(scopes),
		TokenID:  strings.TrimSpace(row.TokenID),
	}, nil
}

func extractBearerToken(header string) (string, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", fmt.Errorf("missing authorization header")
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid authorization header")
	}
	if !strings.EqualFold(strings.TrimSpace(parts[0]), "Bearer") {
		return "", fmt.Errorf("invalid authorization scheme")
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", fmt.Errorf("empty bearer token")
	}
	return token, nil
}

func isDiscoveryPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/scim/v2/Schemas") ||
		strings.HasPrefix(path, "/scim/v2/ResourceTypes") ||
		strings.HasPrefix(path, "/scim/v2/ServiceProviderConfig")
}

// SCIMMiddleware authenticates SCIM bearer tokens and injects claims + tenant into context.
func SCIMMiddleware(
	db *sqlx.DB,
	next http.Handler,
) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeSCIMError(w, http.StatusNotFound, "", "not found")
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isDiscoveryPath(r.URL.Path) {
			next.ServeHTTP(w, r.WithContext(withSCIMAuthBypass(r.Context(), true)))
			return
		}
		token, err := extractBearerToken(r.Header.Get("Authorization"))
		if err != nil {
			writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
			return
		}
		claims, err := ValidateSCIMToken(r.Context(), db, token)
		if err != nil {
			writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
			return
		}

		ctx := WithSCIMClaims(r.Context(), claims)
		ctx = multitenancy.WithTenant(ctx, claims.TenantID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// SCIMScopeMiddleware enforces required SCIM scope on authenticated requests.
func SCIMScopeMiddleware(
	requiredScope string,
	next http.Handler,
) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeSCIMError(w, http.StatusNotFound, "", "not found")
		})
	}
	requiredScope = strings.ToLower(strings.TrimSpace(requiredScope))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if scimAuthBypassFromContext(r.Context()) || requiredScope == "" {
			next.ServeHTTP(w, r)
			return
		}
		claims, ok := SCIMClaimsFromContext(r.Context())
		if !ok {
			writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
			return
		}
		if !scopeContains(claims.Scopes, requiredScope) {
			writeSCIMError(w, http.StatusForbidden, "", "insufficient scope")
			return
		}
		next.ServeHTTP(w, r)
	})
}
