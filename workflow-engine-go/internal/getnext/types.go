package getnext

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Sentinel errors
// ─────────────────────────────────────────────────────────────────────────────

var (
	ErrNoEligibleCases    = errors.New("no eligible cases found matching your skills and criteria")
	ErrUserAtCapacity     = errors.New("user has reached maximum active case limit")
	ErrCaseAlreadyClaimed = errors.New("case was claimed by another operator — please try again")
	ErrInvalidWeights     = errors.New("scoring weights do not sum to 1.000 (±0.001 tolerance)")
	ErrSkipLimitExceeded  = errors.New("skip limit exceeded — contact your supervisor to continue")
)

// ─────────────────────────────────────────────────────────────────────────────
// String enums
// ─────────────────────────────────────────────────────────────────────────────

type GetNextClaimAction string

const (
	ActionClaimed         GetNextClaimAction = "CLAIMED"
	ActionSkipped         GetNextClaimAction = "SKIPPED"
	ActionPreview         GetNextClaimAction = "PREVIEW"
	ActionCapacityBlocked GetNextClaimAction = "CAPACITY_BLOCKED"
	ActionNoEligibleCases GetNextClaimAction = "NO_ELIGIBLE_CASES"
)

type SkipReason string

const (
	SkipReasonFreeText           SkipReason = "FREE_TEXT"
	SkipReasonConflictOfInterest SkipReason = "CONFLICT_OF_INTEREST"
	SkipReasonTooComplex         SkipReason = "TOO_COMPLEX"
	SkipReasonWrongSkill         SkipReason = "WRONG_SKILL"
	SkipReasonOther              SkipReason = "OTHER"
)

// ─────────────────────────────────────────────────────────────────────────────
// Weight configuration
// ─────────────────────────────────────────────────────────────────────────────

// GetNextWeights holds the seven scoring weights. All must sum to 1.0 (±0.001).
type GetNextWeights struct {
	WSla        float64 `json:"wSla"        db:"w_sla"`
	WSkill      float64 `json:"wSkill"      db:"w_skill"`
	WAge        float64 `json:"wAge"        db:"w_age"`
	WComplexity float64 `json:"wComplexity" db:"w_complexity"`
	WValue      float64 `json:"wValue"      db:"w_value"`
	WAffinity   float64 `json:"wAffinity"   db:"w_affinity"`
	WWorkload   float64 `json:"wWorkload"   db:"w_workload"`
}

// DefaultWeights returns the canonical default scoring weights.
func DefaultWeights() GetNextWeights {
	return GetNextWeights{
		WSla:        0.35,
		WSkill:      0.25,
		WAge:        0.10,
		WComplexity: 0.10,
		WValue:      0.10,
		WAffinity:   0.05,
		WWorkload:   0.05,
	}
}

