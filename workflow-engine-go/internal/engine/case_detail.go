package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"
)

// ---------------------------------------------------------------------------
// GetCaseDetail — assembles the full case picture from parallel queries.
// Uses 5 queries total (no N+1):
//   1. Case header (JOIN case_types)
//   2. Stage history
//   3. Sub-cases
//   4. Tasks for current stage (single query, group in Go)
//   5. Overall task progress (COUNT with FILTER)
// ---------------------------------------------------------------------------

func GetCaseDetail(
	ctx context.Context,
	repo *repository.Repository,
	caseID string,
) (CaseDetail, error) {

	// 1. Case header — look up by UUID (the frontend navigates by case.id)
	header, err := repo.GetCaseByID(ctx, caseID)
	if err != nil {
		return CaseDetail{}, fmt.Errorf("case not found: %w", err)
	}

	detail := CaseDetail{
		CaseID:          header.CaseID,
		ReferenceNumber: header.ReferenceNumber,
		CaseType:        header.CaseTypeCode,
		CaseTypeVersion: header.CaseTypeVersion,
		Status:          header.Status,
		CurrentStage:    header.CurrentStage,
		AssignedTo:      header.AssignedTo,
		Metadata:        header.Metadata,
		CreatedAt:       header.CreatedAt,
		UpdatedAt:       header.UpdatedAt,
		CompletedAt:     header.CompletedAt,
		// SLA defaults — real computation would require sla_definitions table
		SLAStatus:           "ON_TRACK",
		SLARemainingMinutes: 2880, // 48h default
	}

	// Extract borrowerName, priority, loanAmount, productType from metadata JSON
	if len(header.Metadata) > 0 {
		var meta map[string]interface{}
		if err := json.Unmarshal(header.Metadata, &meta); err == nil {
			if v, ok := meta["borrowerName"].(string); ok && v != "" {
				detail.BorrowerName = &v
			}
			if v, ok := meta["priority"].(string); ok && v != "" {
				detail.Priority = &v
			}
			if v, ok := meta["productType"].(string); ok && v != "" {
				detail.ProductType = &v
			}
			if v, ok := meta["loanAmount"].(float64); ok {
				detail.LoanAmount = v
			}
		}
	}

	// Map classifier-set summary fields
	detail.Complexity = header.Complexity
	detail.IsVIP = header.IsVIP
	detail.Channel = header.Channel
	detail.Officer = header.OfficerName
	if header.TargetCloseDate != nil {
		s := header.TargetCloseDate.Format("2006-01-02")
		detail.TargetCloseDate = &s
	}

	// 2. Stage history
	transitions, err := repo.GetStageHistory(ctx, header.CaseID)
	if err != nil {
		slog.Warn("failed to load stage history", "error", err, "case_id", header.CaseID)
	} else {
		detail.StageHistory = make([]StageHistoryEntry, 0, len(transitions))
		for _, t := range transitions {
			detail.StageHistory = append(detail.StageHistory, StageHistoryEntry{
				FromStage:    t.FromStageCode,
				ToStage:      t.ToStageCode,
				IsRegression: t.IsRegression,
				Reason:       t.RegressionReason,
				TriggeredBy:  t.TriggeredBy,
				TransitionAt: t.TransitionedAt,
			})
		}
	}

	// 3. Sub-cases
	subCases, err := repo.GetSubCases(ctx, header.CaseID)
	if err != nil {
		slog.Warn("failed to load sub-cases", "error", err, "case_id", header.CaseID)
	} else {
		detail.SubCases = make([]SubCaseSummary, 0, len(subCases))
		for _, sc := range subCases {
			detail.SubCases = append(detail.SubCases, SubCaseSummary{
				CaseID:          sc.CaseID,
				ReferenceNumber: sc.ReferenceNumber,
				CaseTypeCode:    sc.CaseTypeCode,
				Status:          sc.Status,
				CurrentStage:    sc.CurrentStage,
			})
		}
	}

	// 4. Tasks for current stage — single query, grouped in Go
	if header.CurrentStage != nil {
		taskRows, err := repo.GetTasksForCurrentStage(ctx, header.CaseID, *header.CurrentStage)
		if err != nil {
			slog.Warn("failed to load tasks", "error", err, "case_id", header.CaseID)
		} else {
			detail.Activities = groupTasksByActivity(taskRows)
		}
	}

	// 5. Overall progress
	progress, err := repo.GetOverallTaskProgress(ctx, header.CaseID)
	if err != nil {
		slog.Warn("failed to load progress", "error", err, "case_id", header.CaseID)
	} else {
		detail.TotalTasks = progress.TotalTasks
		detail.CompletedTasks = progress.CompletedTasks
		if progress.TotalTasks > 0 {
			detail.PercentComplete = float64(progress.CompletedTasks) / float64(progress.TotalTasks) * 100.0
		}
	}

	// 6. Deal snapshot from case_deal_links (for Deal 360 view)
	dealSnapshot, err := repo.GetDealSnapshot(ctx, header.CaseID)
	if err != nil {
		slog.Debug("no deal snapshot for case", "case_id", header.CaseID, "error", err)
	} else if len(dealSnapshot) > 0 {
		detail.DealSnapshot = dealSnapshot
	}

	// Default empty slices for JSON (avoid null)
	if detail.StageHistory == nil {
		detail.StageHistory = []StageHistoryEntry{}
	}
	if detail.SubCases == nil {
		detail.SubCases = []SubCaseSummary{}
	}
	if detail.Activities == nil {
		detail.Activities = []ActivitySummary{}
	}

	return detail, nil
}

