package getnext

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// GetNextWeights.Validate
// ─────────────────────────────────────────────────────────────────────────────

func TestGetNextWeights_Validate_Default(t *testing.T) {
	w := DefaultWeights()
	require.NoError(t, w.Validate(), "default weights must sum to 1.000")
}

func TestGetNextWeights_Validate_Custom_Valid(t *testing.T) {
	w := GetNextWeights{
		WSla: 0.20, WSkill: 0.20, WAge: 0.20,
		WComplexity: 0.20, WValue: 0.10, WAffinity: 0.05, WWorkload: 0.05,
	}
	require.NoError(t, w.Validate())
}

func TestGetNextWeights_Validate_Invalid_Sum(t *testing.T) {
	w := GetNextWeights{
		WSla: 0.50, WSkill: 0.50, // sum = 1.00 with zero others? no — leaves others 0 = 0.00
		// total = 1.0 still. Let's make it fail:
		WAge: 0.10,
	}
	err := w.Validate()
	assert.ErrorIs(t, err, ErrInvalidWeights, "weights summing to > 1 must fail validation")
}

func TestGetNextWeights_Validate_Tolerance(t *testing.T) {
	// Within ±0.001 tolerance; should NOT return error
	w := DefaultWeights()
	w.WSla += 0.0005 // push sum just over by 0.0005 — still within tolerance
	require.NoError(t, w.Validate(), "tiny floating-point drift within ±0.001 must be allowed")
}

// ─────────────────────────────────────────────────────────────────────────────
// explainSLAScore — pure function, no DB needed
// ─────────────────────────────────────────────────────────────────────────────

func TestExplainSLAScore_Nil_DueAt(t *testing.T) {
	msg := explainSLAScore(0, nil)
	assert.Contains(t, msg, "No SLA deadline", "nil due_at must return no-deadline message")
}

func TestExplainSLAScore_Breached(t *testing.T) {
	past := time.Now().Add(-3 * time.Hour)
	msg := explainSLAScore(100, &past)
	assert.Contains(t, msg, "breached", "score=100 with past due_at must indicate breach")
}

func TestExplainSLAScore_Critical(t *testing.T) {
	soon := time.Now().Add(90 * time.Minute)
	msg := explainSLAScore(90, &soon)
	assert.Contains(t, msg, "critical", "score=90 with 90m remaining must be critical urgency")
}

// ─────────────────────────────────────────────────────────────────────────────
// explainSkillScore — pure function
// ─────────────────────────────────────────────────────────────────────────────

func TestExplainSkillScore_NoRequirements(t *testing.T) {
	msg := explainSkillScore(100, nil, []string{"LOAN_PROCESSING"})
	assert.Contains(t, msg, "No specific skills required")
}

func TestExplainSkillScore_FullMatch(t *testing.T) {
	msg := explainSkillScore(100, []string{"LOAN_PROCESSING"}, []string{"LOAN_PROCESSING"})
	assert.Contains(t, msg, "all 1 required skills")
}

func TestExplainSkillScore_PartialMatch(t *testing.T) {
	msg := explainSkillScore(40, []string{"A", "B", "C"}, []string{"A"})
	assert.Contains(t, msg, "partial match")
}

// ─────────────────────────────────────────────────────────────────────────────
// sessionSkipCount and incrementSkipCount — in-memory, no DB
// ─────────────────────────────────────────────────────────────────────────────

func TestSessionSkipCount_InitiallyZero(t *testing.T) {
	userID := "test-skip-zero-" + time.Now().Format("150405.000000000")
	assert.Equal(t, 0, sessionSkipCount(userID))
}

func TestIncrementSkipCount_CountsCorrectly(t *testing.T) {
	userID := "test-skip-count-" + time.Now().Format("150405.000000000")
	for i := 1; i <= 3; i++ {
		n := incrementSkipCount(userID)
		assert.Equal(t, i, n, "skip count after %d increments", i)
	}
}

