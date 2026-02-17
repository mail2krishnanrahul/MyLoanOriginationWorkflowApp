# Schema Evolution & Migration Strategy

## Tooling Choice: golang-migrate

**Recommendation: [golang-migrate](https://github.com/golang-migrate/migrate)**

| Criteria | golang-migrate | Atlas |
|---|---|---|
| Stack fit | Pure Go, works with `pgx` | Requires Atlas CLI + HCL |
| Migration style | Sequential numbered SQL (up/down) | Declarative (diff-based) |
| CI/CD simplicity | `migrate -source file://... up` | Needs schema snapshot |
| Team learning curve | Zero — write SQL | Medium — new DSL |
| Rollback | Explicit `.down.sql` files | Auto-generated (risky) |

**golang-migrate wins** because: your stack is Go + raw SQL + pgx, your team
already writes DDL by hand, and you need explicit rollback control for a
financial-grade system.

---

## Migration Folder Structure

```
db/
├── migrations/                          # golang-migrate numbered files
│   ├── 000001_recursive_schema.up.sql
│   ├── 000001_recursive_schema.down.sql
│   ├── 000002_case_types.up.sql
│   ├── 000002_case_types.down.sql
│   ├── 000003_cases.up.sql
│   ├── 000003_cases.down.sql
│   ├── 000004_stages.up.sql
│   ├── 000004_stages.down.sql
│   ├── 000005_activities.up.sql
│   ├── 000005_activities.down.sql
│   ├── 000006_tasks.up.sql
│   ├── 000006_tasks.down.sql
│   ├── 000007_events_outbox.up.sql
│   ├── 000007_events_outbox.down.sql
│   ├── 000008_task_heartbeat.up.sql
│   ├── 000008_task_heartbeat.down.sql
│   ├── 000009_tasks_add_sla_deadline.up.sql   ← sample new column
│   └── 000009_tasks_add_sla_deadline.down.sql
├── seeds/
│   └── home_loan_v1.json
└── migrate.go                           # Go wrapper for CLI-less usage
```

**Naming convention:** `{6-digit seq}_{snake_case_description}.{up|down}.sql`

> [!NOTE]
> Your existing `005-*.sql` through `012-*.sql` files should be renumbered to
> this format as a one-time migration. The content stays identical.

---

## Zero-Downtime Migration Pattern

### Rules for backward-compatible DDL changes:

1. **ADD COLUMN** — always `DEFAULT` + `NOT NULL` or nullable. Never require app changes first.
2. **Never DROP COLUMN** in the same release — deprecate first, drop in the next release.
3. **Never RENAME COLUMN** — add new, backfill, migrate reads, drop old.
4. **Never change column type** — add new column, backfill, swap.
5. **ADD INDEX CONCURRENTLY** — use `CREATE INDEX CONCURRENTLY` to avoid table locks.

### Two-phase deploy pattern:
```
Release N:   Migration adds new column (nullable/defaulted)
             App code ignores the new column (backward compatible)

Release N+1: App code starts reading/writing the new column
             Old column is still present (backward compatible)

Release N+2: Migration drops the old column (if renamed)
```

---

## CaseType Versioning Policy

### Version Semantics

| Change | Version Bump | Safe for running cases? |
|---|---|---|
| Fix typo in task name/description | Patch (v1 → v1) | ✅ Config update in place |
| Add optional task to existing activity | Minor (v1 → v2) | ✅ Old cases unaffected |
| Add new activity to existing stage | Minor (v1 → v2) | ✅ Old cases unaffected |
| Add new stage | Minor (v1 → v2) | ✅ Old cases unaffected |
| Remove a required task | Major (v1 → v2) | ⚠️ New version only |
| Rename task_definition_code | Major (v1 → v2) | ⚠️ New version only |
| Reorder stages | Major (v1 → v2) | ⚠️ New version only |
| Change completion_policy | Major (v1 → v2) | ⚠️ New version only |

### Core Rule

> **A case is pinned to the exact case_type version it was created with.**
> The `cases.case_type_id` FK points to a specific `case_types` row (which
> includes the version). The case's config never changes mid-flight.

### How to release a new version

1. INSERT a new `case_types` row with `version = 2`, `status = 'DRAFT'`
2. Update the config JSON with your changes
3. Test with synthetic cases
4. UPDATE `status = 'ACTIVE'` (the partial unique index ensures only one
   ACTIVE version per code)
5. UPDATE the old version's `status = 'DEPRECATED'`
6. Existing cases on v1 continue running — they reference the v1 row by ID

### How to rename a task_definition_code across versions

```
v1 config: { "code": "CREDIT_SCORE_PULL", ... }
v2 config: { "code": "CREDIT_BUREAU_CHECK", ... }
```

- v1 cases: tasks have `task_definition_code = 'CREDIT_SCORE_PULL'`
  → still works, still matches v1 config
- v2 cases: tasks have `task_definition_code = 'CREDIT_BUREAU_CHECK'`
  → uses v2 config
- Worker code: handle BOTH codes during the overlap period
- Once all v1 cases are COMPLETED/CANCELLED, remove old code from workers

### How to add a stage to v2 without affecting v1

```sql
-- v2 simply has an extra stage in its config JSON:
-- v1 config: stages = [APPLICATION, UNDERWRITING, SETTLEMENT]
-- v2 config: stages = [APPLICATION, UNDERWRITING, COMPLIANCE, SETTLEMENT]
--
-- v1 cases reference v1's case_types.id → they never see COMPLIANCE
-- v2 cases reference v2's case_types.id → they traverse COMPLIANCE
```

No DDL changes needed — stages are in the JSONB config, pinned per case.
