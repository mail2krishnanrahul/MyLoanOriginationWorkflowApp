package repository_test

import (
	"testing"
	// Uncomment once a test DB harness is available:
	// "context"
	// "workflow-engine/internal/repository"
	// "workflow-engine/pkg/model"
)

// TestNormalProgression verifies that moving forward from ordinal 1 → 2:
//   - updates cases.current_stage_code and current_stage_ordinal
//   - inserts a case_stage_transitions row with is_regression = false
//   - auto-promotes status from OPEN → IN_PROGRESS on first transition
func TestNormalProgression(t *testing.T) {
	t.Skip("requires Postgres test harness — implement with dockertest or testcontainers")

	// Setup:
	//   1. Create a case_type and case (status=OPEN, ordinal=1, stage="INITIAL_REVIEW")
	//
	// Act:
	//   repo.RecordStageTransition(ctx, tx, model.TransitionInput{
	//       CaseID:         caseID,
	//       ToStageCode:    "CREDIT_ASSESSMENT",
	//       ToStageOrdinal: 2,
	//       TriggeredBy:    "workflow-engine",
	//   })
	//
	// Assert:
	//   - case.current_stage_code  == "CREDIT_ASSESSMENT"
	//   - case.current_stage_ordinal == 2
	//   - case.status == "IN_PROGRESS"
	//   - case.row_version incremented
	//   - transition.is_regression == false
	//   - transition.from_stage_code == "INITIAL_REVIEW"
}

// TestRegression verifies that moving backward from ordinal 3 → 1:
//   - sets is_regression = true
//   - requires regression_reason (returns error without it)
//   - records the reason in the audit row when provided
func TestRegression(t *testing.T) {
	t.Skip("requires Postgres test harness — implement with dockertest or testcontainers")

	// Setup:
	//   1. Create a case at ordinal=3, stage="APPROVAL"
	//
	// Act (expect error — no reason):
	//   err := repo.RecordStageTransition(ctx, tx, model.TransitionInput{
	//       CaseID:         caseID,
	//       ToStageCode:    "INITIAL_REVIEW",
	//       ToStageOrdinal: 1,
	//       TriggeredBy:    "underwriter",
	//   })
	//   assert err != nil, "expected error for missing regression_reason"
	//
	// Act (success — with reason):
	//   reason := "Additional docs required by compliance"
	//   err = repo.RecordStageTransition(ctx, tx, model.TransitionInput{
	//       CaseID:           caseID,
	//       ToStageCode:      "INITIAL_REVIEW",
	//       ToStageOrdinal:   1,
	//       RegressionReason: &reason,
	//       TriggeredBy:      "underwriter",
	//   })
	//   assert err == nil
	//
	// Assert:
	//   - transition.is_regression == true
	//   - transition.regression_reason == "Additional docs required by compliance"
	//   - case.current_stage_ordinal == 1
}

// TestSameStageNoOp verifies that transitioning to the same stage+ordinal
// is a silent no-op: no case update, no audit row inserted.
func TestSameStageNoOp(t *testing.T) {
	t.Skip("requires Postgres test harness — implement with dockertest or testcontainers")

	// Setup:
	//   1. Create a case at ordinal=2, stage="CREDIT_ASSESSMENT"
	//
	// Act:
	//   err := repo.RecordStageTransition(ctx, tx, model.TransitionInput{
	//       CaseID:         caseID,
	//       ToStageCode:    "CREDIT_ASSESSMENT",
	//       ToStageOrdinal: 2,
	//       TriggeredBy:    "idempotent-retry",
	//   })
	//   assert err == nil
	//
	// Assert:
	//   - case.row_version unchanged
	//   - No new row in case_stage_transitions
}

// TestTerminalCaseBlocked verifies that COMPLETED or CANCELLED cases
// reject any further transitions.
func TestTerminalCaseBlocked(t *testing.T) {
	t.Skip("requires Postgres test harness — implement with dockertest or testcontainers")

	// Setup:
	//   1. Create a case with status=COMPLETED
	//
	// Act:
	//   err := repo.RecordStageTransition(ctx, tx, model.TransitionInput{
	//       CaseID:         caseID,
	//       ToStageCode:    "INITIAL_REVIEW",
	//       ToStageOrdinal: 1,
	//       TriggeredBy:    "should-fail",
	//   })
	//
	// Assert:
	//   - err != nil
	//   - err contains "terminal status"
}
