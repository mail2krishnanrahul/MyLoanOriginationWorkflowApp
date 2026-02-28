package multitenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/jmoiron/sqlx"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

// TenantMiddleware validates incoming tenant_id or tenant_code and injects tenant_id into context.
func TenantMiddleware(db *sqlx.DB, next http.Handler) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/healthz" || path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		if db == nil {
			writeTenantError(w, http.StatusInternalServerError, "tenant middleware misconfigured")
			return
		}

		identifier := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
		if identifier == "" {
			identifier = strings.TrimSpace(r.Header.Get("X-Tenant-Code"))
		}
		if identifier == "" {
			if v := r.Context().Value("jwt_tenant_id"); v != nil {
				identifier = strings.TrimSpace(fmt.Sprint(v))
			}
		}
		if identifier == "" {
			// Fall back to the DEFAULT tenant when no tenant header is present.
			// This supports single-tenant dev deployments and non-tenant-aware API clients.
			identifier = "DEFAULT"
		}

		slog.Debug("tenant middleware resolving", "identifier", identifier, "path", path)
		tenant, err := loadTenantByIdentifier(r.Context(), db, identifier)
		if err != nil {
			slog.Warn("tenant middleware lookup failed", "identifier", identifier, "error", err)
			if errors.Is(err, ErrTenantNotFound) {
				writeTenantError(w, http.StatusNotFound, ErrTenantNotFound.Error())
				return
			}
			writeTenantError(w, http.StatusInternalServerError, err.Error())
			return
		}

		switch tenant.Status {
		case TenantStatusActive:
			ctx := WithTenant(r.Context(), tenant.TenantID)
			slog.Debug("tenant middleware accepted request", "tenant_id", tenant.TenantID, "tenant_code", tenant.TenantCode, "path", path)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		case TenantStatusSuspended:
			writeTenantError(w, http.StatusForbidden, ErrTenantSuspended.Error())
			return
		case TenantStatusOffboarded:
			writeTenantError(w, http.StatusForbidden, ErrTenantOffboarded.Error())
			return
		default:
			writeTenantError(w, http.StatusForbidden, fmt.Sprintf("unsupported tenant status %s", tenant.Status))
			return
		}
	})
}

func loadTenantByIdentifier(ctx context.Context, db *sqlx.DB, identifier string) (Tenant, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return Tenant{}, ErrTenantNotFound
	}

	q := `
		SELECT tenant_id::text AS tenant_id, tenant_code, name, status, tier, config, created_at, updated_at
		FROM tenants
		WHERE tenant_id::text = $1
	`
	args := []interface{}{identifier}
	if !uuidPattern.MatchString(identifier) {
		q = `
			SELECT tenant_id::text AS tenant_id, tenant_code, name, status, tier, config, created_at, updated_at
			FROM tenants
			WHERE tenant_code = $1
		`
		args = []interface{}{strings.ToUpper(identifier)}
	}

	var tenant Tenant
	if err := db.GetContext(ctx, &tenant, q, args...); err != nil {
		slog.Warn("loadTenantByIdentifier query error", "identifier", identifier, "error", err, "error_type", fmt.Sprintf("%T", err), "is_no_rows", errors.Is(err, sql.ErrNoRows))
		if errors.Is(err, sql.ErrNoRows) {
			return Tenant{}, ErrTenantNotFound
		}
		return Tenant{}, fmt.Errorf("loadTenantByIdentifier: query tenant: %w", err)
	}
	return tenant, nil
}

func writeTenantError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
