package docverification

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ClassifyCaseInput carries inputs for the case classification step.
type ClassifyCaseInput struct {
	CaseID       string
	Complexity   model.CaseComplexity
	SkillCodes   []model.SkillCode
	ClassifiedBy string // user_id of the classifying officer
	Notes        string
}

// ClassifyCase sets the complexity tier and required skills on a case,
// then publishes CASE_CLASSIFIED. If the case is NON_STANDARD it also
// publishes NON_STANDARD_CASE_FLAGGED for supervisor routing.
func ClassifyCase(ctx context.Context, pool *pgxpool.Pool, input ClassifyCaseInput) error {
	if !model.ValidCaseComplexity(input.Complexity) {
		return fmt.Errorf("ClassifyCase: invalid complexity %q", input.Complexity)
	}
	if len(input.SkillCodes) == 0 {
		return fmt.Errorf("ClassifyCase: at least one skill code is required")
	}
	for _, s := range input.SkillCodes {
		if !model.ValidSkillCode(s) {
			return fmt.Errorf("ClassifyCase: invalid skill code %q", s)
		}
	}

	skillStrs := make([]string, len(input.SkillCodes))
	for i, s := range input.SkillCodes {
		skillStrs[i] = string(s)
	}
	skillsJSON, _ := json.Marshal(skillStrs)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ClassifyCase: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Persist classification on the case.
	_, err = tx.Exec(ctx, `
		UPDATE cases
		SET case_complexity = $2,
		    required_skills = $3::text[],
		    metadata        = metadata || jsonb_build_object(
		        'classification_notes', $4::text,
		        'classified_by',        $5::text,
		        'classified_at',        now()::text
		    ),
		    updated_at      = now(),
		    row_version     = row_version + 1
		WHERE id = $1::uuid
		  AND current_stage_code = $6
	`, input.CaseID, string(input.Complexity), strings.Join(skillStrs, ","),
		input.Notes, input.ClassifiedBy, StageClassification)
	if err != nil {
		return fmt.Errorf("ClassifyCase: update case: %w", err)
	}

	// 2. Publish CASE_CLASSIFIED.
	payload := map[string]interface{}{
		"case_id":         input.CaseID,
		"complexity":      input.Complexity,
		"required_skills": skillStrs,
		"classified_by":   input.ClassifiedBy,
		"timestamp":       time.Now().UTC(),
	}
	if err := publishEventInTx(ctx, tx, input.CaseID, model.EventCaseClassified, payload); err != nil {
		return fmt.Errorf("ClassifyCase: publish CASE_CLASSIFIED: %w", err)
	}

	// 3. If NON_STANDARD, also flag for supervisor review.
	if input.Complexity == model.CaseComplexityNonStandard {
		nsPayload := map[string]interface{}{
			"case_id":    input.CaseID,
			"complexity": input.Complexity,
			"flagged_by": input.ClassifiedBy,
			"skills":     skillStrs,
		}
		if err := publishEventInTx(ctx, tx, input.CaseID, model.EventNonStandardCaseFlagged, nsPayload); err != nil {
			return fmt.Errorf("ClassifyCase: publish NON_STANDARD_CASE_FLAGGED: %w", err)
		}
		// Also update skill scores in the tasks index for allocation
		_, _ = tx.Exec(ctx, `
			UPDATE tasks
			SET metadata = metadata || $2::jsonb
			WHERE case_id = $1::uuid
			  AND stage_code = $3
		`, input.CaseID, skillsJSON, StageAllocation)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ClassifyCase: commit: %w", err)
	}

	slog.Info("case classified",
		"case_id", input.CaseID,
		"complexity", input.Complexity,
		"skills", skillStrs,
		"non_standard", input.Complexity == model.CaseComplexityNonStandard)
	return nil
}
