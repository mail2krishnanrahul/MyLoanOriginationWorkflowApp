package engine

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"workflow-engine/internal/multitenancy"
	"workflow-engine/internal/repository"
)

// RegisterCaseActionsHandler attaches POST /cases/{id}/actions to the mux.
func RegisterCaseActionsHandler(mux *http.ServeMux, repo *repository.Repository) {
	mux.HandleFunc("POST /cases/{id}/actions", handleCaseAction(repo))
}

type caseActionRequest struct {
	Action         string `json:"action"`
	Reason         string `json:"reason"`
	FourEyesToken  string `json:"fourEyesToken"`
}

func handleCaseAction(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caseID := r.PathValue("id")
		if caseID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "case id is required"})
			return
		}

		tenantID, _ := multitenancy.TenantFromContext(r.Context())
		if tenantID == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "tenant context required"})
			return
		}

		userID := multitenancy.UserIDFromContext(r.Context())

		var req caseActionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		eng := NewEngine(repo, nil, 0)

		var err error
		switch req.Action {
		case "RELEASE":
			err = eng.ReleaseCase(r.Context(), caseID, userID)
		case "SUSPEND":
			err = eng.SuspendCase(r.Context(), caseID, req.Reason, nil)
		case "WITHDRAW":
			err = eng.WithdrawCase(r.Context(), caseID, req.Reason)
		case "EMERGENCY_CLOSE":
			err = eng.EmergencyClose(r.Context(), caseID, req.Reason, userID, req.FourEyesToken)
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}

		if err != nil {
			slog.Error("POST /cases/{id}/actions failed", "action", req.Action, "case_id", caseID, "error", err)
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