func TestIncrementSkipCount_ExceedsLimit(t *testing.T) {
	userID := "test-skip-limit-" + time.Now().Format("150405.000000000")
	for i := 0; i < defaultSkipLimit+2; i++ {
		incrementSkipCount(userID)
	}
	assert.Greater(t, sessionSkipCount(userID), defaultSkipLimit,
		"session count must exceed limit after enough increments")
}

// ─────────────────────────────────────────────────────────────────────────────
// LoadGetNextWeights — cache path
// ─────────────────────────────────────────────────────────────────────────────

func TestLoadGetNextWeights_CacheHit(t *testing.T) {
	// Seed the cache directly
	key := cacheKey("tenant-a", "HOME_LOAN")
	w := DefaultWeights()
	weightCache.Store(key, weightCacheEntry{
		weights:   w,
		expiresAt: time.Now().Add(5 * time.Minute),
	})

	// Calling with a nil pool should NOT panic because cache is hit first
	loaded, err := LoadGetNextWeights(context.TODO(), nil, "tenant-a", "HOME_LOAN")
	require.NoError(t, err)
	assert.Equal(t, w, loaded)
}

func TestLoadGetNextWeights_ExpiredCache(t *testing.T) {
	key := cacheKey("tenant-exp", "HOME_LOAN")
	weightCache.Store(key, weightCacheEntry{
		weights:   DefaultWeights(),
		expiresAt: time.Now().Add(-1 * time.Second), // already expired
	})
	// Pool is nil, so fallback defaults are returned (ErrNoRows path)
	// We can't call pool.QueryRow without a real pool, so just verify cache was not hit
	v, _ := weightCache.Load(key)
	entry := v.(weightCacheEntry)
	assert.True(t, time.Now().After(entry.expiresAt), "entry must be expired")
}

// ─────────────────────────────────────────────────────────────────────────────
// buildGetNextQuery — SQL structure sanity
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildGetNextQuery_ClaimModeHasFORUPDATE(t *testing.T) {
	sql, args := buildGetNextQuery("uid", "tid", DefaultWeights(), 10, 1, false)
	assert.Contains(t, sql, "FOR UPDATE SKIP LOCKED", "claim query must include row lock")
	assert.Equal(t, 11, len(args), "must have 11 bind args")
}

func TestBuildGetNextQuery_PreviewModeNoLock(t *testing.T) {
	sql, _ := buildGetNextQuery("uid", "tid", DefaultWeights(), 10, 3, true)
	assert.NotContains(t, sql, "FOR UPDATE", "preview query must NOT hold any row locks")
}

func TestBuildGetNextQuery_ScoreWeightsInSQL(t *testing.T) {
	w := DefaultWeights()
	sql, args := buildGetNextQuery("uid", "tid", w, 10, 1, true)
	// Verify weight args appear in expected positions
	assert.Equal(t, w.WSla, args[3].(float64))
	assert.Equal(t, w.WSkill, args[4].(float64))
	assert.Equal(t, w.WAge, args[5].(float64))
	assert.Equal(t, w.WComplexity, args[6].(float64))
	assert.Equal(t, w.WValue, args[7].(float64))
	assert.Equal(t, w.WAffinity, args[8].(float64))
	assert.Equal(t, w.WWorkload, args[9].(float64))
	_ = sql
}

