package docverification

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultChecklistItems returns the initial credit memo checklist for a case,
// pre-populated with NOT_CHECKED status for each section.
func defaultChecklistItems() []model.ChecklistItem {
	sections := []struct {
		itemCode string
		label    string
		section  string
	}{
		{"BORROWER_IDENTITY", "Borrower identity matches application", "IDENTITY"},
		{"INCOME_CONSISTENCY", "Stated income consistent with supporting documents", "INCOME"},
		{"EMPLOYMENT_VERIFICATION", "Employment status confirmed", "INCOME"},
		{"PROPERTY_VALUE_MATCH", "Security property value matches valuation", "SECURITY"},
		{"LVR_COMPLIANCE", "LVR within policy limits", "SECURITY"},
		{"LOAN_AMOUNT_CORRECT", "Loan amount matches credit memo", "LOAN"},
		{"RATE_CORRECT", "Interest rate and rate type correct", "LOAN"},
		{"REPAYMENT_TYPE_CORRECT", "Repayment type matches application", "LOAN"},
		{"CONDITIONS_MET", "All pre-settlement conditions noted", "CONDITIONS"},
		{"AML_KYC_COMPLETE", "AML/KYC checks completed", "COMPLIANCE"},
		{"RESPONSIBLE_LENDING_CHECK", "Responsible lending obligations satisfied", "COMPLIANCE"},
	}

	items := make([]model.ChecklistItem, len(sections))
	for i, s := range sections {
		items[i] = model.ChecklistItem{
			ItemCode: s.itemCode,
			Label:    s.label,
			Section:  s.section,
			Status:   model.ChecklistItemNotChecked,
		}
	}
	return items
}

// InitialiseCreditMemoChecklist creates the checklist for a given task.
// Idempotent — only creates once per task_id.
func InitialiseCreditMemoChecklist(ctx context.Context, pool *pgxpool.Pool, taskID, caseID, tenantID string) (*model.CreditMemoChecklist, error) {
	items := defaultChecklistItems()
	itemsJSON, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("InitialiseCreditMemoChecklist: marshal items: %w", err)
	}

	var checklistID string
	err = pool.QueryRow(ctx, `
		INSERT INTO credit_memo_checklist (tenant_id, case_id, task_id, items, overall_status)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::jsonb, 'INCOMPLETE')
		ON CONFLICT (tenant_id, task_id) DO NOTHING
		RETURNING checklist_id::text
	`, tenantID, caseID, taskID, itemsJSON).Scan(&checklistID)
	if err != nil || checklistID == "" {
		// Already exists — load it.
		var existingItems []byte
		err = pool.QueryRow(ctx, `
			SELECT checklist_id::text, items
			FROM credit_memo_checklist
			WHERE tenant_id = $1::uuid AND task_id = $2::uuid
		`, tenantID, taskID).Scan(&checklistID, &existingItems)
		if err != nil {
			return nil, fmt.Errorf("InitialiseCreditMemoChecklist: load existing: %w", err)
		}
		if err := json.Unmarshal(existingItems, &items); err != nil {
			return nil, fmt.Errorf("InitialiseCreditMemoChecklist: unmarshal existing items: %w", err)
		}
	}

	return &model.CreditMemoChecklist{
		ChecklistID:   checklistID,
		TenantID:      tenantID,
		CaseID:        caseID,
		TaskID:        taskID,
		Items:         items,
		OverallStatus: model.ChecklistOverallIncomplete,
	}, nil
}

