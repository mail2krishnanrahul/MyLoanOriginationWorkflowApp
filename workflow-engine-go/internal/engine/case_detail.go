package engine

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

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
	referenceNumber string,
) (CaseDetail, error) {

	// 1. Case header
	header, err := repo.GetCaseByReference(ctx, referenceNumber)
	if err != nil {
		return CaseDetail{}, fmt.Errorf("case not found: %w", err)
	}

	detail := CaseDetail{
		CaseID:          header.CaseID,
		ReferenceNumber: header.ReferenceNumber,
		CaseTypeCode:    header.CaseTypeCode,
		CaseTypeVersion: header.CaseTypeVersion,
		Status:          header.Status,
		CurrentStage:    header.CurrentStage,
		AssignedTo:      header.AssignedTo,
		Metadata:        header.Metadata,
		CreatedAt:       header.CreatedAt,
		UpdatedAt:       header.UpdatedAt,
		CompletedAt:     header.CompletedAt,
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

// RegisterCaseDetailHandler attaches the case detail read handler.
func RegisterCaseDetailHandler(mux *http.ServeMux, repo *repository.Repository) {
	mux.HandleFunc("GET /cases/{ref}", handleGetCaseDetail(repo))
}

func handleGetCaseDetail(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if ref == "" {
			// Fallback for older Go versions: parse from path
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 3 {
				ref = parts[len(parts)-1]
			}
		}

		if ref == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "reference_number is required",
			})
			return
		}

		detail, err := GetCaseDetail(r.Context(), repo, ref)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, detail)
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
