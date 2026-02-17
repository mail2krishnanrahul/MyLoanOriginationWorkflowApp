package engine

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"workflow-engine/internal/repository"
)

// RegisterCaseHandlers attaches the case-related HTTP handlers to the mux.
func RegisterCaseHandlers(mux *http.ServeMux, repo *repository.Repository) {
	mux.HandleFunc("POST /cases", handleCreateCase(repo))
}

// handleCreateCase returns an http.HandlerFunc for POST /cases.
func handleCreateCase(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. Decode request
		var req CreateCaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid request body: " + err.Error(),
			})
			return
		}

		// 2. Validate required fields
		if req.CaseTypeCode == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "case_type_code is required",
			})
			return
		}
		if req.RequestedBy == "" {
			req.RequestedBy = "api" // default
		}

		// 3. Create case
		resp, err := CreateCase(r.Context(), repo, req)
		if err != nil {
			slog.Error("POST /cases failed", "error", err)
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error": err.Error(),
			})
			return
		}

		// 4. Return 201 Created
		writeJSON(w, http.StatusCreated, resp)
	}
}

// writeJSON is a small helper for JSON responses.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}