// groupTasksByActivity groups flat task rows into ActivitySummary slices.
func groupTasksByActivity(rows []repository.TaskStatusRow) []ActivitySummary {
	activityMap := make(map[string]*ActivitySummary)
	var activityOrder []string

	for _, r := range rows {
		a, exists := activityMap[r.ActivityCode]
		if !exists {
			a = &ActivitySummary{
				ActivityCode: r.ActivityCode,
				Tasks:        []TaskSummary{},
				StatusCounts: make(map[string]int),
			}
			activityMap[r.ActivityCode] = a
			activityOrder = append(activityOrder, r.ActivityCode)
		}

		a.Tasks = append(a.Tasks, TaskSummary{
			TaskID:             r.TaskID,
			TaskDefinitionCode: r.TaskDefinitionCode,
			Status:             r.Status,
			Priority:           r.Priority,
			AssignedService:    r.AssignedService,
			DueAt:              r.DueAt,
			CompletedAt:        r.CompletedAt,
		})

		a.StatusCounts[r.Status]++
		a.Total++
		if r.Status == string(model.TaskStatusDone) {
			a.Completed++
		}
	}

	// Preserve insertion order
	result := make([]ActivitySummary, 0, len(activityOrder))
	for _, code := range activityOrder {
		result = append(result, *activityMap[code])
	}
	return result
}

// ---------------------------------------------------------------------------
// HTTP handler: GET /cases/{reference_number}
// ---------------------------------------------------------------------------

// RegisterCaseDetailHandler attaches the case detail and task sub-route handlers.
func RegisterCaseDetailHandler(mux *http.ServeMux, repo *repository.Repository) {
	// GET /cases/{id}  — full case detail (fetched by UUID)
	mux.HandleFunc("GET /cases/{id}", handleGetCaseDetail(repo))
	// /cases/{id}/summary — method dispatch: PATCH = classifier step
	// Using an unqualified pattern avoids Go mux pattern conflicts with
	// the parent GET /cases/{id} route.
	mux.HandleFunc("/cases/{id}/summary", handleCaseSummary(repo))
	// GET /cases/{id}/tasks — tasks for the case (already embedded in detail,
	// but the frontend requests this endpoint separately)
	mux.HandleFunc("GET /cases/{id}/tasks", handleGetCaseTasks(repo))
}

func handleGetCaseDetail(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 3 {
				id = parts[len(parts)-1]
			}
		}

		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "case id is required",
			})
			return
		}

		detail, err := GetCaseDetail(r.Context(), repo, id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, detail)
	}
}

// handleCaseSummary dispatches /cases/{id}/summary by method.
// Using a method-agnostic pattern on the mux avoids the Go 1.22 pattern
// conflict where PATCH /cases/{id}/summary could be shadowed by GET /cases/{id}.
func handleCaseSummary(repo *repository.Repository) http.HandlerFunc {
	patch := handlePatchCaseSummary(repo)
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			patch(w, r)
		case http.MethodOptions:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Header().Set("Allow", "PATCH, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "method not allowed",
			})
		}
	}
}

// ---------------------------------------------------------------------------
// PATCH /cases/{id}/summary — classifier step
// ---------------------------------------------------------------------------

// complexitySLADays maps each complexity tier to its target calendar-day window.
var complexitySLADays = map[string]int{
	"SIMPLE":       10,
	"STANDARD_1":   20,
	"STANDARD_2":   30,
	"COMPLEX":      45,
	"NON_STANDARD": 60,
}

// patchSummaryRequest is the wire shape for PATCH /cases/{id}/summary.
type patchSummaryRequest struct {
	Complexity      *string  `json:"complexity"`
	IsVIP           *bool    `json:"isVip"`
	TargetCloseDate *string  `json:"targetCloseDate"` // YYYY-MM-DD, optional override
	LoanAmount      *float64 `json:"loanAmount"`
	ProductType     *string  `json:"productType"`
	Channel         *string  `json:"channel"`
	Officer         *string  `json:"officer"`
	BorrowerName    *string  `json:"borrowerName"`
}

