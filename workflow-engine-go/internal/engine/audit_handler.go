package engine

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"
)

// RegisterAuditHandlers attaches the audit trail HTTP handlers.
func RegisterAuditHandlers(mux *http.ServeMux, repo *repository.Repository) {
	mux.HandleFunc("GET /cases/{ref}/audit", handleGetAuditTrail(repo))
}

// handleGetAuditTrail returns the audit trail for a case.
// Query params: ?action=STAGE_CHANGED&limit=50&offset=0
func handleGetAuditTrail(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if ref == "" {
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 3 {
				ref = parts[2] // /cases/{ref}/audit
			}
		}

		if ref == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "reference_number is required",
			})
			return
		}

		// Look up case by reference number to get the UUID
		header, err := repo.GetCaseByReference(r.Context(), ref)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "case not found: " + ref,
			})
			return
		}

		// Parse query params
		filterAction := r.URL.Query().Get("action")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if limit <= 0 {
			limit = 50
		}

		entries, err := repo.GetAuditTrail(r.Context(), header.CaseID, filterAction, limit, offset)
		if err != nil {
			slog.Error("GET /cases/audit failed", "error", err, "ref", ref)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to fetch audit trail",
			})
			return
		}

		if entries == nil {
			entries = []model.AuditEntry{} // empty slice, not null
			writeJSON(w, http.StatusOK, entries)
			return
		}

		writeJSON(w, http.StatusOK, entries)
	}
}
