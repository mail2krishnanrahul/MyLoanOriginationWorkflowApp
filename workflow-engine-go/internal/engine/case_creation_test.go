package engine_test

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Integration test outline for CreateCase
// These tests require a PostgreSQL test harness (e.g. testcontainers-go).
// ---------------------------------------------------------------------------

func TestCreateCase_HappyPath(t *testing.T) {
	t.Skip("requires PostgreSQL test harness")

	// Setup:
	//   1. Start a PostgreSQL container
	//   2. Run all migrations (006 through 012)
	//   3. Seed an ACTIVE case_type (e.g. HOME_LOAN v1) with at least one stage
	//
	// Act:
	//   req := engine.CreateCaseRequest{
	//       CaseTypeCode: "HOME_LOAN",
	//       Metadata:     json.RawMessage(`{"borrower_id":"B001","product_id":"FIXED_30"}`),
	//       RequestedBy:  "test-user",
	//   }
	//   resp, err := engine.CreateCase(ctx, repo, req)
	//
	// Assert:
	//   - err is nil
	//   - resp.CaseID is a valid UUID
	//   - resp.ReferenceNumber matches expected format (e.g. "CASE-000001")
	//   - resp.InitialStage equals the first stage in the config
	//   - resp.TasksCreated > 0
	//   - The cases table has exactly 1 row with status OPEN
	//   - The case_stage_transitions table has 1 entry-stage transition
	//   - The events_outbox has 1 CASE_CREATED event
	//   - The tasks table has the correct number of PENDING tasks
}

func TestCreateCase_InvalidCaseType(t *testing.T) {
	t.Skip("requires PostgreSQL test harness")

	// Setup:
	//   1. Start a PostgreSQL container with migrations applied
	//   2. Do NOT seed any case_types (or seed one as DEPRECATED)
	//
	// Act:
	//   req := engine.CreateCaseRequest{
	//       CaseTypeCode: "NONEXISTENT",
	//       RequestedBy:  "test-user",
	//   }
	//   resp, err := engine.CreateCase(ctx, repo, req)
	//
	// Assert:
	//   - err is not nil
	//   - err contains "not found or not ACTIVE"
	//   - resp is zero-value (empty)
	//   - The cases table has 0 rows
}

func TestCreateCase_WithSubCases(t *testing.T) {
	t.Skip("requires PostgreSQL test harness")

	// Setup:
	//   1. Seed a parent case_type HOME_LOAN with
	//      config.sub_case_types = ["CREDIT_CHECK", "VALUATION"]
	//   2. Seed CREDIT_CHECK and VALUATION as ACTIVE case_types
	//
	// Act:
	//   req := engine.CreateCaseRequest{
	//       CaseTypeCode: "HOME_LOAN",
	//       RequestedBy:  "test-user",
	//   }
	//   resp, err := engine.CreateCase(ctx, repo, req)
	//
	// Assert:
	//   - err is nil
	//   - The cases table has 3 rows (1 parent + 2 sub-cases)
	//   - Sub-cases have parent_case_id = resp.CaseID
	//   - Sub-cases inherit the parent metadata
}

func TestCreateCase_DuplicateIdempotencyKey(t *testing.T) {
	t.Skip("requires PostgreSQL test harness")

	// Setup:
	//   1. Seed an ACTIVE case_type
	//   2. Call CreateCase once to create the initial case
	//
	// Act:
	//   Call CreateCase a second time with an idempotent operation.
	//   Since CreateCase itself doesn't deduplicate case rows (only tasks via
	//   idempotency_key), this test verifies that re-creating tasks for the
	//   same stage is idempotent via the task's idempotency_key mechanism.
	//
	// Assert:
	//   - Both calls succeed
	//   - The tasks table has no duplicate tasks (same idempotency_key)
	//   - Task count matches expected (not doubled)
}

func TestCreateCaseHTTP_201Created(t *testing.T) {
	t.Skip("requires PostgreSQL test harness")

	// Setup:
	//   1. Seed an ACTIVE case_type
	//   2. Create an http.ServeMux and register handlers via RegisterCaseHandlers
	//   3. Use httptest.NewServer
	//
	// Act:
	//   POST /cases with valid JSON body
	//
	// Assert:
	//   - Status 201 Created
	//   - Response body has case_id, reference_number, initial_stage
}

func TestCreateCaseHTTP_400InvalidBody(t *testing.T) {
	t.Skip("requires PostgreSQL test harness")

	// Act:
	//   POST /cases with invalid JSON body
	//
	// Assert:
	//   - Status 400 Bad Request
	//   - Response contains "invalid request body"
}

func TestCreateCaseHTTP_422InvalidCaseType(t *testing.T) {
	t.Skip("requires PostgreSQL test harness")

	// Act:
	//   POST /cases with valid JSON but nonexistent case_type_code
	//
	// Assert:
	//   - Status 422 Unprocessable Entity
	//   - Response contains "invalid case_type"
}