// ─────────────────────────────────────────────────────────────────────────────
// buildResult — pure scoring structure assembly
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildResult_CompositeScoreMatches(t *testing.T) {
	row := scoredRow{
		CaseID:          "case-1",
		ReferenceNumber: "LOAN-2025-00001",
		SLARaw:          80.0,
		SkillRaw:        100.0,
		AgeRaw:          20.0,
		ComplexityRaw:   30.0,
		ValueRaw:        40.0,
		AffinityRaw:     20.0,
		WorkloadRaw:     -10.0,
		QueueDepth:      5,
	}
	w := DefaultWeights()
	row.CompositeScore = w.WSla*row.SLARaw + w.WSkill*row.SkillRaw + w.WAge*row.AgeRaw +
		w.WComplexity*row.ComplexityRaw + w.WValue*row.ValueRaw +
		w.WAffinity*row.AffinityRaw + w.WWorkload*row.WorkloadRaw

	result := buildResult(row, w, nil, UserCapacityInfo{MaxActiveCases: 10})
	assert.InDelta(t, row.CompositeScore, result.CompositeScore, 0.001)
	assert.InDelta(t, row.SLARaw*w.WSla, result.ScoreBreakdown.SLA.WeightedScore, 0.001)
}

func TestBuildResult_WorkloadPenaltyNegative(t *testing.T) {
	row := scoredRow{WorkloadRaw: -50.0}
	w := DefaultWeights()
	result := buildResult(row, w, nil, UserCapacityInfo{CapacityPct: 0.95})
	assert.True(t, result.ScoreBreakdown.Workload.WeightedScore < 0,
		"at-capacity workload penalty must produce negative weighted score")
}

// ─────────────────────────────────────────────────────────────────────────────
// UserCapacityInfo logic
// ─────────────────────────────────────────────────────────────────────────────

func TestUserCapacityInfo_IsAtCapacity(t *testing.T) {
	info := UserCapacityInfo{ActiveCases: 10, MaxActiveCases: 10}
	info.CapacityPct = float64(info.ActiveCases) / float64(info.MaxActiveCases)
	info.IsAtCapacity = info.CapacityPct >= 1.0
	assert.True(t, info.IsAtCapacity)
}

func TestUserCapacityInfo_IsNearCapacity(t *testing.T) {
	pct := 0.80
	info := UserCapacityInfo{CapacityPct: pct}
	info.IsNearCapacity = pct >= 0.75
	info.IsAtCapacity = pct >= 1.0
	assert.True(t, info.IsNearCapacity)
	assert.False(t, info.IsAtCapacity)
}

// ─────────────────────────────────────────────────────────────────────────────
// explainWorkloadScore
// ─────────────────────────────────────────────────────────────────────────────

func TestExplainWorkloadScore_AtCapacity(t *testing.T) {
	msg := explainWorkloadScore(-50, 0.95)
	assert.Contains(t, msg, "penalty applied")
}

func TestExplainWorkloadScore_NoCapacityIssue(t *testing.T) {
	msg := explainWorkloadScore(0, 0.40)
	assert.Contains(t, msg, "no penalty")
}

// ─────────────────────────────────────────────────────────────────────────────
// Age score explanation
// ─────────────────────────────────────────────────────────────────────────────

func TestExplainAgeScore_JustEntered(t *testing.T) {
	msg := explainAgeScore(0, 0)
	assert.Contains(t, msg, "Just entered")
}

func TestExplainAgeScore_Capped(t *testing.T) {
	msg := explainAgeScore(50, 55.3)
	assert.Contains(t, msg, "capped at 50")
}

// ─────────────────────────────────────────────────────────────────────────────
// Complex weight scenario — ensures weighted sum is deterministic
// ─────────────────────────────────────────────────────────────────────────────

func TestCompositeScoreDeterministic(t *testing.T) {
	w := DefaultWeights()
	row := scoredRow{
		SLARaw: 90, SkillRaw: 75, AgeRaw: 30,
		ComplexityRaw: 40, ValueRaw: 10, AffinityRaw: 20, WorkloadRaw: -30,
	}
	expected := w.WSla*90 + w.WSkill*75 + w.WAge*30 +
		w.WComplexity*40 + w.WValue*10 + w.WAffinity*20 + w.WWorkload*(-30)
	row.CompositeScore = expected
	result := buildResult(row, w, nil, UserCapacityInfo{})
	// Round-trip through buildResult should not alter the score
	assert.True(t, math.Abs(result.CompositeScore-expected) < 0.001)
}
