package docverification

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GetNextCaseResult is returned by GetNextCase.
type GetNextCaseResult struct {
	CaseID          string               `json:"case_id"`
	ReferenceNumber string               `json:"reference_number"`
	Complexity      model.CaseComplexity `json:"complexity"`
	RequiredSkills  []model.SkillCode    `json:"required_skills"`
	TotalScore      float64              `json:"total_score"`
}

// GetNextCase returns the highest-priority unallocated case for the given officer.
// Uses a weighted score formula:
//
//	score = (SLA_urgency * 40) + (complexity * 30) + (skill_match * 20) + (age * 10) + (VIP * 10)
//
// Selects FOR UPDATE SKIP LOCKED to prevent double-allocation under concurrency.
func GetNextCase(ctx context.Context, pool *pgxpool.Pool, officerUserID string, officerSkills []model.SkillCode) (*GetNextCaseResult, error) {
	if officerUserID == "" {
		return nil, fmt.Errorf("GetNextCase: officerUserID is required")
	}

	skillStrs := make([]string, len(officerSkills))
	for i, s := range officerSkills {
		skillStrs[i] = string(s)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetNextCase: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var caseID, refNum, complexity, skillsCSV string
	var slaRemainingHrs float64
	// Weighted GetNext query — a single SELECT FOR UPDATE SKIP LOCKED.
	// Score breakdown:
	//   SLA urgency (40%): inverse proportion of remaining hours (capped at 120h).
	//   Complexity (30%): map NON_STANDARD=50, STANDARD_3=40, etc.
	//   Skill match (20%): 1 if officer has ANY matching skill.
	//   Age (10%): case age in hours (capped at 72h).
	//   VIP (10%): metadata flag is_vip.
	err = tx.QueryRow(ctx, `
		WITH ranked AS (
			SELECT
				c.id::text,
				c.reference_number,
				COALESCE(c.case_complexity, 'SIMPLE') AS complexity,
				COALESCE(c.required_skills::text, '') AS skills_csv,
				EXTRACT(EPOCH FROM (COALESCE(c.case_due_at, c.created_at + interval '5 days') - now())) / 3600.0 AS sla_remaining_hours,
				EXTRACT(EPOCH FROM (now() - c.created_at)) / 3600.0 AS age_hours,
				COALESCE((c.metadata->>'is_vip')::bool, false) AS is_vip,
				-- Score formula
				(
					-- SLA urgency (40): higher urgency = higher score
					CASE
						WHEN EXTRACT(EPOCH FROM (COALESCE(c.case_due_at, c.created_at + interval '5 days') - now())) / 3600.0 <= 0 THEN 40
						ELSE GREATEST(0, 40 - (EXTRACT(EPOCH FROM (COALESCE(c.case_due_at, c.created_at + interval '5 days') - now())) / 3600.0 / 3.0))
					END
					+
					-- Complexity (30)
					CASE COALESCE(c.case_complexity, 'SIMPLE')
						WHEN 'NON_STANDARD' THEN 30
						WHEN 'STANDARD_3'   THEN 24
						WHEN 'STANDARD_2'   THEN 18
						WHEN 'STANDARD_1'   THEN 12
						ELSE 6
					END
					+
					-- Skill match (20): officer skills ∩ required_skills != ∅
					CASE
						WHEN c.required_skills IS NULL THEN 10
						WHEN c.required_skills::text[] && $2::text[] THEN 20
						ELSE 0
					END
					+
					-- Age bonus (10)
					LEAST(10, EXTRACT(EPOCH FROM (now() - c.created_at)) / 3600.0 / 7.2)
					+
					-- VIP bonus (10)
					CASE WHEN COALESCE((c.metadata->>'is_vip')::bool, false) THEN 10 ELSE 0 END
				) AS total_score
			FROM cases c
			JOIN case_types ct ON ct.id = c.case_type_id
			WHERE ct.code = $1
			  AND c.status = 'OPEN'
			  AND c.assigned_user_id IS NULL
			  AND c.current_stage_code = $3
			ORDER BY total_score DESC
			LIMIT 1
		)
		SELECT id, reference_number, complexity, skills_csv, sla_remaining_hours
		FROM ranked
		FOR UPDATE SKIP LOCKED
	`, CaseTypeCode, skillStrs, StageAllocation).Scan(
		&caseID, &refNum, &complexity, &skillsCSV, &slaRemainingHrs)
	if err != nil {
		if isNoRows(err) {
			return nil, nil // no eligible case
		}
		return nil, fmt.Errorf("GetNextCase: select: %w", err)
	}

	// Assign directly.
	_, err = tx.Exec(ctx, `
		UPDATE cases
		SET assigned_user_id = $2::uuid,
		    metadata         = metadata || jsonb_build_object(
		        'allocated_at',  now()::text,
		        'allocated_to',  $2::text
		    ),
		    updated_at       = now(),
		    row_version      = row_version + 1
		WHERE id = $1::uuid
	`, caseID, officerUserID)
	if err != nil {
		return nil, fmt.Errorf("GetNextCase: assign: %w", err)
	}

	// Parse skills.
	var skills []model.SkillCode
	for _, s := range strings.Split(skillsCSV, ",") {
		if s != "" {
			skills = append(skills, model.SkillCode(s))
		}
	}

	payload := map[string]interface{}{
		"case_id":             caseID,
		"allocated_to":        officerUserID,
		"allocation_method":   "GET_NEXT",
		"complexity":          complexity,
		"sla_remaining_hours": slaRemainingHrs,
		"timestamp":           time.Now().UTC(),
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventCaseAllocated, payload); err != nil {
		return nil, fmt.Errorf("GetNextCase: publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("GetNextCase: commit: %w", err)
	}

	slog.Info("case allocated via GetNext",
		"case_id", caseID,
		"officer_id", officerUserID,
		"complexity", complexity)

	return &GetNextCaseResult{
		CaseID:          caseID,
		ReferenceNumber: refNum,
		Complexity:      model.CaseComplexity(complexity),
		RequiredSkills:  skills,
	}, nil
}

// ManuallyAllocateCase assigns a specific case to a specific loan officer.
// Validates that the officer holds at least one of the required skills unless
// the override flag is set (team lead privilege).
func ManuallyAllocateCase(
	ctx context.Context,
	pool *pgxpool.Pool,
	caseID, targetUserID, allocatedByUserID string,
	skillOverride bool,
) error {
	if caseID == "" || targetUserID == "" {
		return fmt.Errorf("ManuallyAllocateCase: caseID and targetUserID are required")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ManuallyAllocateCase: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Load case skills and current assignee.
	var requiredSkillsArray []string
	var currentAssignee *string
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(required_skills, '{}'), assigned_user_id::text
		FROM cases WHERE id = $1::uuid FOR UPDATE
	`, caseID).Scan(&requiredSkillsArray, &currentAssignee)
	if err != nil {
		return fmt.Errorf("ManuallyAllocateCase: load case: %w", err)
	}

	// Skill check (unless override).
	if !skillOverride && len(requiredSkillsArray) > 0 {
		var skillMatch bool
		err = tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM jsonb_array_elements_text((
					SELECT COALESCE(metadata->>'skills', '[]')::jsonb
					FROM users WHERE user_id = $1::uuid
				)) AS s(skill)
				WHERE s.skill = ANY($2::text[])
			)
		`, targetUserID, requiredSkillsArray).Scan(&skillMatch)
		if err != nil {
			return fmt.Errorf("ManuallyAllocateCase: skill check: %w", err)
		}
		if !skillMatch {
			return model.ErrSkillMismatch
		}
	}

	_, err = tx.Exec(ctx, `
		UPDATE cases
		SET assigned_user_id = $2::uuid,
		    metadata         = metadata || jsonb_build_object(
		        'allocated_at',    now()::text,
		        'allocated_to',    $2::text,
		        'allocated_by',    $3::text,
		        'allocation_method', 'MANUAL'
		    ),
		    updated_at       = now(),
		    row_version      = row_version + 1
		WHERE id = $1::uuid
	`, caseID, targetUserID, allocatedByUserID)
	if err != nil {
		return fmt.Errorf("ManuallyAllocateCase: update: %w", err)
	}

	payload := map[string]interface{}{
		"case_id":           caseID,
		"allocated_to":      targetUserID,
		"allocated_by":      allocatedByUserID,
		"allocation_method": "MANUAL",
		"skill_override":    skillOverride,
		"previous_assignee": currentAssignee,
		"timestamp":         time.Now().UTC(),
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventCaseAllocated, payload); err != nil {
		return fmt.Errorf("ManuallyAllocateCase: publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ManuallyAllocateCase: commit: %w", err)
	}

	slog.Info("case manually allocated",
		"case_id", caseID,
		"to_user", targetUserID,
		"by_user", allocatedByUserID)
	return nil
}

// UnallocateCase removes the assigned user from a case and publishes
// CASE_UNALLOCATED so it re-enters the queue.
func UnallocateCase(ctx context.Context, pool *pgxpool.Pool, caseID, unallocatedByUserID, reason string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("UnallocateCase: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var previousAssignee string
	err = tx.QueryRow(ctx, `
		UPDATE cases
		SET assigned_user_id = NULL,
		    metadata         = metadata || jsonb_build_object(
		        'unallocated_at', now()::text,
		        'unallocated_by', $2::text,
		        'unallocated_reason', $3::text
		    ),
		    updated_at       = now(),
		    row_version      = row_version + 1
		WHERE id = $1::uuid
		RETURNING COALESCE(assigned_user_id::text, '')
	`, caseID, unallocatedByUserID, reason).Scan(&previousAssignee)
	if err != nil {
		return fmt.Errorf("UnallocateCase: update: %w", err)
	}

	payload := map[string]interface{}{
		"case_id":           caseID,
		"previous_assignee": previousAssignee,
		"unallocated_by":    unallocatedByUserID,
		"reason":            reason,
		"timestamp":         time.Now().UTC(),
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventCaseUnallocated, payload); err != nil {
		return fmt.Errorf("UnallocateCase: publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("UnallocateCase: commit: %w", err)
	}

	slog.Info("case unallocated",
		"case_id", caseID,
		"previous_assignee", previousAssignee,
		"reason", reason)
	return nil
}

// isNoRows returns true if err represents a "no rows found" condition.
func isNoRows(err error) bool {
	return err != nil && (err.Error() == "no rows in result set" ||
		strings.Contains(err.Error(), "no rows"))
}

// GetNextCasePreview returns scored previews without claiming any case.
// Useful for team leads to inspect the current queue state.
func GetNextCasePreview(ctx context.Context, pool *pgxpool.Pool, officerSkills []model.SkillCode, limit int) ([]model.CaseScorePreview, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	skillStrs := make([]string, len(officerSkills))
	for i, s := range officerSkills {
		skillStrs[i] = string(s)
	}

	rows, err := pool.Query(ctx, `
		SELECT
			c.id::text,
			c.reference_number,
			COALESCE(c.case_complexity, 'SIMPLE'),
			COALESCE(c.required_skills::text, ''),
			EXTRACT(EPOCH FROM (COALESCE(c.case_due_at, c.created_at + interval '5 days') - now())) / 3600.0,
			CASE
				WHEN EXTRACT(EPOCH FROM (COALESCE(c.case_due_at, c.created_at + interval '5 days') - now())) / 3600.0 <= 0 THEN 40.0
				ELSE GREATEST(0.0, 40.0 - (EXTRACT(EPOCH FROM (COALESCE(c.case_due_at, c.created_at + interval '5 days') - now())) / 3600.0 / 3.0))
			END AS sla_score
		FROM cases c
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE ct.code = $1
		  AND c.status = 'OPEN'
		  AND c.assigned_user_id IS NULL
		  AND c.current_stage_code = $2
		ORDER BY sla_score DESC
		LIMIT $3
	`, CaseTypeCode, StageAllocation, limit)
	if err != nil {
		return nil, fmt.Errorf("GetNextCasePreview: query: %w", err)
	}
	defer rows.Close()

	var results []model.CaseScorePreview
	for rows.Next() {
		var p model.CaseScorePreview
		var skillsCSV string
		if err := rows.Scan(
			&p.CaseID, &p.ReferenceNumber, &p.Complexity,
			&skillsCSV, &p.SLARemainingHrs, &p.SLAScore,
		); err != nil {
			return nil, fmt.Errorf("GetNextCasePreview: scan: %w", err)
		}
		for _, s := range strings.Split(skillsCSV, ",") {
			if s != "" {
				p.RequiredSkills = append(p.RequiredSkills, model.SkillCode(s))
			}
		}
		p.ComplexityScore = float64(model.ComplexityScore(p.Complexity))
		results = append(results, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetNextCasePreview: rows: %w", err)
	}

	return results, nil
}
