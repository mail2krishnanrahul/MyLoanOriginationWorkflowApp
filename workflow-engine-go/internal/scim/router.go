package scim

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type statusCaptureResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCaptureResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func generateRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UTC().UnixNano())
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	hexStr := hex.EncodeToString(buf)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexStr[0:8], hexStr[8:12], hexStr[12:16], hexStr[16:20], hexStr[20:32])
}

func requestIDMiddleware(next http.Handler) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeSCIMError(w, http.StatusNotFound, "", "not found")
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := generateRequestID()
		w.Header().Set("X-Request-ID", requestID)
		ctx := withRequestID(r.Context(), requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestLoggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeSCIMError(w, http.StatusNotFound, "", "not found")
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now().UTC()
		capture := &statusCaptureResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(capture, r)
		tenantID := ""
		tokenID := ""
		if claims, ok := SCIMClaimsFromContext(r.Context()); ok {
			tenantID = claims.TenantID
			tokenID = claims.TokenID
		}
		logger.Info("scim request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", capture.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"tenant_id", tenantID,
			"token_id", tokenID,
			"request_id", requestIDFromContext(r.Context()),
		)
	})
}

func panicRecoveryMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeSCIMError(w, http.StatusNotFound, "", "not found")
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("scim panic recovered", "panic", rec, "path", r.URL.Path, "request_id", requestIDFromContext(r.Context()))
				writeSCIMError(w, http.StatusInternalServerError, "", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func chainScopes(handler http.Handler, scopes ...string) http.Handler {
	wrapped := handler
	for i := len(scopes) - 1; i >= 0; i-- {
		scope := strings.TrimSpace(scopes[i])
		if scope == "" {
			continue
		}
		wrapped = SCIMScopeMiddleware(scope, wrapped)
	}
	return wrapped
}

// NewSCIMRouter builds the SCIM v2 router with middleware ordering and route scope guards.
func NewSCIMRouter(
	db *sqlx.DB,
	logger *slog.Logger,
) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := newSCIMHandler(db, logger)
	mux := http.NewServeMux()

	mux.Handle("GET /scim/v2/Schemas", chainScopes(http.HandlerFunc(h.GetSchemas)))
	mux.Handle("GET /scim/v2/Schemas/{id}", chainScopes(http.HandlerFunc(h.GetSchema)))
	mux.Handle("GET /scim/v2/ResourceTypes", chainScopes(http.HandlerFunc(h.GetResourceTypes)))
	mux.Handle("GET /scim/v2/ResourceTypes/{name}", chainScopes(http.HandlerFunc(h.GetResourceType)))
	mux.Handle("GET /scim/v2/ServiceProviderConfig", chainScopes(http.HandlerFunc(h.GetServiceProviderConfig)))

	mux.Handle("GET /scim/v2/Users", chainScopes(http.HandlerFunc(h.ListUsers), "users:read"))
	mux.Handle("GET /scim/v2/Users/{id}", chainScopes(http.HandlerFunc(h.GetUser), "users:read"))
	mux.Handle("POST /scim/v2/Users", chainScopes(http.HandlerFunc(h.CreateUser), "users:write"))
	mux.Handle("PUT /scim/v2/Users/{id}", chainScopes(http.HandlerFunc(h.ReplaceUser), "users:write"))
	mux.Handle("PATCH /scim/v2/Users/{id}", chainScopes(http.HandlerFunc(h.PatchUser), "users:write"))
	mux.Handle("DELETE /scim/v2/Users/{id}", chainScopes(http.HandlerFunc(h.DeleteUser), "users:write"))

	mux.Handle("GET /scim/v2/Groups", chainScopes(http.HandlerFunc(h.ListGroups), "groups:read"))
	mux.Handle("GET /scim/v2/Groups/{id}", chainScopes(http.HandlerFunc(h.GetGroup), "groups:read"))
	mux.Handle("POST /scim/v2/Groups", chainScopes(http.HandlerFunc(h.CreateGroup), "groups:write"))
	mux.Handle("PUT /scim/v2/Groups/{id}", chainScopes(http.HandlerFunc(h.ReplaceGroup), "groups:write"))
	mux.Handle("PATCH /scim/v2/Groups/{id}", chainScopes(http.HandlerFunc(h.PatchGroup), "groups:write"))
	mux.Handle("DELETE /scim/v2/Groups/{id}", chainScopes(http.HandlerFunc(h.DeleteGroup), "groups:write"))

	mux.Handle("POST /scim/v2/Bulk", chainScopes(http.HandlerFunc(h.BulkOperation), "users:write", "groups:write"))

	var handler http.Handler = mux
	handler = chainScopes(handler)
	handler = SCIMRateLimitMiddleware(db, handler)
	handler = SCIMMiddleware(db, handler)
	handler = panicRecoveryMiddleware(logger, handler)
	handler = requestLoggingMiddleware(logger, handler)
	handler = requestIDMiddleware(handler)
	return handler
}
