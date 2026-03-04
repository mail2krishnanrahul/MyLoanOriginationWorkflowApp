package getnext

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"workflow-engine/internal/multitenancy"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// RegisterGetNextHandlers registers all GetNext routes onto the provided mux.
//
//	POST   /v1/getnext/claim              — claim the next highest-scored case
//	GET    /v1/getnext/preview            — preview top-N without claiming
//	POST   /v1/getnext/skip               — skip a case (record reason)
//	GET    /v1/getnext/queue              — current queue depth and metrics
//	POST   /v1/getnext/refresh-affinity   — admin: refresh case_user_affinity view
//	POST   /v1/getnext/snapshot           — admin: record queue snapshot
//	GET    /v1/getnext/supervisor         — supervisor queue dashboard
func RegisterGetNextHandlers(mux *http.ServeMux, pool *pgxpool.Pool) {
	mux.HandleFunc("POST /v1/getnext/claim", handleClaim(pool))
	mux.HandleFunc("GET /v1/getnext/preview", handlePreview(pool))
	mux.HandleFunc("POST /v1/getnext/skip", handleSkip(pool))
	mux.HandleFunc("GET /v1/getnext/queue", handleQueueDepth(pool))
	mux.HandleFunc("POST /v1/getnext/refresh-affinity", handleRefreshAffinity(pool))
	mux.HandleFunc("POST /v1/getnext/snapshot", handleRecordSnapshot(pool))
	mux.HandleFunc("GET /v1/getnext/supervisor", handleSupervisor(pool))
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func tenantFromCtx(r *http.Request) string {
	// Read the resolved tenant UUID from context (set by TenantMiddleware).
	tid, err := multitenancy.TenantFromContext(r.Context())
	if err == nil && tid != "" {
		return tid
	}
	return "00000000-0000-0000-0000-000000000000"
}

func mapGetNextError(err error) (int, string) {
	switch {
	case errors.Is(err, ErrNoEligibleCases):
		return http.StatusOK, err.Error() // 200 + message (not an error from client POV)
	case errors.Is(err, ErrUserAtCapacity):
		return http.StatusConflict, err.Error()
	case errors.Is(err, ErrCaseAlreadyClaimed):
		return http.StatusConflict, err.Error()
	case errors.Is(err, ErrSkipLimitExceeded):
		return http.StatusUnprocessableEntity, err.Error()
	case errors.Is(err, ErrInvalidWeights):
		return http.StatusInternalServerError, err.Error()
	default:
		return http.StatusInternalServerError, "internal error"
	}
}

// observeLatency returns a closure that records duration when called (use with defer).
func observeLatency(tenantID, action string) func() {
	start := time.Now()
	return func() {
		GetNextLatencyHistogram.With(prometheus.Labels{
			"tenant_id": tenantID,
			"action":    action,
		}).Observe(time.Since(start).Seconds())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /v1/getnext/claim
// ─────────────────────────────────────────────────────────────────────────────

// handleClaim serves POST /v1/getnext/claim.
// Claims the highest-scored eligible case for the authenticated user.
// Request body (optional): { "caseTypeCode": "HOME_LOAN" }
func handleClaim(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := tenantFromCtx(r)
		userID := multitenancy.UserIDFromContext(r.Context())
		if userID == "" {
			writeErr(w, http.StatusUnauthorized, "authenticated user required")
			return
		}
		defer observeLatency(tenantID, "claim")()

		var body struct {
			CaseTypeCode string `json:"caseTypeCode"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		req := GetNextRequest{
			UserID:       userID,
			TenantID:     tenantID,
			CaseTypeCode: body.CaseTypeCode,
			Preview:      false,
		}

		result, err := GetNext(r.Context(), pool, req)
		if err != nil {
			status, msg := mapGetNextError(err)
			if errors.Is(err, ErrNoEligibleCases) {
				GetNextNoEligibleTotal.WithLabelValues(tenantID).Inc()
				writeJSON(w, status, map[string]interface{}{
					"message":         msg,
					"noEligibleCases": true,
				})
				return
			}
			if errors.Is(err, ErrUserAtCapacity) {
				GetNextCapacityBlockTotal.WithLabelValues(tenantID).Inc()
				writeErr(w, status, msg)
				return
			}
			slog.Error("POST /v1/getnext/claim failed", "error", err, "user_id", userID)
			writeErr(w, status, msg)
			return
		}

		GetNextClaimsTotal.WithLabelValues(tenantID).Inc()
		GetNextScoreHistogram.WithLabelValues(tenantID).Observe(result.CompositeScore)
		writeJSON(w, http.StatusOK, result)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/getnext/preview
// ─────────────────────────────────────────────────────────────────────────────

// handlePreview serves GET /v1/getnext/preview.
// Returns top-N scored cases without claiming any.
// Query params: caseTypeCode (optional), n (1–5, default 3)
func handlePreview(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := tenantFromCtx(r)
		userID := multitenancy.UserIDFromContext(r.Context())
		if userID == "" {
			writeErr(w, http.StatusUnauthorized, "authenticated user required")
			return
		}
		defer observeLatency(tenantID, "preview")()

		q := r.URL.Query()
		caseTypeCode := q.Get("caseTypeCode")
		topN := 3
		if n := q.Get("n"); n != "" {
			_, _ = fmt.Sscanf(n, "%d", &topN)
		}

		preview, err := GetNextPreview(r.Context(), pool, userID, tenantID, caseTypeCode, topN)
		if err != nil {
			status, msg := mapGetNextError(err)
			if errors.Is(err, ErrNoEligibleCases) {
				writeJSON(w, status, map[string]interface{}{
					"topCases":        []interface{}{},
					"queueDepth":      0,
					"noEligibleCases": true,
				})
				return
			}
			slog.Error("GET /v1/getnext/preview failed", "error", err, "user_id", userID)
			writeErr(w, status, msg)
			return
		}

		writeJSON(w, http.StatusOK, preview)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /v1/getnext/skip
// ─────────────────────────────────────────────────────────────────────────────

// handleSkip serves POST /v1/getnext/skip.
// Body: { "caseId": "...", "reason": "CONFLICT_OF_INTEREST", "notes": "..." }
func handleSkip(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := tenantFromCtx(r)
		userID := multitenancy.UserIDFromContext(r.Context())
		if userID == "" {
			writeErr(w, http.StatusUnauthorized, "authenticated user required")
			return
		}
		defer observeLatency(tenantID, "skip")()

		var body struct {
			CaseID string     `json:"caseId"`
			Reason SkipReason `json:"reason"`
			Notes  string     `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if body.CaseID == "" {
			writeErr(w, http.StatusBadRequest, "caseId is required")
			return
		}

		validReason := map[SkipReason]bool{
			SkipReasonFreeText:           true,
			SkipReasonConflictOfInterest: true,
			SkipReasonTooComplex:         true,
			SkipReasonWrongSkill:         true,
			SkipReasonOther:              true,
		}
		if !validReason[body.Reason] {
			writeErr(w, http.StatusBadRequest, "reason must be one of: FREE_TEXT, CONFLICT_OF_INTEREST, TOO_COMPLEX, WRONG_SKILL, OTHER")
			return
		}

		req := GetNextSkipRequest{
			UserID:   userID,
			TenantID: tenantID,
			CaseID:   body.CaseID,
			Reason:   body.Reason,
			Notes:    body.Notes,
		}

		if err := SkipCase(r.Context(), pool, req); err != nil {
			status, msg := mapGetNextError(err)
			if errors.Is(err, ErrSkipLimitExceeded) {
				writeErr(w, status, msg)
				return
			}
			slog.Error("POST /v1/getnext/skip failed", "error", err, "user_id", userID)
			writeErr(w, status, msg)
			return
		}

		GetNextSkipsTotal.With(prometheus.Labels{
			"tenant_id": tenantID,
			"reason":    string(body.Reason),
		}).Inc()

		writeJSON(w, http.StatusOK, map[string]string{
			"message": "case skipped",
			"caseId":  body.CaseID,
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/getnext/queue
// ─────────────────────────────────────────────────────────────────────────────

// handleQueueDepth serves GET /v1/getnext/queue.
// Query params: caseTypeCode (optional)
func handleQueueDepth(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := tenantFromCtx(r)
		userID := multitenancy.UserIDFromContext(r.Context())
		caseTypeCode := r.URL.Query().Get("caseTypeCode")

		depth, err := GetQueueDepth(r.Context(), pool, tenantID, caseTypeCode, userID)
		if err != nil {
			slog.Error("GET /v1/getnext/queue failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "failed to get queue depth")
			return
		}

		// Update gauge metrics
		GetNextQueueDepthGauge.WithLabelValues(tenantID).Set(float64(depth.TotalAllocatable))
		GetNextSLABreachedGauge.WithLabelValues(tenantID).Set(float64(depth.SLABreachedCount))

		writeJSON(w, http.StatusOK, depth)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /v1/getnext/refresh-affinity
// Admin-only: refresh the case_user_affinity materialised view.
// ─────────────────────────────────────────────────────────────────────────────

func handleRefreshAffinity(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := RefreshAffinityView(r.Context(), pool); err != nil {
			slog.Error("POST /v1/getnext/refresh-affinity failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "failed to refresh affinity view")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "case_user_affinity view refreshed"})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /v1/getnext/snapshot
// Admin-only: record a queue snapshot.
// ─────────────────────────────────────────────────────────────────────────────

func handleRecordSnapshot(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := tenantFromCtx(r)
		var body struct {
			CaseTypeCode string `json:"caseTypeCode"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		if err := RecordQueueSnapshot(r.Context(), pool, tenantID, body.CaseTypeCode); err != nil {
			slog.Error("POST /v1/getnext/snapshot failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "failed to record queue snapshot")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"message": "queue snapshot recorded"})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/getnext/supervisor
// ─────────────────────────────────────────────────────────────────────────────

// handleSupervisor serves GET /v1/getnext/supervisor.
// Returns the full supervisor dashboard payload.
func handleSupervisor(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := tenantFromCtx(r)
		userID := multitenancy.UserIDFromContext(r.Context())
		if userID == "" {
			writeErr(w, http.StatusUnauthorized, "authenticated user required")
			return
		}

		view, err := GetSupervisorQueueView(r.Context(), pool, tenantID, userID)
		if err != nil {
			slog.Error("GET /v1/getnext/supervisor failed", "error", err, "supervisor_id", userID)
			writeErr(w, http.StatusInternalServerError, "failed to build supervisor view")
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}
