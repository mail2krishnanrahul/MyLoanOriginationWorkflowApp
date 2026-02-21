package integration

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jmoiron/sqlx"
)

// RegisterDealIngestionHandlers mounts business lending ingestion endpoints.
func RegisterDealIngestionHandlers(mux *http.ServeMux, db *sqlx.DB, logger *slog.Logger) {
	if mux == nil || db == nil {
		return
	}
	service := NewDealIngestionService(db, logger)
	if logger == nil {
		logger = slog.Default()
	}

	mux.HandleFunc("POST /ingest/deals", func(w http.ResponseWriter, r *http.Request) {
		var req IngestDealRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeProblemDetail(w, ProblemDetail{
				Title:  "Snapshot Schema Validation Failure",
				Status: 422,
				Detail: "invalid request body: " + err.Error(),
			})
			return
		}
		if req.SourceSystem == "" {
			req.SourceSystem = strings.TrimSpace(r.Header.Get("X-Source-System"))
		}

		idempotencyKey := strings.TrimSpace(r.Header.Get("X-Idempotency-Key"))
		status, response, problem := service.IngestDeal(r.Context(), req, idempotencyKey)
		if problem != nil {
			logger.Warn("POST /ingest/deals rejected", "status", problem.Status, "title", problem.Title)
			writeProblemDetail(w, *problem)
			return
		}

		writeJSON(w, status, response)
	})

	mux.HandleFunc("GET /ingest/deals/{dealId}/history", func(w http.ResponseWriter, r *http.Request) {
		dealID := strings.TrimSpace(r.PathValue("dealId"))
		result, problem := service.GetIngestionHistory(r.Context(), dealID)
		if problem != nil {
			writeProblemDetail(w, *problem)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

func writeProblemDetail(w http.ResponseWriter, problem ProblemDetail) {
	if problem.Status == 0 {
		problem.Status = http.StatusInternalServerError
	}
	writeJSON(w, problem.Status, problem)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
