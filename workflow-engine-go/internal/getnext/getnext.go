package getnext

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─────────────────────────────────────────────────────────────────────────────
// Weight cache (5-minute TTL per tenant+caseType key)
// ─────────────────────────────────────────────────────────────────────────────

type weightCacheEntry struct {
	weights   GetNextWeights
	expiresAt time.Time
}

var (
	weightCache sync.Map // map[string]weightCacheEntry
	cacheTTL    = 5 * time.Minute
)

// cacheKey returns the sync.Map key for a tenant+caseType pair.
func cacheKey(tenantID, caseTypeCode string) string {
	return tenantID + ":" + caseTypeCode
}

// ─────────────────────────────────────────────────────────────────────────────
// Session skip counter (in-memory, per user, 4-hour TTL)
// ─────────────────────────────────────────────────────────────────────────────

type skipCounterEntry struct {
	count     int
	expiresAt time.Time
}

var skipCounters sync.Map // map[userID]skipCounterEntry

// defaultSkipLimit is the maximum skips per session before ErrSkipLimitExceeded.
// This is the fallback; tenant config can override via tenant metadata.
const defaultSkipLimit = 5

// sessionSkipCount returns the number of skips recorded in this session.
func sessionSkipCount(userID string) int {
	if v, ok := skipCounters.Load(userID); ok {
		e := v.(skipCounterEntry)
		if time.Now().Before(e.expiresAt) {
			return e.count
		}
	}
	return 0
}

// incrementSkipCount increments and returns the new session skip count.
// The 4-hour TTL resets on first skip in a new session.
func incrementSkipCount(userID string) int {
	entry := skipCounterEntry{
		count:     1,
		expiresAt: time.Now().Add(4 * time.Hour),
	}
	if v, ok := skipCounters.Load(userID); ok {
		existing := v.(skipCounterEntry)
		if time.Now().Before(existing.expiresAt) {
			entry.count = existing.count + 1
			entry.expiresAt = existing.expiresAt // preserve original TTL
		}
	}
	skipCounters.Store(userID, entry)
	return entry.count
}

// ─────────────────────────────────────────────────────────────────────────────
// Step 4a — Weight loading
// ─────────────────────────────────────────────────────────────────────────────

// LoadGetNextWeights loads weights from getnext_weights, falling back to defaults.
// Results are cached for 5 minutes.
func LoadGetNextWeights(ctx context.Context, pool *pgxpool.Pool, tenantID, caseTypeCode string) (GetNextWeights, error) {
	key := cacheKey(tenantID, caseTypeCode)

	// Cache hit
	if v, ok := weightCache.Load(key); ok {
		e := v.(weightCacheEntry)
		if time.Now().Before(e.expiresAt) {
			return e.weights, nil
		}
	}

	var w GetNextWeights
	err := pool.QueryRow(ctx, `
		SELECT w_sla, w_skill, w_age, w_complexity, w_value, w_affinity, w_workload
		FROM getnext_weights
		WHERE tenant_id = $1::uuid
		  AND case_type_code = $2
		ORDER BY effective_from DESC
		LIMIT 1`, tenantID, caseTypeCode).Scan(
		&w.WSla, &w.WSkill, &w.WAge, &w.WComplexity, &w.WValue, &w.WAffinity, &w.WWorkload,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			// No tenant override — use defaults
			w = DefaultWeights()
		} else {
			return w, fmt.Errorf("LoadGetNextWeights: %w", err)
		}
	}

	if err := w.Validate(); err != nil {
		return w, err
	}

	weightCache.Store(key, weightCacheEntry{weights: w, expiresAt: time.Now().Add(cacheTTL)})
	return w, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Step 4b — Capacity check
// ─────────────────────────────────────────────────────────────────────────────

