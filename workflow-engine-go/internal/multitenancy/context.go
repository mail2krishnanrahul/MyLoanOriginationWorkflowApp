package multitenancy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"workflow-engine/pkg/model"
)

// WithTenant returns a child context that carries tenant_id.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return ctx
	}
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

// TenantFromContext extracts tenant_id from context.
func TenantFromContext(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", ErrTenantNotFound
	}
	if v := ctx.Value(tenantContextKey{}); v != nil {
		if tenantID, ok := v.(string); ok {
			tenantID = strings.TrimSpace(tenantID)
			if tenantID != "" {
				return tenantID, nil
			}
		}
	}
	for _, key := range []string{"tenant_id", "tenantID", "x_tenant_id"} {
		if v := ctx.Value(key); v != nil {
			if tenantID, ok := v.(string); ok {
				tenantID = strings.TrimSpace(tenantID)
				if tenantID != "" {
					return tenantID, nil
				}
			}
		}
	}
	return "", ErrTenantNotFound
}

// TenantFromPayload extracts tenant_id from event payload JSON.
func TenantFromPayload(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		return ""
	}
	raw, ok := body["tenant_id"]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

// EnsureTenantContextForEvent injects tenant_id from event into context if missing.
func EnsureTenantContextForEvent(ctx context.Context, event model.Event) context.Context {
	if _, err := TenantFromContext(ctx); err == nil {
		return ctx
	}
	tenantID := strings.TrimSpace(event.TenantID)
	if tenantID == "" {
		tenantID = TenantFromPayload(event.Payload)
	}
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	return WithTenant(ctx, tenantID)
}
