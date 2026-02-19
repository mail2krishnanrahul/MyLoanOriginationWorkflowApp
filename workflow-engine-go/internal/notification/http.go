package notification

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// RegisterNotificationHandlers registers notification endpoints.
func RegisterNotificationHandlers(mux *http.ServeMux, db *sqlx.DB, logger *slog.Logger) {
	if mux == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	mux.HandleFunc("POST /notifications/{id}/acknowledge", handleAcknowledgeNotification(db, logger))
	mux.HandleFunc("GET /cases/{case_id}/notifications", handleGetNotificationHistory(db, logger))
	mux.HandleFunc("GET /cases/{case_id}/correspondence-summary", handleGetCorrespondenceSummary(db, logger))
}

func handleAcknowledgeNotification(db *sqlx.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notificationID := strings.TrimSpace(r.PathValue("id"))
		if notificationID == "" {
			writeNotificationJSON(w, http.StatusBadRequest, map[string]string{"error": "notification id is required"})
			return
		}

		ackAt, already, err := AcknowledgeNotification(r.Context(), db, notificationID)
		if err != nil {
			logger.Error("POST /notifications/{id}/acknowledge failed", "notification_id", notificationID, "error", err)
			writeNotificationJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}

		writeNotificationJSON(w, http.StatusOK, map[string]interface{}{
			"notification_id":      notificationID,
			"acknowledged_at":      ackAt,
			"already_acknowledged": already,
		})
	}
}

func writeNotificationJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func handleGetNotificationHistory(db *sqlx.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caseID := strings.TrimSpace(r.PathValue("case_id"))
		if caseID == "" {
			writeNotificationJSON(w, http.StatusBadRequest, map[string]string{"error": "case_id is required"})
			return
		}

		history, err := GetNotificationHistory(r.Context(), db, caseID)
		if err != nil {
			logger.Error("GET /cases/{case_id}/notifications failed", "case_id", caseID, "error", err)
			writeNotificationJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch notification history"})
			return
		}

		writeNotificationJSON(w, http.StatusOK, history)
	}
}

func handleGetCorrespondenceSummary(db *sqlx.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caseID := strings.TrimSpace(r.PathValue("case_id"))
		if caseID == "" {
			writeNotificationJSON(w, http.StatusBadRequest, map[string]string{"error": "case_id is required"})
			return
		}

		// Refresh view first (optional, but ensures freshness for the dashboard)
		if err := RefreshCorrespondenceSummary(r.Context(), db); err != nil {
			logger.Warn("failed to refresh correspondence summary", "error", err)
			// Continue, serve stale data if available
		}

		summary, err := GetCorrespondenceSummary(r.Context(), db, caseID)
		if err != nil {
			logger.Error("GET /cases/{case_id}/correspondence-summary failed", "case_id", caseID, "error", err)
			writeNotificationJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch correspondence summary"})
			return
		}
		if summary == nil {
			// No notifications sent yet, return empty structure with caseID
			summary = &model.CorrespondenceSummary{
				CaseID:        caseID,
				SentByChannel: json.RawMessage("{}"),
				FailedReasons: json.RawMessage("{}"),
			}
		}

		writeNotificationJSON(w, http.StatusOK, summary)
	}
}
