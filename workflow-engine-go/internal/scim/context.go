package scim

import (
	"context"
	"strings"
)

type claimsContextKey struct{}
type requestIDContextKey struct{}
type authBypassContextKey struct{}

// WithSCIMClaims stores validated SCIM token claims in context.
func WithSCIMClaims(ctx context.Context, claims SCIMTokenClaims) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

// SCIMClaimsFromContext retrieves SCIM token claims from context.
func SCIMClaimsFromContext(ctx context.Context) (SCIMTokenClaims, bool) {
	if ctx == nil {
		return SCIMTokenClaims{}, false
	}
	v := ctx.Value(claimsContextKey{})
	claims, ok := v.(SCIMTokenClaims)
	if !ok {
		return SCIMTokenClaims{}, false
	}
	claims.TenantID = strings.TrimSpace(claims.TenantID)
	claims.TokenID = strings.TrimSpace(claims.TokenID)
	if claims.TenantID == "" || claims.TokenID == "" {
		return SCIMTokenClaims{}, false
	}
	return claims, true
}

func withRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

func requestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(requestIDContextKey{}); v != nil {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	if v := ctx.Value("request_id"); v != nil {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func withSCIMAuthBypass(ctx context.Context, bypass bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, authBypassContextKey{}, bypass)
}

func scimAuthBypassFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v := ctx.Value(authBypassContextKey{})
	b, ok := v.(bool)
	return ok && b
}