// Validate returns an error when the weights do not sum to 1.000 (±0.001).
func (w GetNextWeights) Validate() error {
	sum := w.WSla + w.WSkill + w.WAge + w.WComplexity + w.WValue + w.WAffinity + w.WWorkload
	if math.Abs(sum-1.0) > 0.001 {
		return fmt.Errorf("%w: wSla=%.4f wSkill=%.4f wAge=%.4f wComplexity=%.4f wValue=%.4f wAffinity=%.4f wWorkload=%.4f sum=%.4f",
			ErrInvalidWeights, w.WSla, w.WSkill, w.WAge, w.WComplexity, w.WValue, w.WAffinity, w.WWorkload, sum)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Score breakdown
// ─────────────────────────────────────────────────────────────────────────────

// ScoreFactor holds the raw, weighted, and explanatory breakdown for one factor.
type ScoreFactor struct {
	RawScore      float64 `json:"rawScore"`
	Weight        float64 `json:"weight"`
	WeightedScore float64 `json:"weightedScore"`
	Explanation   string  `json:"explanation"`
}

// ScoreBreakdown holds the full breakdown for all seven scoring factors.
type ScoreBreakdown struct {
	SLA         ScoreFactor    `json:"sla"`
	Skill       ScoreFactor    `json:"skill"`
	Age         ScoreFactor    `json:"age"`
	Complexity  ScoreFactor    `json:"complexity"`
	Value       ScoreFactor    `json:"value"`
	Affinity    ScoreFactor    `json:"affinity"`
	Workload    ScoreFactor    `json:"workload"`
	WeightsUsed GetNextWeights `json:"weightsUsed"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Capacity
// ─────────────────────────────────────────────────────────────────────────────

// UserCapacityInfo describes the requesting user's current workload state.
type UserCapacityInfo struct {
	ActiveCases    int     `json:"activeCases"`
	MaxActiveCases int     `json:"maxActiveCases"`
	CapacityPct    float64 `json:"capacityPct"`    // 0.0–1.0
	IsAtCapacity   bool    `json:"isAtCapacity"`   // >= 100%
	IsNearCapacity bool    `json:"isNearCapacity"` // >= 75%
}

// ─────────────────────────────────────────────────────────────────────────────
// Case summary (returned with each GetNext result)
// ─────────────────────────────────────────────────────────────────────────────

// CaseSummary is a lightweight case projection returned inside GetNextResult.
type CaseSummary struct {
	ID               string     `json:"id"`
	ReferenceNumber  string     `json:"referenceNumber"`
	CaseTypeCode     string     `json:"caseTypeCode"`
	CurrentStageCode string     `json:"currentStageCode"`
	Status           string     `json:"status"`
	Complexity       string     `json:"complexity"`
	RequiredSkills   []string   `json:"requiredSkills"`
	CaseDueAt        *time.Time `json:"caseDueAt"`
	CreatedAt        time.Time  `json:"createdAt"`
}

// ─────────────────────────────────────────────────────────────────────────────
// GetNext results
// ─────────────────────────────────────────────────────────────────────────────

// GetNextResult is returned from GetNext (claim) and from each item in Preview.
type GetNextResult struct {
	Case           CaseSummary    `json:"case"`
	CompositeScore float64        `json:"compositeScore"`
	ScoreBreakdown ScoreBreakdown `json:"scoreBreakdown"`
	QueueDepth     int            `json:"queueDepth"`
}

// PreviewResult is returned from GetNextPreview (read-only, no lock).
type PreviewResult struct {
	TopCases     []GetNextResult  `json:"topCases"`
	QueueDepth   int              `json:"queueDepth"`
	UserCapacity UserCapacityInfo `json:"userCapacity"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Queue depth
// ─────────────────────────────────────────────────────────────────────────────

// QueueDepthInfo is returned from GetQueueDepth.
type QueueDepthInfo struct {
	TotalAllocatable int              `json:"totalAllocatable"`
	EligibleForUser  int              `json:"eligibleForUser"`
	AvgWaitHours     float64          `json:"avgWaitHours"`
	MaxWaitHours     float64          `json:"maxWaitHours"`
	SLABreachedCount int              `json:"slaBreachedCount"`
	SLAAtRiskCount   int              `json:"slaAtRiskCount"` // < 4h remaining
	ByComplexity     map[string]int   `json:"byComplexity"`
	BySkill          map[string]int   `json:"bySkill"`
	UserCapacity     UserCapacityInfo `json:"userCapacity"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Supervisor view
// ─────────────────────────────────────────────────────────────────────────────

// TeamWorkloadRow is one row in the supervisor team workload table.
type TeamWorkloadRow struct {
	TeamID      string  `json:"teamId"`
	TeamName    string  `json:"teamName"`
	Members     int     `json:"members"`
	ActiveCases int     `json:"activeCases"`
	MaxCases    int     `json:"maxCases"`
	CapacityPct float64 `json:"capacityPct"`
}

// UserSummary is a lightweight user projection for supervisor views.
type UserSummary struct {
	UserID      string   `json:"userId"`
	DisplayName string   `json:"displayName"`
	Email       string   `json:"email"`
	ActiveCases int      `json:"activeCases"`
	MaxCases    int      `json:"maxCases"`
	CapacityPct float64  `json:"capacityPct"`
	Skills      []string `json:"skills"`
}

// SupervisorQueueView aggregates all data needed for the supervisor dashboard.
type SupervisorQueueView struct {
	QueueDepth    QueueDepthInfo    `json:"queueDepth"`
	TopCases      []GetNextResult   `json:"topCases"` // top 10 by composite score
	TeamWorkloads []TeamWorkloadRow `json:"teamWorkloads"`
	IdleOperators []UserSummary     `json:"idleOperators"` // active, 0 cases
	AtCapacityOps []UserSummary     `json:"atCapacityOps"` // >= 90% capacity
	StalledCases  []CaseSummary     `json:"stalledCases"`  // in queue > 24h
}

// ─────────────────────────────────────────────────────────────────────────────
// Request/Response types
// ─────────────────────────────────────────────────────────────────────────────

// GetNextRequest is the input to GetNext.
type GetNextRequest struct {
	UserID       string `json:"userId"`
	TenantID     string `json:"tenantId"`
	CaseTypeCode string `json:"caseTypeCode"` // optional filter
	Preview      bool   `json:"preview"`      // true = score without claiming
}

// GetNextSkipRequest is the input to SkipCase.
type GetNextSkipRequest struct {
	UserID   string     `json:"userId"`
	TenantID string     `json:"tenantId"`
	CaseID   string     `json:"caseId"`
	Reason   SkipReason `json:"reason"`
	Notes    string     `json:"notes"`
}
