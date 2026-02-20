package multitenancy

import (
	"encoding/json"
	"errors"
	"time"
)

const (
	// DefaultTenantID is used for legacy/system-scope rows where tenant is not explicitly set.
	DefaultTenantID = "00000000-0000-0000-0000-000000000000"
)

// TenantStatus is the lifecycle state for a tenant record.
type TenantStatus string

const (
	TenantStatusActive     TenantStatus = "ACTIVE"
	TenantStatusSuspended TenantStatus = "SUSPENDED"
	TenantStatusOffboarded TenantStatus = "OFFBOARDED"
)

// TenantTier controls configured limits and entitlements.
type TenantTier string

const (
	TenantTierStandard   TenantTier = "STANDARD"
	TenantTierPremium    TenantTier = "PREMIUM"
	TenantTierEnterprise TenantTier = "ENTERPRISE"
)

// TenantFeatureFlag is a typed feature flag key.
type TenantFeatureFlag string

const (
	TenantFeatureCompensationEnabled TenantFeatureFlag = "compensation_enabled"
	TenantFeatureDLQRequeueEnabled   TenantFeatureFlag = "dlq_requeue_enabled"
	TenantFeatureNotificationEnabled TenantFeatureFlag = "notification_enabled"
	TenantFeatureSubCaseEnabled      TenantFeatureFlag = "sub_case_enabled"
	TenantFeatureSLAEnforcement      TenantFeatureFlag = "sla_enforcement_enabled"
)

// TenantFeatureFlags maps to tenants.config.feature_flags.
type TenantFeatureFlags struct {
	CompensationEnabled bool `json:"compensation_enabled"`
	DLQRequeueEnabled   bool `json:"dlq_requeue_enabled"`
	NotificationEnabled bool `json:"notification_enabled"`
	SubCaseEnabled      bool `json:"sub_case_enabled"`
	SLAEnforcement      bool `json:"sla_enforcement_enabled"`
}

// TenantConfig maps to tenants.config JSONB payload.
type TenantConfig struct {
	MaxActiveCases      int                 `json:"max_active_cases"`
	MaxConcurrentTasks  int                 `json:"max_concurrent_tasks"`
	MaxCasesPerMinute   int                 `json:"max_cases_per_minute"`
	AllowedCaseTypeCodes []string           `json:"allowed_case_type_codes,omitempty"`
	FeatureFlags        TenantFeatureFlags  `json:"feature_flags"`
	SLAMultiplier       float64             `json:"sla_multiplier,omitempty"`
}

// Tenant is the persisted tenant registry row.
type Tenant struct {
	TenantID   string          `json:"tenant_id" db:"tenant_id"`
	TenantCode string          `json:"tenant_code" db:"tenant_code"`
	Name       string          `json:"name" db:"name"`
	Status     TenantStatus    `json:"status" db:"status"`
	Tier       TenantTier      `json:"tier" db:"tier"`
	Config     json.RawMessage `json:"config" db:"config"`
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at" db:"updated_at"`
}

// TenantRateLimitCounter persists minute-level case creation counters.
type TenantRateLimitCounter struct {
	TenantID    string    `json:"tenant_id" db:"tenant_id"`
	WindowStart time.Time `json:"window_start" db:"window_start"`
	CaseCount   int       `json:"case_count" db:"case_count"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// OnboardTenantInput defines operator-controlled onboarding inputs.
type OnboardTenantInput struct {
	TenantCode string       `json:"tenant_code"`
	Name       string       `json:"name"`
	Tier       TenantTier   `json:"tier"`
	Config     TenantConfig `json:"config"`
	CreatedBy  string       `json:"created_by"`
}

// TenantConfigValidationError surfaces all limit violations.
type TenantConfigValidationError struct {
	Violations []string
}

func (e *TenantConfigValidationError) Error() string {
	if e == nil || len(e.Violations) == 0 {
		return "tenant config validation failed"
	}
	msg := "tenant config validation failed"
	for i := range e.Violations {
		msg += "; " + e.Violations[i]
	}
	return msg
}

// tenantContextKey stores tenant id in request context.
type tenantContextKey struct{}

var (
	ErrTenantSuspended       = errors.New("tenant is suspended")
	ErrTenantOffboarded      = errors.New("tenant is offboarded")
	ErrTenantNotFound        = errors.New("tenant not found")
	ErrCaseTypeForbidden     = errors.New("case type is not visible to tenant")
	ErrTenantCapacityExceeded = errors.New("tenant capacity exceeded")
)
