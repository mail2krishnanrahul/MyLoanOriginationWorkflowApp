# Architectural Governance Audit Report

This document outlines the violations of the architectural governance rules defined in `AG_RULES.md` found during the codebase audit.

## 1. Data Integrity (Rule 3.4 / Requirements)
**Rule Violated:** All temporal data must use `timestamptz`. (Implicit from canonical system design / DB rules).
**Files & Lines:**
- `db/migrations/000005_recursive_schema.up.sql`: Lines 8, 17, 32, 47, 60, 61, 73, 74, 75
**Description:** The table definitions use the `TIMESTAMP` type without a timezone instead of `TIMESTAMPTZ`. This violates the temporal data correctness rule for global systems.

## 2. Fail Fast & Meaningful Errors (Rule 2.2)
**Rule Violated:** Validate inputs and preconditions early. Return meaningful errors immediately upon detecting invalid state. Do not panic for business logic.
**Files & Lines:**
- `internal/multitenancy/service.go`: Line 182
**Description:** The code uses `panic("tenant scope missing from context")` instead of returning a formatted error that can be handled by the caller or middleware gracefully.

## 3. Error Handling - Ignored Errors (Rule 2.4)
**Rule Violated:** Always check and handle errors. Never use `_` to discard errors unless explicitly justified with a comment.
**Files & Lines:**
- `internal/document/storage.go`: Lines 124, 125 
  - `_ = tmpFile.Close()`
  - `_ = os.Remove(tmpPath)`
- `internal/versioning/service.go`: Lines 158, 287, 361, 520, 666
  - `_ = tx.Rollback()`
**Description:** Errors from standard library OS operations and database rollback operations are explicitly ignored using the blank identifier `_` without any corresponding explanatory comments justifying the omissions.

## 4. Context Propagation & Logging Validation (Rule 7.1 / 2.4)
**Status:** Audit showed high compliance. `slog` is used universally across the codebase. `fmt.Errorf("...: %w", err)` is correctly utilized to wrap errors. Contexts are successfully propagated to SQL transactions (e.g., `tx.QueryRow(ctx, ...)`). No violations found in these specific areas.