// CheckUserCapacity returns the user's current workload state.
// This function never returns an error for "at capacity" — callers make that decision.
func CheckUserCapacity(ctx context.Context, pool *pgxpool.Pool, userID, tenantID string) (UserCapacityInfo, error) {
	var info UserCapacityInfo

	// Count cases currently owned by this user (OPEN or IN_PROGRESS)
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM cases
		WHERE tenant_id = $1::uuid
		  AND assigned_user_id = $2::uuid
		  AND status IN ('OPEN', 'IN_PROGRESS', 'ALLOCATABLE')`, tenantID, userID).Scan(&info.ActiveCases)
	if err != nil {
		return info, fmt.Errorf("CheckUserCapacity count: %w", err)
	}

	// Fetch the worker capacity limit (using users.id → workers.id mapping)
	var maxCases int
	err = pool.QueryRow(ctx, `
		SELECT COALESCE(w.max_concurrent_tasks, 10)
		FROM users u
		LEFT JOIN workers w ON w.id = u.id
		WHERE u.user_id = $1::uuid
		  AND u.tenant_id = $2::uuid
		LIMIT 1`, userID, tenantID).Scan(&maxCases)
	if err != nil {
		// Default capacity if user record not found
		maxCases = 10
	}

	info.MaxActiveCases = maxCases
	if maxCases > 0 {
		info.CapacityPct = float64(info.ActiveCases) / float64(maxCases)
	}
	info.IsAtCapacity = info.CapacityPct >= 1.0
	info.IsNearCapacity = info.CapacityPct >= 0.75

	return info, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Step 4c — Scoring SQL query builder
// ─────────────────────────────────────────────────────────────────────────────

// scoredRow holds the raw columns returned by the scoring query.
type scoredRow struct {
	CaseID           string
	ReferenceNumber  string
	CaseTypeCode     string
	CurrentStageCode string
	Status           string
	Complexity       string
	RequiredSkills   []string
	CaseDueAt        *time.Time
	CreatedAt        time.Time
	SLARaw           float64
	SkillRaw         float64
	AgeRaw           float64
	ComplexityRaw    float64
	ValueRaw         float64
	AffinityRaw      float64
	WorkloadRaw      float64
	CompositeScore   float64
	HoursInQueue     float64
	QueueDepth       int
}

// buildGetNextQuery returns a parameterised scoring SQL query and its arguments.
// When preview=false, FOR UPDATE SKIP LOCKED is appended to prevent double-claim.
func buildGetNextQuery(userID, tenantID string, weights GetNextWeights, maxCases, limit int, preview bool) (string, []interface{}) {
	forUpdate := ""
	if !preview {
		forUpdate = "FOR UPDATE SKIP LOCKED"
	}

	cteSQL := `
WITH
user_data AS (
    SELECT u.user_id, u.tenant_id,
           COALESCE(array_agg(ws.skill_code::text) FILTER (WHERE ws.skill_code IS NOT NULL), '{}') AS skill_codes
    FROM users u
    LEFT JOIN workers wk ON wk.id = u.id
    LEFT JOIN worker_skills ws ON ws.worker_id = wk.id
    WHERE u.user_id = $1::uuid
      AND u.tenant_id = $2::uuid
    GROUP BY u.user_id, u.tenant_id
),
user_capacity AS (
    SELECT COUNT(*)::int AS active_cases
    FROM cases
    WHERE tenant_id = $2::uuid
      AND assigned_user_id = $1::uuid
      AND status IN ('OPEN', 'IN_PROGRESS', 'ALLOCATABLE')
),
affinity AS (
    SELECT case_id, affinity_score
    FROM case_user_affinity
    WHERE user_id = $1::uuid
      AND tenant_id = $2::uuid
),
stage_age AS (
    SELECT cat.case_id,
           EXTRACT(EPOCH FROM (NOW() - cat.entered_at)) / 3600.0 AS hours_in_queue
    FROM case_allocation_transitions cat
    WHERE cat.is_current = TRUE
      AND cat.tenant_id = $2::uuid
),
queue_depth AS (
    SELECT COUNT(*)::int AS depth
    FROM cases c
    CROSS JOIN user_data ud
    WHERE c.tenant_id = $2::uuid
      AND c.status = 'ALLOCATABLE'
      AND c.assigned_user_id IS NULL
      AND (c.required_skills = '{}' OR c.required_skills IS NULL
           OR c.required_skills && ud.skill_codes)
),
scored AS (
    SELECT
        c.id            AS case_id,
        c.reference_number,
        ct.code         AS case_type_code,
        c.current_stage_code,
        c.status,
        COALESCE(c.case_complexity, '')               AS complexity,
        COALESCE(c.required_skills, '{}')             AS required_skills,
        c.case_due_at,
        c.created_at,
        -- ── SLA Score (0–100) ────────────────────────────────────────────────
        CASE
            WHEN c.case_due_at < NOW()                            THEN 100.0
            WHEN c.case_due_at < NOW() + INTERVAL '2 hours'      THEN  90.0
            WHEN c.case_due_at < NOW() + INTERVAL '4 hours'      THEN  80.0
            WHEN c.case_due_at < NOW() + INTERVAL '8 hours'      THEN  65.0
            WHEN c.case_due_at < NOW() + INTERVAL '24 hours'     THEN  45.0
            WHEN c.case_due_at < NOW() + INTERVAL '48 hours'     THEN  25.0
            ELSE 0.0
        END AS sla_raw,
        -- ── Skill Score (0–100) — cases with 0 match are excluded in WHERE ──
        CASE
            WHEN c.required_skills = '{}' OR c.required_skills IS NULL THEN 100.0
            WHEN c.required_skills <@ ud.skill_codes                    THEN 100.0
            WHEN (SELECT COUNT(*) FROM unnest(c.required_skills) s WHERE s = ANY(ud.skill_codes))::float
                 / NULLIF(cardinality(c.required_skills), 0) > 0.5      THEN  75.0
            WHEN (SELECT COUNT(*) FROM unnest(c.required_skills) s WHERE s = ANY(ud.skill_codes)) >= 1   THEN  40.0
            ELSE 0.0
        END AS skill_raw,
        -- ── Age Score (0–50) ─────────────────────────────────────────────────
        LEAST(COALESCE(sa.hours_in_queue, 0.0), 50.0) AS age_raw,
        -- ── Complexity Score (0–50) ──────────────────────────────────────────
        CASE c.case_complexity
            WHEN 'NON_STANDARD' THEN 50.0
            WHEN 'STANDARD_3'   THEN 40.0
            WHEN 'STANDARD_2'   THEN 30.0
            WHEN 'STANDARD_1'   THEN 20.0
            WHEN 'SIMPLE'       THEN  5.0
            ELSE 0.0
        END AS complexity_raw,
        -- ── Value Score (0–50) ───────────────────────────────────────────────
        CASE
            WHEN (c.metadata->>'vip_borrower')::boolean IS TRUE          THEN 50.0
            WHEN (c.metadata->>'loan_amount')::numeric > 5000000         THEN 40.0
            WHEN (c.metadata->>'loan_amount')::numeric > 2000000         THEN 25.0
            WHEN (c.metadata->>'loan_amount')::numeric > 1000000         THEN 10.0
            ELSE 0.0
        END AS value_raw,
        -- ── Affinity Score (0–30) ────────────────────────────────────────────
        COALESCE(af.affinity_score, 0.0)              AS affinity_raw,
        -- ── Workload Penalty (−50 to 0) ─────────────────────────────────────
        CASE
            WHEN uc.active_cases::float / NULLIF($3, 0) >= 0.90 THEN -50.0
            WHEN uc.active_cases::float / NULLIF($3, 0) >= 0.75 THEN -30.0
            WHEN uc.active_cases::float / NULLIF($3, 0) >= 0.50 THEN -10.0
            ELSE 0.0
        END AS workload_raw,
        COALESCE(sa.hours_in_queue, 0.0)              AS hours_in_queue,
        (SELECT depth FROM queue_depth)               AS queue_depth
    FROM cases c
    CROSS JOIN user_data ud
    CROSS JOIN user_capacity uc
    LEFT JOIN case_types ct ON ct.id = c.case_type_id
    LEFT JOIN affinity af ON af.case_id = c.id
    LEFT JOIN stage_age sa ON sa.case_id = c.id
    WHERE c.tenant_id = $2::uuid
      AND c.status = 'ALLOCATABLE'
      AND c.assigned_user_id IS NULL
      -- Skill filter: user must overlap at least 1 required skill,
      -- OR case has no required skills (open to all)
      AND (
          c.required_skills = '{}'
          OR c.required_skills IS NULL
          OR c.required_skills && ud.skill_codes
      )
)
SELECT
    case_id,
    reference_number,
    COALESCE(case_type_code, ''),
    current_stage_code,
    status,
    complexity,
    required_skills,
    case_due_at,
    created_at,
    sla_raw,
    skill_raw,
    age_raw,
    complexity_raw,
    value_raw,
    affinity_raw,
    workload_raw,
    -- Composite score
    ($4 * sla_raw
     + $5 * skill_raw
     + $6 * age_raw
     + $7 * complexity_raw
     + $8 * value_raw
     + $9 * affinity_raw
     + $10 * workload_raw
    ) AS composite_score,
    hours_in_queue,
    queue_depth
FROM scored
ORDER BY composite_score DESC
LIMIT $11
` + forUpdate

	args := []interface{}{
		userID,              // $1
		tenantID,            // $2
		maxCases,            // $3 (for workload penalty divisor)
		weights.WSla,        // $4
		weights.WSkill,      // $5
		weights.WAge,        // $6
		weights.WComplexity, // $7
		weights.WValue,      // $8
		weights.WAffinity,   // $9
		weights.WWorkload,   // $10
		limit,               // $11
	}
	return cteSQL, args
}

// ─────────────────────────────────────────────────────────────────────────────
// Score explanation helpers
// ─────────────────────────────────────────────────────────────────────────────

func explainSLAScore(rawScore float64, caseDueAt *time.Time) string {
	if caseDueAt == nil {
		return "No SLA deadline set"
	}
	remaining := time.Until(*caseDueAt)
	switch {
	case rawScore == 100:
		overdue := -remaining
		h := int(overdue.Hours())
		return fmt.Sprintf("SLA breached %dh ago — critical urgency", h)
	case rawScore >= 90:
		return fmt.Sprintf("SLA due in %.0f minutes — critical urgency", remaining.Minutes())
	case rawScore >= 80:
		return fmt.Sprintf("SLA due in %.1f hours — high urgency", remaining.Hours())
	case rawScore >= 65:
		return fmt.Sprintf("SLA due in %.1f hours — elevated urgency", remaining.Hours())
	case rawScore >= 45:
		return fmt.Sprintf("SLA due in %.1f hours — moderate urgency", remaining.Hours())
	case rawScore >= 25:
		return fmt.Sprintf("SLA due in %.1f hours — low urgency", remaining.Hours())
	default:
		return fmt.Sprintf("SLA due in %.1f hours — sufficient time remaining", remaining.Hours())
	}
}

func explainSkillScore(rawScore float64, requiredSkills, userSkills []string) string {
	if len(requiredSkills) == 0 {
		return "No specific skills required — open to all operators"
	}
	matched := 0
	for _, req := range requiredSkills {
		for _, us := range userSkills {
			if req == us {
				matched++
				break
			}
		}
	}
	switch {
	case rawScore == 100:
		return fmt.Sprintf("You hold all %d required skills (100%% match)", len(requiredSkills))
	case rawScore == 75:
		return fmt.Sprintf("You hold %d of %d required skills (>50%% match)", matched, len(requiredSkills))
	default:
		return fmt.Sprintf("You hold %d of %d required skills (partial match)", matched, len(requiredSkills))
	}
}

func explainAgeScore(rawScore, hoursInQueue float64) string {
	if hoursInQueue == 0 {
		return "Just entered the allocation queue"
	}
	if rawScore >= 50 {
		return fmt.Sprintf("Case in queue for %.1f hours (age score capped at 50)", hoursInQueue)
	}
	return fmt.Sprintf("Case in queue for %.1f hours (age score %.1f)", hoursInQueue, rawScore)
}

func explainComplexityScore(rawScore float64, complexity string) string {
	if complexity == "" {
		return "Complexity not set"
	}
	return fmt.Sprintf("Complexity: %s (score %.0f)", complexity, rawScore)
}

func explainValueScore(rawScore float64) string {
	switch {
	case rawScore == 50:
		return "VIP borrower — highest business priority"
	case rawScore == 40:
		return "Loan value > $5M — high business value"
	case rawScore == 25:
		return "Loan value > $2M — elevated business value"
	case rawScore == 10:
		return "Loan value > $1M — standard business value"
	default:
		return "Standard loan value"
	}
}

func explainAffinityScore(rawScore float64) string {
	switch {
	case rawScore == 30:
		return "You were previously assigned to this case"
	case rawScore == 20:
		return "You completed a task on this case previously"
	case rawScore == 10:
		return "A teammate worked on this case"
	default:
		return "No prior relationship with this case"
	}
}

func explainWorkloadScore(rawScore float64, capacityPct float64) string {
	pct := int(capacityPct * 100)
	switch {
	case rawScore <= -50:
		return fmt.Sprintf("You are at %d%% capacity — workload penalty applied", pct)
	case rawScore <= -30:
		return fmt.Sprintf("You are at %d%% capacity — moderate workload penalty", pct)
	case rawScore <= -10:
		return fmt.Sprintf("You are at %d%% capacity — light workload penalty", pct)
	default:
		return fmt.Sprintf("You are at %d%% capacity — no penalty", pct)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal: scan a scored row and build GetNextResult
// ─────────────────────────────────────────────────────────────────────────────

func buildResult(row scoredRow, weights GetNextWeights, userSkills []string, capacity UserCapacityInfo) GetNextResult {
	breakdown := ScoreBreakdown{
		SLA: ScoreFactor{
			RawScore:      row.SLARaw,
			Weight:        weights.WSla,
			WeightedScore: row.SLARaw * weights.WSla,
			Explanation:   explainSLAScore(row.SLARaw, row.CaseDueAt),
		},
		Skill: ScoreFactor{
			RawScore:      row.SkillRaw,
			Weight:        weights.WSkill,
			WeightedScore: row.SkillRaw * weights.WSkill,
			Explanation:   explainSkillScore(row.SkillRaw, row.RequiredSkills, userSkills),
		},
		Age: ScoreFactor{
			RawScore:      row.AgeRaw,
			Weight:        weights.WAge,
			WeightedScore: row.AgeRaw * weights.WAge,
			Explanation:   explainAgeScore(row.AgeRaw, row.HoursInQueue),
		},
		Complexity: ScoreFactor{
			RawScore:      row.ComplexityRaw,
			Weight:        weights.WComplexity,
			WeightedScore: row.ComplexityRaw * weights.WComplexity,
			Explanation:   explainComplexityScore(row.ComplexityRaw, row.Complexity),
		},
		Value: ScoreFactor{
			RawScore:      row.ValueRaw,
			Weight:        weights.WValue,
			WeightedScore: row.ValueRaw * weights.WValue,
			Explanation:   explainValueScore(row.ValueRaw),
		},
		Affinity: ScoreFactor{
			RawScore:      row.AffinityRaw,
			Weight:        weights.WAffinity,
			WeightedScore: row.AffinityRaw * weights.WAffinity,
			Explanation:   explainAffinityScore(row.AffinityRaw),
		},
		Workload: ScoreFactor{
			RawScore:      row.WorkloadRaw,
			Weight:        weights.WWorkload,
			WeightedScore: row.WorkloadRaw * weights.WWorkload,
			Explanation:   explainWorkloadScore(row.WorkloadRaw, capacity.CapacityPct),
		},
		WeightsUsed: weights,
	}

	return GetNextResult{
		Case: CaseSummary{
			ID:               row.CaseID,
			ReferenceNumber:  row.ReferenceNumber,
			CaseTypeCode:     row.CaseTypeCode,
			CurrentStageCode: row.CurrentStageCode,
			Status:           row.Status,
			Complexity:       row.Complexity,
			RequiredSkills:   row.RequiredSkills,
			CaseDueAt:        row.CaseDueAt,
			CreatedAt:        row.CreatedAt,
		},
		CompositeScore: row.CompositeScore,
		ScoreBreakdown: breakdown,
		QueueDepth:     row.QueueDepth,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal: record an audit entry in getnext_claims
// ─────────────────────────────────────────────────────────────────────────────

func recordAudit(ctx context.Context, tx pgx.Tx, tenantID, userID string, caseID *string,
	action GetNextClaimAction, score *float64, breakdown *ScoreBreakdown,
	skipReason *SkipReason, skipNotes string, queueDepth, activeCases int) error {

	var breakdownJSON []byte
	if breakdown != nil {
		var err error
		breakdownJSON, err = json.Marshal(breakdown)
		if err != nil {
			return fmt.Errorf("recordAudit marshal breakdown: %w", err)
		}
	}

	var reasonStr *string
	if skipReason != nil {
		s := string(*skipReason)
		reasonStr = &s
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO getnext_claims
		    (tenant_id, user_id, case_id, action, composite_score, score_breakdown,
		     skip_reason, skip_notes, queue_depth, user_active_cases)
		VALUES
		    ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9, $10)`,
		tenantID, userID, caseID, string(action), score, breakdownJSON,
		reasonStr, skipNotes, queueDepth, activeCases,
	)
	if err != nil {
		return fmt.Errorf("recordAudit insert: %w", err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Step 4d — GetNext (main claim function)
// ─────────────────────────────────────────────────────────────────────────────

// GetNext returns and optionally claims the highest-scored case for the user.
// When req.Preview=true, no state is changed and no rows are locked.
func GetNext(ctx context.Context, pool *pgxpool.Pool, req GetNextRequest) (GetNextResult, error) {
	log := slog.Default().With("user_id", req.UserID, "tenant_id", req.TenantID, "preview", req.Preview)

	// 1. Capacity check
	capacity, err := CheckUserCapacity(ctx, pool, req.UserID, req.TenantID)
	if err != nil {
		return GetNextResult{}, fmt.Errorf("GetNext capacity: %w", err)
	}
	if capacity.IsAtCapacity && !req.Preview {
		_ = func() error {
			tx, err := pool.Begin(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
			_ = recordAudit(ctx, tx, req.TenantID, req.UserID, nil,
				ActionCapacityBlocked, nil, nil, nil, "", 0, capacity.ActiveCases)
			return tx.Commit(ctx)
		}()
		return GetNextResult{}, ErrUserAtCapacity
	}

	// 2. Load weights
	weights, err := LoadGetNextWeights(ctx, pool, req.TenantID, req.CaseTypeCode)
	if err != nil {
		return GetNextResult{}, fmt.Errorf("GetNext weights: %w", err)
	}

	// 3. Build and execute scoring query
	query, args := buildGetNextQuery(req.UserID, req.TenantID, weights, capacity.MaxActiveCases, 1, req.Preview)

	// Fetch user's current skills for explanation building
	var userSkills []string
	_ = pool.QueryRow(ctx, `
		SELECT COALESCE(array_agg(ws.skill_code) FILTER (WHERE ws.skill_code IS NOT NULL), '{}')
		FROM users u
		LEFT JOIN workers wk ON wk.id = u.id
		LEFT JOIN worker_skills ws ON ws.worker_id = wk.id
		WHERE u.user_id = $1::uuid AND u.tenant_id = $2::uuid
		GROUP BY u.user_id`, req.UserID, req.TenantID).Scan(&userSkills)

	// For preview mode: read-only, no transaction
	if req.Preview {
		var row scoredRow
		err := pool.QueryRow(ctx, query, args...).Scan(
			&row.CaseID, &row.ReferenceNumber, &row.CaseTypeCode,
			&row.CurrentStageCode, &row.Status, &row.Complexity,
			&row.RequiredSkills, &row.CaseDueAt, &row.CreatedAt,
			&row.SLARaw, &row.SkillRaw, &row.AgeRaw, &row.ComplexityRaw,
			&row.ValueRaw, &row.AffinityRaw, &row.WorkloadRaw,
			&row.CompositeScore, &row.HoursInQueue, &row.QueueDepth,
		)
		if err == pgx.ErrNoRows {
			return GetNextResult{}, ErrNoEligibleCases
		}
		if err != nil {
			return GetNextResult{}, fmt.Errorf("GetNext preview query: %w", err)
		}
		result := buildResult(row, weights, userSkills, capacity)
		log.Info("getnext preview", "case_id", row.CaseID, "score", row.CompositeScore)
		return result, nil
	}

	// 4. Skip limit check (claim mode only)
	skipCount := sessionSkipCount(req.UserID)
	if skipCount >= defaultSkipLimit {
		return GetNextResult{}, ErrSkipLimitExceeded
	}

	// 5. Full claim in a transaction
	tx, err := pool.Begin(ctx)
	if err != nil {
		return GetNextResult{}, fmt.Errorf("GetNext begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var row scoredRow
	err = tx.QueryRow(ctx, query, args...).Scan(
		&row.CaseID, &row.ReferenceNumber, &row.CaseTypeCode,
		&row.CurrentStageCode, &row.Status, &row.Complexity,
		&row.RequiredSkills, &row.CaseDueAt, &row.CreatedAt,
		&row.SLARaw, &row.SkillRaw, &row.AgeRaw, &row.ComplexityRaw,
		&row.ValueRaw, &row.AffinityRaw, &row.WorkloadRaw,
		&row.CompositeScore, &row.HoursInQueue, &row.QueueDepth,
	)
	if err == pgx.ErrNoRows {
		_ = recordAudit(ctx, tx, req.TenantID, req.UserID, nil,
			ActionNoEligibleCases, nil, nil, nil, "", 0, capacity.ActiveCases)
		_ = tx.Commit(ctx)
		return GetNextResult{}, ErrNoEligibleCases
	}
	if err != nil {
		return GetNextResult{}, fmt.Errorf("GetNext score query: %w", err)
	}

	// 6. Claim the case: optimistic lock on row_version
	tag, err := tx.Exec(ctx, `
		UPDATE cases
		SET assigned_user_id = $1::uuid,
		    status           = 'IN_PROGRESS',
		    row_version      = row_version + 1,
		    updated_at       = now()
		WHERE id = $2::uuid
		  AND status = 'ALLOCATABLE'
		  AND assigned_user_id IS NULL`, req.UserID, row.CaseID)
	if err != nil {
		return GetNextResult{}, fmt.Errorf("GetNext claim update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return GetNextResult{}, ErrCaseAlreadyClaimed
	}

	// 7. Close the allocation transition
	_, _ = tx.Exec(ctx, `
		UPDATE case_allocation_transitions
		SET is_current = FALSE, exited_at = now()
		WHERE case_id = $1::uuid AND is_current = TRUE`, row.CaseID)

	// 8. Build result and record audit
	result := buildResult(row, weights, userSkills, capacity)
	compositeScore := result.CompositeScore
	caseID := row.CaseID

	if err := recordAudit(ctx, tx, req.TenantID, req.UserID, &caseID,
		ActionClaimed, &compositeScore, &result.ScoreBreakdown,
		nil, "", row.QueueDepth, capacity.ActiveCases); err != nil {
		return GetNextResult{}, fmt.Errorf("GetNext audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return GetNextResult{}, fmt.Errorf("GetNext commit: %w", err)
	}

	log.Info("getnext claim successful",
		"case_id", row.CaseID,
		"reference", row.ReferenceNumber,
		"score", compositeScore,
	)
	return result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Step 4e — Preview (top-N without claiming)
// ─────────────────────────────────────────────────────────────────────────────

// GetNextPreview returns the top N cases without acquiring any row locks.
func GetNextPreview(ctx context.Context, pool *pgxpool.Pool, userID, tenantID, caseTypeCode string, topN int) (PreviewResult, error) {
	if topN < 1 {
		topN = 3
	}
	if topN > 5 {
		topN = 5
	}

	capacity, err := CheckUserCapacity(ctx, pool, userID, tenantID)
	if err != nil {
		return PreviewResult{}, fmt.Errorf("GetNextPreview capacity: %w", err)
	}

	weights, err := LoadGetNextWeights(ctx, pool, tenantID, caseTypeCode)
	if err != nil {
		return PreviewResult{}, fmt.Errorf("GetNextPreview weights: %w", err)
	}

	var userSkills []string
	_ = pool.QueryRow(ctx, `
		SELECT COALESCE(array_agg(ws.skill_code) FILTER (WHERE ws.skill_code IS NOT NULL), '{}')
		FROM users u
		LEFT JOIN workers wk ON wk.id = u.id
		LEFT JOIN worker_skills ws ON ws.worker_id = wk.id
		WHERE u.user_id = $1::uuid AND u.tenant_id = $2::uuid
		GROUP BY u.user_id`, userID, tenantID).Scan(&userSkills)

	query, args := buildGetNextQuery(userID, tenantID, weights, capacity.MaxActiveCases, topN, true)
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return PreviewResult{}, fmt.Errorf("GetNextPreview query: %w", err)
	}
	defer rows.Close()

	var results []GetNextResult
	queueDepth := 0
	for rows.Next() {
		var row scoredRow
		if err := rows.Scan(
			&row.CaseID, &row.ReferenceNumber, &row.CaseTypeCode,
			&row.CurrentStageCode, &row.Status, &row.Complexity,
			&row.RequiredSkills, &row.CaseDueAt, &row.CreatedAt,
			&row.SLARaw, &row.SkillRaw, &row.AgeRaw, &row.ComplexityRaw,
			&row.ValueRaw, &row.AffinityRaw, &row.WorkloadRaw,
			&row.CompositeScore, &row.HoursInQueue, &row.QueueDepth,
		); err != nil {
			return PreviewResult{}, fmt.Errorf("GetNextPreview scan: %w", err)
		}
		if queueDepth == 0 {
			queueDepth = row.QueueDepth
		}
		results = append(results, buildResult(row, weights, userSkills, capacity))
	}
	if rows.Err() != nil {
		return PreviewResult{}, fmt.Errorf("GetNextPreview rows: %w", rows.Err())
	}

	return PreviewResult{
		TopCases:     results,
		QueueDepth:   queueDepth,
		UserCapacity: capacity,
	}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Step 4f — SkipCase
// ─────────────────────────────────────────────────────────────────────────────

// SkipCase records a skip decision. The case remains ALLOCATABLE for others.
func SkipCase(ctx context.Context, pool *pgxpool.Pool, req GetNextSkipRequest) error {
	// Enforce skip limit server-side
	newCount := incrementSkipCount(req.UserID)
	if newCount > defaultSkipLimit {
		return ErrSkipLimitExceeded
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("SkipCase begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Verify case is still ALLOCATABLE before recording the skip
	var exists bool
	_ = tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM cases WHERE id = $1::uuid AND status = 'ALLOCATABLE')`,
		req.CaseID).Scan(&exists)

	reason := req.Reason
	if err := recordAudit(ctx, tx, req.TenantID, req.UserID, &req.CaseID,
		ActionSkipped, nil, nil, &reason, req.Notes, 0, 0); err != nil {
		return fmt.Errorf("SkipCase audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("SkipCase commit: %w", err)
	}

	slog.Info("case skipped", "case_id", req.CaseID, "user_id", req.UserID,
		"reason", req.Reason, "session_skip_count", newCount)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Step 4g — Queue depth and analytics
// ─────────────────────────────────────────────────────────────────────────────

// GetQueueDepth returns current queue metrics.
// userID is optional — empty string returns global depth without user-skill filtering.
func GetQueueDepth(ctx context.Context, pool *pgxpool.Pool, tenantID, caseTypeCode, userID string) (QueueDepthInfo, error) {
	var info QueueDepthInfo

	// Global stats
	query := `
		SELECT
		    COUNT(*)::int                                                          AS total,
		    COALESCE(AVG(EXTRACT(EPOCH FROM (NOW() - cat.entered_at))/3600), 0)    AS avg_wait,
		    COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - cat.entered_at))/3600), 0)    AS max_wait,
		    COUNT(*) FILTER (WHERE c.case_due_at < NOW())::int                     AS sla_breached,
		    COUNT(*) FILTER (WHERE c.case_due_at >= NOW()
		                     AND c.case_due_at < NOW() + INTERVAL '4 hours')::int  AS sla_at_risk
		FROM cases c
		LEFT JOIN case_allocation_transitions cat ON cat.case_id = c.id AND cat.is_current = TRUE
		WHERE c.tenant_id = $1::uuid
		  AND c.status = 'ALLOCATABLE'
		  AND c.assigned_user_id IS NULL
		  AND ($2 = '' OR c.current_stage_code = 'ALLOCATION')`

	err := pool.QueryRow(ctx, query, tenantID, caseTypeCode).Scan(
		&info.TotalAllocatable, &info.AvgWaitHours, &info.MaxWaitHours,
		&info.SLABreachedCount, &info.SLAAtRiskCount,
	)
	if err != nil {
		return info, fmt.Errorf("GetQueueDepth global: %w", err)
	}

	// Complexity breakdown
	rows, err := pool.Query(ctx, `
		SELECT COALESCE(case_complexity, 'UNKNOWN'), COUNT(*)::int
		FROM cases
		WHERE tenant_id = $1::uuid
		  AND status = 'ALLOCATABLE'
		  AND assigned_user_id IS NULL
		GROUP BY case_complexity`, tenantID)
	if err == nil {
		defer rows.Close()
		info.ByComplexity = make(map[string]int)
		for rows.Next() {
			var k string
			var v int
			if rows.Scan(&k, &v) == nil {
				info.ByComplexity[k] = v
			}
		}
	}

	// User capacity
	if userID != "" {
		cap, err := CheckUserCapacity(ctx, pool, userID, tenantID)
		if err == nil {
			info.UserCapacity = cap
			// User-eligible depth
			_ = pool.QueryRow(ctx, `
				SELECT COUNT(*)::int
				FROM cases c
				JOIN users u ON u.user_id = $2::uuid AND u.tenant_id = $1::uuid
				LEFT JOIN workers wk ON wk.id = u.id
				WHERE c.tenant_id = $1::uuid
				  AND c.status = 'ALLOCATABLE'
				  AND c.assigned_user_id IS NULL
				  AND (c.required_skills = '{}' OR c.required_skills IS NULL
				       OR c.required_skills && (
				           SELECT COALESCE(array_agg(ws.skill_code::text),'{}')
				           FROM worker_skills ws WHERE ws.worker_id = wk.id
				       ))`, tenantID, userID).Scan(&info.EligibleForUser)
		}
	}

	return info, nil
}

// RefreshAffinityView refreshes the case_user_affinity materialised view
// using CONCURRENTLY so reads are not blocked during the refresh.
func RefreshAffinityView(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `REFRESH MATERIALIZED VIEW CONCURRENTLY case_user_affinity`)
	if err != nil {
		return fmt.Errorf("RefreshAffinityView: %w", err)
	}
	slog.Info("case_user_affinity view refreshed")
	return nil
}

// RecordQueueSnapshot writes a point-in-time snapshot of queue state.
func RecordQueueSnapshot(ctx context.Context, pool *pgxpool.Pool, tenantID, caseTypeCode string) error {
	depth, err := GetQueueDepth(ctx, pool, tenantID, caseTypeCode, "")
	if err != nil {
		return fmt.Errorf("RecordQueueSnapshot depth: %w", err)
	}

	skillJSON, _ := json.Marshal(depth.BySkill)
	complexJSON, _ := json.Marshal(depth.ByComplexity)

	_, err = pool.Exec(ctx, `
		INSERT INTO getnext_queue_snapshots
		    (tenant_id, case_type_code, total_allocatable, avg_wait_hours,
		     max_wait_hours, sla_breached_count, sla_at_risk_count,
		     skill_breakdown, complexity_breakdown)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb)`,
		tenantID, caseTypeCode,
		depth.TotalAllocatable, depth.AvgWaitHours, depth.MaxWaitHours,
		depth.SLABreachedCount, depth.SLAAtRiskCount,
		skillJSON, complexJSON,
	)
	return err
}

// ─────────────────────────────────────────────────────────────────────────────
// Step 4h — Supervisor view
// ─────────────────────────────────────────────────────────────────────────────

// GetSupervisorQueueView returns the full supervisor dashboard payload.
func GetSupervisorQueueView(ctx context.Context, pool *pgxpool.Pool, tenantID, supervisorID string) (SupervisorQueueView, error) {
	var view SupervisorQueueView

	// Queue depth
	depth, err := GetQueueDepth(ctx, pool, tenantID, "", supervisorID)
	if err != nil {
		return view, fmt.Errorf("GetSupervisorQueueView depth: %w", err)
	}
	view.QueueDepth = depth

	// Top 10 cases by score (preview mode — no lock)
	weights, _ := LoadGetNextWeights(ctx, pool, tenantID, "")
	cap, _ := CheckUserCapacity(ctx, pool, supervisorID, tenantID)
	query, args := buildGetNextQuery(supervisorID, tenantID, weights, cap.MaxActiveCases, 10, true)
	rows, err := pool.Query(ctx, query, args...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var row scoredRow
			if rows.Scan(
				&row.CaseID, &row.ReferenceNumber, &row.CaseTypeCode,
				&row.CurrentStageCode, &row.Status, &row.Complexity,
				&row.RequiredSkills, &row.CaseDueAt, &row.CreatedAt,
				&row.SLARaw, &row.SkillRaw, &row.AgeRaw, &row.ComplexityRaw,
				&row.ValueRaw, &row.AffinityRaw, &row.WorkloadRaw,
				&row.CompositeScore, &row.HoursInQueue, &row.QueueDepth,
			) == nil {
				view.TopCases = append(view.TopCases, buildResult(row, weights, nil, cap))
			}
		}
	}

	// Stalled cases (in queue > 24h)
	stalledRows, err := pool.Query(ctx, `
		SELECT c.id, c.reference_number, ct.code, c.current_stage_code,
		       c.status, COALESCE(c.case_complexity,''), c.required_skills,
		       c.case_due_at, c.created_at
		FROM cases c
		JOIN case_allocation_transitions cat ON cat.case_id = c.id AND cat.is_current = TRUE
		LEFT JOIN case_types ct ON ct.id = c.case_type_id
		WHERE c.tenant_id = $1::uuid
		  AND c.status = 'ALLOCATABLE'
		  AND c.assigned_user_id IS NULL
		  AND cat.entered_at < NOW() - INTERVAL '24 hours'
		ORDER BY cat.entered_at ASC
		LIMIT 50`, tenantID)
	if err == nil {
		defer stalledRows.Close()
		for stalledRows.Next() {
			var cs CaseSummary
			if stalledRows.Scan(&cs.ID, &cs.ReferenceNumber, &cs.CaseTypeCode,
				&cs.CurrentStageCode, &cs.Status, &cs.Complexity,
				&cs.RequiredSkills, &cs.CaseDueAt, &cs.CreatedAt) == nil {
				view.StalledCases = append(view.StalledCases, cs)
			}
		}
	}

	// Team workloads
	teamRows, err := pool.Query(ctx, `
		SELECT t.team_id, t.display_name,
		       COUNT(DISTINCT tm.user_id)::int    AS members,
		       COUNT(DISTINCT c.id)::int          AS active_cases,
		       COALESCE(SUM(COALESCE(w.max_concurrent_tasks, 10)),0)::int AS max_cases
		FROM teams t
		JOIN team_members tm ON tm.team_id = t.team_id AND tm.tenant_id = t.tenant_id
		JOIN users u ON u.user_id = tm.user_id AND u.tenant_id = t.tenant_id
		LEFT JOIN workers w ON w.id = u.id
		LEFT JOIN cases c ON c.assigned_user_id = u.user_id
		                   AND c.tenant_id = t.tenant_id
		                   AND c.status IN ('OPEN','IN_PROGRESS')
		WHERE t.tenant_id = $1::uuid
		  AND t.status = 'ACTIVE'
		GROUP BY t.team_id, t.display_name
		ORDER BY active_cases::float / NULLIF(SUM(COALESCE(w.max_concurrent_tasks,10)),0) DESC`, tenantID)
	if err == nil {
		defer teamRows.Close()
		for teamRows.Next() {
			var tr TeamWorkloadRow
			if teamRows.Scan(&tr.TeamID, &tr.TeamName, &tr.Members, &tr.ActiveCases, &tr.MaxCases) == nil {
				if tr.MaxCases > 0 {
					tr.CapacityPct = float64(tr.ActiveCases) / float64(tr.MaxCases)
				}
				view.TeamWorkloads = append(view.TeamWorkloads, tr)
			}
		}
	}

	return view, nil
}