// UpdateChecklistItem updates a single item in the checklist and recomputes
// the overall status. Publishes CHECKLIST_COMPLETED if all items are checked.
func UpdateChecklistItem(
	ctx context.Context,
	pool *pgxpool.Pool,
	taskID, tenantID, itemCode, checkedByUserID string,
	status model.ChecklistItemStatus,
	discrepancyNote *string,
) (*model.CreditMemoChecklist, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("UpdateChecklistItem: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var caseID string
	var rawItems []byte
	now := time.Now().UTC()
	err = tx.QueryRow(ctx, `
		SELECT case_id::text, items
		FROM credit_memo_checklist
		WHERE task_id = $1::uuid AND tenant_id = $2::uuid
		FOR UPDATE
	`, taskID, tenantID).Scan(&caseID, &rawItems)
	if err != nil {
		return nil, fmt.Errorf("UpdateChecklistItem: load checklist: %w", err)
	}

	var items []model.ChecklistItem
	if err := json.Unmarshal(rawItems, &items); err != nil {
		return nil, fmt.Errorf("UpdateChecklistItem: unmarshal items: %w", err)
	}

	// Update the target item.
	found := false
	for i, item := range items {
		if item.ItemCode == itemCode {
			items[i].Status = status
			items[i].CheckedBy = checkedByUserID
			items[i].CheckedAt = &now
			items[i].DiscrepancyNote = discrepancyNote
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("UpdateChecklistItem: item %q not found in checklist", itemCode)
	}

	// Recompute overall status.
	hasIncomplete := false
	hasFail := false
	for _, item := range items {
		if item.Status == model.ChecklistItemNotChecked {
			hasIncomplete = true
		}
		if item.Status == model.ChecklistItemFail {
			hasFail = true
		}
	}
	var overall model.ChecklistOverallStatus
	switch {
	case hasIncomplete:
		overall = model.ChecklistOverallIncomplete
	case hasFail:
		overall = model.ChecklistOverallFailed
	default:
		overall = model.ChecklistOverallPassed
		// Check for N/A items — treat as PASSED_WITH_EXCEPTIONS.
		for _, item := range items {
			if item.Status == model.ChecklistItemNA {
				overall = model.ChecklistOverallPassedWithExceptions
				break
			}
		}
	}

	updatedJSON, _ := json.Marshal(items)
	var checklistID string
	var completedBy *string
	var completedAt *time.Time
	if !hasIncomplete && overall != model.ChecklistOverallIncomplete {
		completedBy = &checkedByUserID
		completedAt = &now
	}

	err = tx.QueryRow(ctx, `
		UPDATE credit_memo_checklist
		SET items        = $3::jsonb,
		    overall_status = $4,
		    completed_by  = CASE WHEN $5::bool THEN $6::uuid ELSE completed_by END,
		    completed_at  = CASE WHEN $5::bool THEN $7 ELSE completed_at END,
		    updated_at    = now()
		WHERE task_id = $1::uuid AND tenant_id = $2::uuid
		RETURNING checklist_id::text
	`, taskID, tenantID, updatedJSON, string(overall),
		completedAt != nil, checkedByUserID, completedAt,
	).Scan(&checklistID)
	if err != nil {
		return nil, fmt.Errorf("UpdateChecklistItem: update checklist: %w", err)
	}

	// If checklist just became complete, publish event.
	if completedAt != nil {
		payload := map[string]interface{}{
			"case_id":        caseID,
			"task_id":        taskID,
			"overall_status": overall,
			"completed_by":   checkedByUserID,
			"timestamp":      now,
		}
		if err := publishEventInTx(ctx, tx, caseID, model.EventChecklistCompleted, payload); err != nil {
			return nil, fmt.Errorf("UpdateChecklistItem: publish event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("UpdateChecklistItem: commit: %w", err)
	}

	slog.Debug("checklist item updated",
		"task_id", taskID,
		"item_code", itemCode,
		"status", status,
		"overall", overall)

	return &model.CreditMemoChecklist{
		ChecklistID:   checklistID,
		TenantID:      tenantID,
		CaseID:        caseID,
		TaskID:        taskID,
		Items:         items,
		OverallStatus: overall,
		CompletedBy:   completedBy,
		CompletedAt:   completedAt,
	}, nil
}

// GetChecklist loads the current checklist state for a task.
func GetChecklist(ctx context.Context, pool *pgxpool.Pool, taskID, tenantID string) (*model.CreditMemoChecklist, error) {
	var cl model.CreditMemoChecklist
	var rawItems []byte
	err := pool.QueryRow(ctx, `
		SELECT checklist_id::text, tenant_id::text, case_id::text, task_id::text,
		       checklist_version, items, overall_status,
		       completed_by::text, completed_at, created_at, updated_at
		FROM credit_memo_checklist
		WHERE task_id = $1::uuid AND tenant_id = $2::uuid
	`, taskID, tenantID).Scan(
		&cl.ChecklistID, &cl.TenantID, &cl.CaseID, &cl.TaskID,
		&cl.ChecklistVersion, &rawItems, &cl.OverallStatus,
		&cl.CompletedBy, &cl.CompletedAt, &cl.CreatedAt, &cl.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("GetChecklist: %w", err)
	}
	if err := json.Unmarshal(rawItems, &cl.Items); err != nil {
		return nil, fmt.Errorf("GetChecklist: unmarshal items: %w", err)
	}
	return &cl, nil
}

// IsChecklistRequiredForTask returns true if the task has requires_checklist in its config.
// Used to gate QA submission.
func IsChecklistComplete(ctx context.Context, pool *pgxpool.Pool, taskID, tenantID string) (bool, error) {
	var overallStatus string
	err := pool.QueryRow(ctx, `
		SELECT overall_status
		FROM credit_memo_checklist
		WHERE task_id = $1::uuid AND tenant_id = $2::uuid
	`, taskID, tenantID).Scan(&overallStatus)
	if err != nil {
		if isNoRows(err) {
			return false, nil // checklist not yet initialised
		}
		return false, fmt.Errorf("IsChecklistComplete: %w", err)
	}
	return overallStatus != string(model.ChecklistOverallIncomplete), nil
}
