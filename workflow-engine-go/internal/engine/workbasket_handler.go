package engine

import (
	"log/slog"
	"net/http"

	"workflow-engine/internal/multitenancy"
	"workflow-engine/internal/repository"

	"github.com/jackc/pgx/v5"
)

// RegisterWorkbasketHandlers attaches workbasket HTTP endpoints to the mux.
func RegisterWorkbasketHandlers(mux *http.ServeMux, repo *repository.Repository) {
	mux.HandleFunc("GET /workbaskets", handleListWorkbaskets(repo))
	mux.HandleFunc("GET /workbaskets/{id}/tasks", handleListWorkbasketTasks(repo))
	mux.HandleFunc("POST /tasks/{id}/claim", handleClaimTask(repo))
}

// handleListWorkbaskets serves GET /workbaskets.
// Returns all workbaskets with queue depth and oldest task age.
func handleListWorkbaskets(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		baskets, err := repo.ListWorkbaskets(r.Context())
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