func handlePatchCaseSummary(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "case id required"})
			return
		}

		var req patchSummaryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}

		// Build repo patch payload
		patch := repository.PatchSummaryPayload{
			Complexity:   req.Complexity,
			IsVIP:        req.IsVIP,
			LoanAmount:   req.LoanAmount,
			ProductType:  req.ProductType,
			Channel:      req.Channel,
			OfficerName:  req.Officer,
			BorrowerName: req.BorrowerName,
		}

		// Auto-compute targetCloseDate from complexity SLA when no explicit date given
		if req.TargetCloseDate != nil && *req.TargetCloseDate != "" {
			t, err := time.Parse("2006-01-02", *req.TargetCloseDate)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "targetCloseDate must be YYYY-MM-DD"})
				return
			}
			patch.TargetCloseDate = &t
		} else if req.Complexity != nil && *req.Complexity != "" {
			if days, ok := complexitySLADays[*req.Complexity]; ok {
				t := time.Now().UTC().AddDate(0, 0, days)
				patch.TargetCloseDate = &t
			}
		}

		if err := repo.UpdateCaseSummary(r.Context(), id, patch); err != nil {
			slog.Error("PATCH /cases/{id}/summary failed", "error", err, "case_id", id)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update case"})
			return
		}

		// Return the refreshed case detail so the frontend can update in one round-trip
		detail, err := GetCaseDetail(r.Context(), repo, id)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}

// taskSummaryJSON is the wire shape that matches the frontend's TaskSummary type.
type taskSummaryJSON struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	ActivityName string  `json:"activityName,omitempty"`
	Status       string  `json:"status"`
	Priority     string  `json:"priority"`
	DueAt        *string `json:"dueAt,omitempty"`
	SLAStatus    string  `json:"slaStatus"`
}

// priorityIntToString maps the integer priority stored in DB to the string enum the frontend expects.
func priorityIntToString(p int) string {
	switch {
	case p >= 80:
		return "CRITICAL"
	case p >= 60:
		return "HIGH"
	case p >= 40:
		return "NORMAL"
	default:
		return "LOW"
	}
}

// handleGetCaseTasks serves GET /cases/{id}/tasks.
// Returns a flat TaskSummary[] array matching the frontend contract.
func handleGetCaseTasks(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "case id is required",
			})
			return
		}

		// Load header to get current stage
		header, err := repo.GetCaseByID(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": err.Error(),
			})
			return
		}

		tasks := []taskSummaryJSON{}
		if header.CurrentStage != nil {
			taskRows, err := repo.GetTasksForCurrentStage(r.Context(), id, *header.CurrentStage)
			if err != nil {
				slog.Error("GET /cases/{id}/tasks failed", "error", err, "case_id", id)
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error": "failed to load tasks",
				})
				return
			}

			for _, t := range taskRows {
				ts := taskSummaryJSON{
					ID:           t.TaskID,
					Name:         t.TaskDefinitionCode,
					ActivityName: t.ActivityCode,
					Status:       t.Status,
					Priority:     priorityIntToString(t.Priority),
					SLAStatus:    "ON_TRACK", // default; real SLA computed from task.due_at
				}
				if t.DueAt != nil {
					formatted := t.DueAt.Format("2006-01-02T15:04:05Z")
					ts.DueAt = &formatted
				}
				tasks = append(tasks, ts)
			}
		}

		writeJSON(w, http.StatusOK, tasks)
	}
}

// ---------------------------------------------------------------------------
// Caching Strategy Notes (for 100k cases/day read patterns)
// ---------------------------------------------------------------------------
//
// FIELDS TO CACHE (change infrequently relative to reads):
//   - CaseType config: immutable per version — cache aggressively (TTL=1h
//     or until version change). Key: case_type_id.
//   - Stage history: append-only — cache with invalidation on new transition.
//   - Sub-cases list: changes rarely — cache with short TTL (30s) or
//     invalidate on CASE_CREATED event that references parent_case_id.
//
// FIELDS NOT TO CACHE (change with every task update):
//   - Task list / status counts / percent_complete — too volatile.
//     However, the aggregated progress query is cheap (indexed scan).
//
// RECOMMENDED STRATEGY (for 100k cases / 1M events/day):
//
//   1. Application-level LRU cache (e.g. github.com/dgraph-io/ristretto):
//      - CaseType configs: key=caseTypeID, TTL=1h
//      - Case headers: key=referenceNumber, TTL=10s (short, covers burst reads)
//
//   2. HTTP-level caching:
//      - Set Cache-Control: private, max-age=5 for active cases
//      - Set Cache-Control: public, max-age=3600 for COMPLETED cases
//        (they never change)
//      - ETag based on case.updated_at for conditional requests
//
//   3. Materialised view (if >500 reads/sec per case):
//      - CREATE MATERIALIZED VIEW case_progress_mv AS
//        SELECT case_id, COUNT(*) FILTER (...) ...
//      - Refresh on TASK_COMPLETED events (async, via outbox consumer)
//
//   4. Read replica routing:
//      - Route GET /cases/* to a read replica to offload the primary.
//      - Acceptable staleness: <2s (async replication lag).
