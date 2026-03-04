package engine

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"workflow-engine/internal/multitenancy"
	"workflow-engine/internal/repository"

	"github.com/jackc/pgx/v5"
)

// RegisterWorkbasketHandlers attaches workbasket HTTP endpoints to the mux.
//
//	GET  /workbaskets                          — list baskets the caller is a member of
//	POST /workbaskets                          — create a new workbasket (admin)
//	GET  /workbaskets/{id}/tasks               — list pending tasks in a basket
//	GET  /workbaskets/{id}/members             — list current members of a basket
//	POST /workbaskets/{id}/members             — add a member (with optional expiry)
//	DELETE /workbaskets/{id}/members/{wid}     — remove a member
//	POST /tasks/{id}/claim                     — claim a task from a basket
func RegisterWorkbasketHandlers(mux *http.ServeMux, repo *repository.Repository) {
	mux.HandleFunc("GET /workbaskets", handleListWorkbaskets(repo))
	mux.HandleFunc("POST /workbaskets", handleCreateWorkbasket(repo))
	mux.HandleFunc("GET /workbaskets/{id}/tasks", handleListWorkbasketTasks(repo))
	mux.HandleFunc("GET /workbaskets/{id}/members", handleListWorkbasketMembers(repo))
	mux.HandleFunc("POST /workbaskets/{id}/members", handleAddWorkbasketMember(repo))
	mux.HandleFunc("DELETE /workbaskets/{id}/members/{wid}", handleRemoveWorkbasketMember(repo))
	mux.HandleFunc("POST /tasks/{id}/claim", handleClaimTask(repo))
}

// handleListWorkbaskets serves GET /workbaskets.
// Returns baskets that the authenticated worker is a current (non-expired) member of,
// with queue depth and oldest task age. ESCALATION baskets are always listed first.
func handleListWorkbaskets(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workerID := multitenancy.UserIDFromContext(r.Context())

		baskets, err := repo.ListWorkbaskets(r.Context(), workerID)
		if err != nil {
			slog.Error("GET /workbaskets failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"message": "failed to list workbaskets",
			})
			return
		}

		writeJSON(w, http.StatusOK, baskets)
	}
}

// handleCreateWorkbasket serves POST /workbaskets.
// Body: { "name": "...", "type": "GENERAL|SPECIALIST|ESCALATION", "strategy": "ROUND_ROBIN|..." }
func handleCreateWorkbasket(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in repository.CreateWorkbasketInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "invalid request body",
			})
			return
		}

		if in.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "name is required",
			})
			return
		}

		validTypes := map[string]bool{"GENERAL": true, "SPECIALIST": true, "ESCALATION": true}
		if !validTypes[in.Type] {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "type must be one of GENERAL, SPECIALIST, ESCALATION",
			})
			return
		}

		id, err := repo.CreateWorkbasket(r.Context(), in)
		if err != nil {
			slog.Error("POST /workbaskets failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"message": "failed to create workbasket",
			})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]string{
			"id":      id,
			"message": "workbasket created",
		})
	}
}

// handleListWorkbasketTasks serves GET /workbaskets/{id}/tasks.
// Returns unclaimed tasks in the specified workbasket.
func handleListWorkbasketTasks(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" || !isValidUUID(id) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "valid workbasket id is required",
			})
			return
		}

		tasks, err := repo.ListWorkbasketTasks(r.Context(), id)
		if err != nil {
			slog.Error("GET /workbaskets/{id}/tasks failed", "error", err, "workbasket_id", id)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"message": "failed to list workbasket tasks",
			})
			return
		}

		writeJSON(w, http.StatusOK, tasks)
	}
}

// handleListWorkbasketMembers serves GET /workbaskets/{id}/members.
// Returns current (non-expired) members of the basket.
func handleListWorkbasketMembers(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" || !isValidUUID(id) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "valid workbasket id is required",
			})
			return
		}

		members, err := repo.ListWorkbasketMembers(r.Context(), id)
		if err != nil {
			slog.Error("GET /workbaskets/{id}/members failed", "error", err, "workbasket_id", id)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"message": "failed to list workbasket members",
			})
			return
		}

		writeJSON(w, http.StatusOK, members)
	}
}

// handleAddWorkbasketMember serves POST /workbaskets/{id}/members.
// Body: { "workerId": "...", "expiresAt": "2026-04-01T00:00:00Z" (optional) }
func handleAddWorkbasketMember(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" || !isValidUUID(id) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "valid workbasket id is required",
			})
			return
		}

		var body struct {
			WorkerID  string     `json:"workerId"`
			ExpiresAt *time.Time `json:"expiresAt,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "invalid request body",
			})
			return
		}
		if body.WorkerID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "workerId is required",
			})
			return
		}

		in := repository.AddWorkbasketMemberInput{
			WorkerID:  body.WorkerID,
			ExpiresAt: body.ExpiresAt,
		}
		if err := repo.AddWorkbasketMember(r.Context(), id, in); err != nil {
			slog.Error("POST /workbaskets/{id}/members failed", "error", err, "workbasket_id", id)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"message": "failed to add member",
			})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]string{
			"message":      "member added",
			"workbasketId": id,
			"workerId":     body.WorkerID,
		})
	}
}

// handleRemoveWorkbasketMember serves DELETE /workbaskets/{id}/members/{wid}.
func handleRemoveWorkbasketMember(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		wid := r.PathValue("wid")
		if id == "" || !isValidUUID(id) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "valid workbasket id is required",
			})
			return
		}
		if wid == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "worker id is required",
			})
			return
		}

		if err := repo.RemoveWorkbasketMember(r.Context(), id, wid); err != nil {
			if err == pgx.ErrNoRows {
				writeJSON(w, http.StatusNotFound, map[string]string{
					"message": "membership not found",
				})
				return
			}
			slog.Error("DELETE /workbaskets/{id}/members/{wid} failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"message": "failed to remove member",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"message":      "member removed",
			"workbasketId": id,
			"workerId":     wid,
		})
	}
}

// handleClaimTask serves POST /tasks/{id}/claim.
// Claims a task from a workbasket for the authenticated user.
func handleClaimTask(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" || !isValidUUID(id) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "valid task id is required",
			})
			return
		}

		// Use the authenticated user or fall back to a default worker ID.
		workerID := multitenancy.UserIDFromContext(r.Context())
		if workerID == "" {
			workerID = "system"
		}

		err := repo.ClaimTaskFromWorkbasket(r.Context(), id, workerID)
		if err != nil {
			if err == pgx.ErrNoRows {
				writeJSON(w, http.StatusConflict, map[string]string{
					"message": "task is already claimed or does not exist",
				})
				return
			}
			slog.Error("POST /tasks/{id}/claim failed", "error", err, "task_id", id)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"message": "failed to claim task",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"message": "task claimed successfully",
			"taskId":  id,
		})
	}
}
