package multitenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"workflow-engine/internal/identity"
	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

var (
	tenantCodePattern = regexp.MustCompile(`^[A-Z0-9_]{2,20}$`)

	featureConfigCache sync.Map
	featureCacheTTLNS  atomic.Int64
)

func init() {
	featureCacheTTLNS.Store(int64((60 * time.Second)))
}

type tenantCacheEntry struct {
	status    TenantStatus
	config    TenantConfig
	expiresAt time.Time
}

// SetTenantFeatureCacheTTL configures in-process tenant config cache TTL.
func SetTenantFeatureCacheTTL(ttl time.Duration) {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	featureCacheTTLNS.Store(int64(ttl))
}

func featureCacheTTL() time.Duration {
	ttl := time.Duration(featureCacheTTLNS.Load())
	if ttl <= 0 {
		return 60 * time.Second
	}
	return ttl
}

// InvalidateTenantFeatureCache clears a single tenant cache entry.
func InvalidateTenantFeatureCache(tenantID string) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return
	}
	featureConfigCache.Delete(tenantID)
}

// InvalidateAllTenantFeatureCache clears all tenant cache entries.
func InvalidateAllTenantFeatureCache() {
	featureConfigCache.Range(func(key interface{}, value interface{}) bool {
		featureConfigCache.Delete(key)
		return true
	})
}

// TenantFeatureEnabled checks a feature flag with TTL-backed in-process cache.
func TenantFeatureEnabled(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	featureFlag TenantFeatureFlag,
) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("TenantFeatureEnabled: db is nil")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		if ctxTenant, err := TenantFromContext(ctx); err == nil {
			tenantID = ctxTenant
		}
	}
	if tenantID == "" {
		return false, fmt.Errorf("TenantFeatureEnabled: %w", ErrTenantNotFound)
	}

	now := time.Now().UTC()
	if raw, ok := featureConfigCache.Load(tenantID); ok {
		entry, castOK := raw.(tenantCacheEntry)
		if castOK && now.Before(entry.expiresAt) {
			return resolveFeatureFlag(entry.config.FeatureFlags, featureFlag), nil
		}
		featureConfigCache.Delete(tenantID)
	}

	tenant, cfg, err := loadTenantAndConfig(ctx, db, tenantID)
	if err != nil {
		return false, fmt.Errorf("TenantFeatureEnabled: %w", err)
	}
	entry := tenantCacheEntry{
		status:    tenant.Status,
		config:    cfg,
		expiresAt: now.Add(featureCacheTTL()),
	}
	featureConfigCache.Store(tenantID, entry)

	return resolveFeatureFlag(cfg.FeatureFlags, featureFlag), nil
}

func resolveFeatureFlag(flags TenantFeatureFlags, featureFlag TenantFeatureFlag) bool {
	switch featureFlag {
	case TenantFeatureCompensationEnabled:
		return flags.CompensationEnabled
	case TenantFeatureDLQRequeueEnabled:
		return flags.DLQRequeueEnabled
	case TenantFeatureNotificationEnabled:
		return flags.NotificationEnabled
	case TenantFeatureSubCaseEnabled:
		return flags.SubCaseEnabled
	case TenantFeatureSLAEnforcement:
		return flags.SLAEnforcement
	default:
		return false
	}
}

// AssertTenantOperational ensures tenant exists and is ACTIVE.
func AssertTenantOperational(ctx context.Context, db *sqlx.DB, tenantID string) error {
	if db == nil {
		return fmt.Errorf("AssertTenantOperational: db is nil")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		if fromCtx, err := TenantFromContext(ctx); err == nil {
			tenantID = fromCtx
		}
	}
	if tenantID == "" {
		return fmt.Errorf("AssertTenantOperational: %w", ErrTenantNotFound)
	}

	var status string
	err := db.GetContext(ctx, &status, `
		SELECT status
		FROM tenants
		WHERE tenant_id = $1::uuid
	`, tenantID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("AssertTenantOperational: %w", ErrTenantNotFound)
		}
		return fmt.Errorf("AssertTenantOperational: query tenant status: %w", err)
	}

	switch TenantStatus(strings.ToUpper(strings.TrimSpace(status))) {
	case TenantStatusActive:
		return nil
	case TenantStatusSuspended:
		return fmt.Errorf("AssertTenantOperational: %w", ErrTenantSuspended)
	case TenantStatusOffboarded:
		return fmt.Errorf("AssertTenantOperational: %w", ErrTenantOffboarded)
	default:
		return fmt.Errorf("AssertTenantOperational: unknown tenant status %s", status)
	}
}

// AssertTenantScope appends tenant filter and binds tenant_id from context.
func AssertTenantScope(
	ctx context.Context,
	query string,
	args []interface{},
) (string, []interface{}, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		if shouldPanicOnMissingTenantScope() {
			return "", nil, fmt.Errorf("tenant scope missing from context")
		}
		return "", nil, fmt.Errorf("AssertTenantScope: %w", err)
	}

	scopedQuery := strings.TrimSpace(query)
	if scopedQuery == "" {
		return "", nil, fmt.Errorf("AssertTenantScope: query is empty")
	}
	if hasTenantPredicate(scopedQuery) {
		return scopedQuery, args, nil
	}

	hasSemicolon := strings.HasSuffix(scopedQuery, ";")
	if hasSemicolon {
		scopedQuery = strings.TrimSuffix(scopedQuery, ";")
	}

	upper := strings.ToUpper(scopedQuery)
	insertIdx := len(scopedQuery)
	for _, kw := range []string{" FOR UPDATE", " RETURNING", " ORDER BY", " LIMIT", " GROUP BY"} {
		if idx := strings.Index(upper, kw); idx >= 0 && idx < insertIdx {
			insertIdx = idx
		}
	}

	head := strings.TrimRight(scopedQuery[:insertIdx], " \n\t\r")
	tail := scopedQuery[insertIdx:]
	placeholder := fmt.Sprintf("$%d", len(args)+1)

	if strings.Contains(strings.ToUpper(head), " WHERE ") {
		head = head + " AND tenant_id = " + placeholder
	} else {
		head = head + " WHERE tenant_id = " + placeholder
	}

	scopedQuery = head + tail
	if hasSemicolon {
		scopedQuery += ";"
	}
	return scopedQuery, append(args, tenantID), nil
}

func hasTenantPredicate(query string) bool {
	upper := strings.ToUpper(query)
	return strings.Contains(upper, "TENANT_ID =") || strings.Contains(upper, "TENANT_ID=")
}

func shouldPanicOnMissingTenantScope() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("TENANT_SCOPE_ENFORCEMENT")))
	return v == "panic"
}

// IsCaseTypeVisibleToTenant returns true when case type is global or tenant-owned.
func IsCaseTypeVisibleToTenant(
	ctx context.Context,
	db *sqlx.DB,
	caseTypeCode string,
	tenantID string,
) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("IsCaseTypeVisibleToTenant: db is nil")
	}
	caseTypeCode = strings.ToUpper(strings.TrimSpace(caseTypeCode))
	tenantID = strings.TrimSpace(tenantID)
	if caseTypeCode == "" {
		return false, fmt.Errorf("IsCaseTypeVisibleToTenant: caseTypeCode is required")
	}
	if tenantID == "" {
		if fromCtx, err := TenantFromContext(ctx); err == nil {
			tenantID = fromCtx
		}
	}
	if tenantID == "" {
		return false, fmt.Errorf("IsCaseTypeVisibleToTenant: %w", ErrTenantNotFound)
	}

	var visible bool
	if err := db.GetContext(ctx, &visible, `
		SELECT EXISTS (
			SELECT 1
			FROM case_types
			WHERE code = $1
			  AND status = 'ACTIVE'
			  AND (tenant_id IS NULL OR tenant_id = $2::uuid)
		)
	`, caseTypeCode, tenantID); err != nil {
		return false, fmt.Errorf("IsCaseTypeVisibleToTenant: query visibility: %w", err)
	}
	return visible, nil
}

// EnforceTenantCaseLimits validates configured active-case and per-minute limits.
func EnforceTenantCaseLimits(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
) error {
	if db == nil {
		return fmt.Errorf("EnforceTenantCaseLimits: db is nil")
	}
	tenant, cfg, err := loadTenantAndConfig(ctx, db, tenantID)
	if err != nil {
		return fmt.Errorf("EnforceTenantCaseLimits: %w", err)
	}
	if err := assertTenantStatus(tenant.Status); err != nil {
		return fmt.Errorf("EnforceTenantCaseLimits: %w", err)
	}

	if cfg.MaxActiveCases > 0 {
		var activeCount int
		err = db.GetContext(ctx, &activeCount, `
			SELECT COUNT(*)::int
			FROM cases
			WHERE tenant_id = $1::uuid
			  AND status NOT IN ('COMPLETED', 'CANCELLED', 'REJECTED', 'CLONED')
		`, tenant.TenantID)
		if err != nil {
			return fmt.Errorf("EnforceTenantCaseLimits: count active cases: %w", err)
		}
		if activeCount >= cfg.MaxActiveCases {
			return fmt.Errorf("EnforceTenantCaseLimits: %w: max_active_cases=%d current=%d", ErrTenantCapacityExceeded, cfg.MaxActiveCases, activeCount)
		}
	}

	if cfg.MaxCasesPerMinute > 0 {
		tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			return fmt.Errorf("EnforceTenantCaseLimits: begin tx: %w", err)
		}
		defer func() {
			_ = tx.Rollback()
		}()

		windowStart := time.Now().UTC().Truncate(time.Minute)
		var caseCount int
		if err := tx.GetContext(ctx, &caseCount, `
			INSERT INTO tenant_rate_limit_counters (tenant_id, window_start, case_count)
			VALUES ($1::uuid, $2, 1)
			ON CONFLICT (tenant_id, window_start)
			DO UPDATE SET
				case_count = tenant_rate_limit_counters.case_count + 1,
				updated_at = now()
			RETURNING case_count
		`, tenant.TenantID, windowStart); err != nil {
			return fmt.Errorf("EnforceTenantCaseLimits: increment minute counter: %w", err)
		}

		if caseCount > cfg.MaxCasesPerMinute {
			return fmt.Errorf("EnforceTenantCaseLimits: %w: max_cases_per_minute=%d current=%d", ErrTenantCapacityExceeded, cfg.MaxCasesPerMinute, caseCount)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("EnforceTenantCaseLimits: commit rate counter increment: %w", err)
		}
	}

	slog.Info("tenant case limits validated", "tenant_id", tenant.TenantID)
	return nil
}

// EnforceTenantTaskLimits validates configured in-progress task capacity.
func EnforceTenantTaskLimits(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
) error {
	if db == nil {
		return fmt.Errorf("EnforceTenantTaskLimits: db is nil")
	}
	tenant, cfg, err := loadTenantAndConfig(ctx, db, tenantID)
	if err != nil {
		return fmt.Errorf("EnforceTenantTaskLimits: %w", err)
	}
	if err := assertTenantStatus(tenant.Status); err != nil {
		return fmt.Errorf("EnforceTenantTaskLimits: %w", err)
	}

	if cfg.MaxConcurrentTasks <= 0 {
		return nil
	}

	var inProgress int
	if err := db.GetContext(ctx, &inProgress, `
		SELECT COUNT(*)::int
		FROM tasks
		WHERE tenant_id = $1::uuid
		  AND status IN ('ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL')
	`, tenant.TenantID); err != nil {
		return fmt.Errorf("EnforceTenantTaskLimits: count in-progress tasks: %w", err)
	}
	if inProgress >= cfg.MaxConcurrentTasks {
		return fmt.Errorf("EnforceTenantTaskLimits: %w: max_concurrent_tasks=%d current=%d", ErrTenantCapacityExceeded, cfg.MaxConcurrentTasks, inProgress)
	}

	slog.Info("tenant task limits validated", "tenant_id", tenant.TenantID)
	return nil
}

// PrepareEventForPublish injects tenant_id into event payload and row model.
func PrepareEventForPublish(ctx context.Context, event model.Event) (model.Event, error) {
	if event.Payload == nil {
		event.Payload = json.RawMessage("{}")
	}

	tenantID := strings.TrimSpace(event.TenantID)
	if tenantID == "" {
		if fromCtx, err := TenantFromContext(ctx); err == nil {
			tenantID = fromCtx
		}
	}
	if tenantID == "" {
		tenantID = TenantFromPayload(event.Payload)
	}
	if tenantID == "" {
		tenantID = DefaultTenantID
	}

	payloadMap := make(map[string]interface{})
	if len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &payloadMap); err != nil {
			return model.Event{}, fmt.Errorf("PrepareEventForPublish: unmarshal payload: %w", err)
		}
	}
	payloadMap["tenant_id"] = tenantID

	rawPayload, err := json.Marshal(payloadMap)
	if err != nil {
		return model.Event{}, fmt.Errorf("PrepareEventForPublish: marshal payload: %w", err)
	}
	event.Payload = rawPayload
	event.TenantID = tenantID
	return event, nil
}

// OnboardTenant registers a new ACTIVE tenant after validation and publishes TENANT_ONBOARDED.
func OnboardTenant(
	ctx context.Context,
	db *sqlx.DB,
	input OnboardTenantInput,
) (Tenant, error) {
	if db == nil {
		return Tenant{}, fmt.Errorf("OnboardTenant: db is nil")
	}
	input.TenantCode = strings.ToUpper(strings.TrimSpace(input.TenantCode))
	input.Name = strings.TrimSpace(input.Name)
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)
	if input.CreatedBy == "" {
		input.CreatedBy = "system"
	}
	if !tenantCodePattern.MatchString(input.TenantCode) {
		return Tenant{}, fmt.Errorf("OnboardTenant: tenant_code must match [A-Z0-9_]{2,20}")
	}
	if input.Name == "" {
		return Tenant{}, fmt.Errorf("OnboardTenant: name is required")
	}
	if input.Tier == "" {
		input.Tier = TenantTierStandard
	}

	if violations := validateTenantConfigForTier(input.Tier, input.Config); len(violations) > 0 {
		sort.Strings(violations)
		return Tenant{}, &TenantConfigValidationError{Violations: violations}
	}

	normalizedConfig := normalizeTenantConfig(input.Tier, input.Config)
	rawConfig, err := json.Marshal(normalizedConfig)
	if err != nil {
		return Tenant{}, fmt.Errorf("OnboardTenant: marshal config: %w", err)
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Tenant{}, fmt.Errorf("OnboardTenant: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var existing int
	if err := tx.GetContext(ctx, &existing, `SELECT COUNT(*)::int FROM tenants WHERE tenant_code = $1`, input.TenantCode); err != nil {
		return Tenant{}, fmt.Errorf("OnboardTenant: check tenant_code uniqueness: %w", err)
	}
	if existing > 0 {
		return Tenant{}, fmt.Errorf("OnboardTenant: tenant_code %s already exists", input.TenantCode)
	}

	var tenant Tenant
	if err := tx.GetContext(ctx, &tenant, `
		INSERT INTO tenants (tenant_code, name, status, tier, config)
		VALUES ($1, $2, 'ACTIVE', $3, $4::jsonb)
		RETURNING tenant_id::text AS tenant_id, tenant_code, name, status, tier, config, created_at, updated_at
	`, input.TenantCode, input.Name, string(input.Tier), rawConfig); err != nil {
		return Tenant{}, fmt.Errorf("OnboardTenant: insert tenant: %w", err)
	}

	// Notice: SeedSystemRoles is now handled by the IdentityService independently
	// or decoupled via Events.
	// To prevent direct cyclic dependencies with an `identity` package, we'll
	// publish a TENANT_ONBOARDED event that `identity` listens to, or just
	// instantiate an ephemeral identity service here.
	identitySvc := identity.NewIdentityService(db, slog.Default(), nil) // Needs a publisher, but SeedSystemRoles doesn't use it
	if err := identitySvc.SeedSystemRoles(ctx, tx, tenant.TenantID); err != nil {
		return Tenant{}, fmt.Errorf("OnboardTenant: seed system roles: %w", err)
	}

	if err := publishTenantLifecycleEventTx(ctx, tx, tenant.TenantID, model.EventTenantOnboarded, map[string]interface{}{
		"tenant_id":   tenant.TenantID,
		"tenant_code": tenant.TenantCode,
		"tier":        tenant.Tier,
		"created_by":  input.CreatedBy,
		"occurred_at": time.Now().UTC(),
	}); err != nil {
		return Tenant{}, fmt.Errorf("OnboardTenant: publish TENANT_ONBOARDED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Tenant{}, fmt.Errorf("OnboardTenant: commit: %w", err)
	}

	InvalidateTenantFeatureCache(tenant.TenantID)
	slog.Info("tenant onboarded", "tenant_id", tenant.TenantID, "tenant_code", tenant.TenantCode)
	return tenant, nil
}

// OffboardTenant marks tenant OFFBOARDED when no active cases remain and publishes TENANT_OFFBOARDED.
func OffboardTenant(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	offboardedBy string,
) error {
	if db == nil {
		return fmt.Errorf("OffboardTenant: db is nil")
	}
	tenantID = strings.TrimSpace(tenantID)
	offboardedBy = strings.TrimSpace(offboardedBy)
	if tenantID == "" {
		return fmt.Errorf("OffboardTenant: tenantID is required")
	}
	if offboardedBy == "" {
		offboardedBy = "system"
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("OffboardTenant: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var tenant Tenant
	if err := tx.GetContext(ctx, &tenant, `
		SELECT tenant_id::text AS tenant_id, tenant_code, name, status, tier, config, created_at, updated_at
		FROM tenants
		WHERE tenant_id = $1::uuid
		FOR UPDATE
	`, tenantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("OffboardTenant: %w", ErrTenantNotFound)
		}
		return fmt.Errorf("OffboardTenant: lock tenant: %w", err)
	}

	var activeCaseCount int
	if err := tx.GetContext(ctx, &activeCaseCount, `
		SELECT COUNT(*)::int
		FROM cases
		WHERE tenant_id = $1::uuid
		  AND status NOT IN ('COMPLETED', 'CANCELLED', 'REJECTED', 'CLONED')
	`, tenantID); err != nil {
		return fmt.Errorf("OffboardTenant: count active cases: %w", err)
	}
	if activeCaseCount > 0 {
		refs := make([]string, 0)
		if err := tx.SelectContext(ctx, &refs, `
			SELECT reference_number
			FROM cases
			WHERE tenant_id = $1::uuid
			  AND status NOT IN ('COMPLETED', 'CANCELLED', 'REJECTED', 'CLONED')
			ORDER BY created_at ASC
			LIMIT 20
		`, tenantID); err != nil {
			return fmt.Errorf("OffboardTenant: list active cases: %w", err)
		}
		return fmt.Errorf("OffboardTenant: active cases remain (%d): %s", activeCaseCount, strings.Join(refs, ","))
	}

	cfg, err := parseTenantConfig(tenant.Config)
	if err != nil {
		return fmt.Errorf("OffboardTenant: parse tenant config: %w", err)
	}
	cfg.FeatureFlags = TenantFeatureFlags{}
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("OffboardTenant: marshal disabled config: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE tenants
		SET status = 'OFFBOARDED',
		    config = $1::jsonb,
		    updated_at = now()
		WHERE tenant_id = $2::uuid
	`, rawCfg, tenantID); err != nil {
		return fmt.Errorf("OffboardTenant: update tenant status: %w", err)
	}

	if err := publishTenantLifecycleEventTx(ctx, tx, tenantID, model.EventTenantOffboarded, map[string]interface{}{
		"tenant_id":     tenantID,
		"tenant_code":   tenant.TenantCode,
		"offboarded_by": offboardedBy,
		"occurred_at":   time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("OffboardTenant: publish TENANT_OFFBOARDED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("OffboardTenant: commit: %w", err)
	}

	InvalidateTenantFeatureCache(tenantID)
	slog.Info("tenant offboarded", "tenant_id", tenantID, "offboarded_by", offboardedBy)
	return nil
}

// UpdateTenantConfig updates config and publishes TENANT_CONFIG_UPDATED event.
func UpdateTenantConfig(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	updatedBy string,
	config TenantConfig,
) error {
	if db == nil {
		return fmt.Errorf("UpdateTenantConfig: db is nil")
	}
	tenantID = strings.TrimSpace(tenantID)
	updatedBy = strings.TrimSpace(updatedBy)
	if tenantID == "" {
		return fmt.Errorf("UpdateTenantConfig: tenantID is required")
	}
	if updatedBy == "" {
		updatedBy = "system"
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("UpdateTenantConfig: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var tenant TierRow
	if err := tx.GetContext(ctx, &tenant, `
		SELECT tier, status
		FROM tenants
		WHERE tenant_id = $1::uuid
		FOR UPDATE
	`, tenantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("UpdateTenantConfig: %w", ErrTenantNotFound)
		}
		return fmt.Errorf("UpdateTenantConfig: lock tenant: %w", err)
	}

	if violations := validateTenantConfigForTier(TenantTier(tenant.Tier), config); len(violations) > 0 {
		sort.Strings(violations)
		return &TenantConfigValidationError{Violations: violations}
	}
	cfg := normalizeTenantConfig(TenantTier(tenant.Tier), config)
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("UpdateTenantConfig: marshal config: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE tenants
		SET config = $1::jsonb,
		    updated_at = now()
		WHERE tenant_id = $2::uuid
	`, rawCfg, tenantID); err != nil {
		return fmt.Errorf("UpdateTenantConfig: update tenant config: %w", err)
	}

	if err := publishTenantLifecycleEventTx(ctx, tx, tenantID, model.EventTenantConfigUpdated, map[string]interface{}{
		"tenant_id":   tenantID,
		"updated_by":  updatedBy,
		"occurred_at": time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("UpdateTenantConfig: publish TENANT_CONFIG_UPDATED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("UpdateTenantConfig: commit: %w", err)
	}

	InvalidateTenantFeatureCache(tenantID)
	slog.Info("tenant config updated", "tenant_id", tenantID, "updated_by", updatedBy)
	return nil
}

type TierRow struct {
	Tier   string `db:"tier"`
	Status string `db:"status"`
}

// HandleTenantConfigUpdatedEvent invalidates tenant config cache using event payload.
func HandleTenantConfigUpdatedEvent(ctx context.Context, payload json.RawMessage) error {
	_ = ctx
	tenantID := TenantFromPayload(payload)
	if tenantID == "" {
		return fmt.Errorf("HandleTenantConfigUpdatedEvent: tenant_id missing in payload")
	}
	InvalidateTenantFeatureCache(tenantID)
	return nil
}

func publishTenantLifecycleEventTx(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	eventType model.EventType,
	payload map[string]interface{},
) error {
	if tx == nil {
		return fmt.Errorf("publishTenantLifecycleEventTx: tx is nil")
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	payload["tenant_id"] = tenantID
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("publishTenantLifecycleEventTx: marshal payload: %w", err)
	}
	if err := PublishEvent(ctx, tx, model.Event{
		TenantID:      tenantID,
		EventType:     eventType,
		Payload:       raw,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
		MaxAttempts:   5,
		PartitionKey:  &tenantID,
	}); err != nil {
		return fmt.Errorf("publishTenantLifecycleEventTx: publish event: %w", err)
	}
	return nil
}

func loadTenantAndConfig(ctx context.Context, db *sqlx.DB, tenantID string) (Tenant, TenantConfig, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		if fromCtx, err := TenantFromContext(ctx); err == nil {
			tenantID = fromCtx
		}
	}
	if tenantID == "" {
		return Tenant{}, TenantConfig{}, fmt.Errorf("loadTenantAndConfig: %w", ErrTenantNotFound)
	}

	var tenant Tenant
	if err := db.GetContext(ctx, &tenant, `
		SELECT tenant_id::text AS tenant_id, tenant_code, name, status, tier, config, created_at, updated_at
		FROM tenants
		WHERE tenant_id = $1::uuid
	`, tenantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Tenant{}, TenantConfig{}, fmt.Errorf("loadTenantAndConfig: %w", ErrTenantNotFound)
		}
		return Tenant{}, TenantConfig{}, fmt.Errorf("loadTenantAndConfig: query tenant: %w", err)
	}
	cfg, err := parseTenantConfig(tenant.Config)
	if err != nil {
		return Tenant{}, TenantConfig{}, fmt.Errorf("loadTenantAndConfig: parse config: %w", err)
	}
	cfg = normalizeTenantConfig(tenant.Tier, cfg)
	return tenant, cfg, nil
}

func parseTenantConfig(raw json.RawMessage) (TenantConfig, error) {
	if len(raw) == 0 {
		return TenantConfig{}, nil
	}
	cfg := TenantConfig{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return TenantConfig{}, fmt.Errorf("parseTenantConfig: %w", err)
	}
	return cfg, nil
}

func assertTenantStatus(status TenantStatus) error {
	switch status {
	case TenantStatusActive:
		return nil
	case TenantStatusSuspended:
		return ErrTenantSuspended
	case TenantStatusOffboarded:
		return ErrTenantOffboarded
	default:
		return fmt.Errorf("unknown tenant status %s", status)
	}
}

func validateTenantConfigForTier(tier TenantTier, cfg TenantConfig) []string {
	maxCfg := tierMaxConfig(tier)
	violations := make([]string, 0)

	if cfg.MaxActiveCases > 0 && maxCfg.MaxActiveCases > 0 && cfg.MaxActiveCases > maxCfg.MaxActiveCases {
		violations = append(violations, fmt.Sprintf("max_active_cases exceeds tier limit %d", maxCfg.MaxActiveCases))
	}
	if cfg.MaxConcurrentTasks > 0 && maxCfg.MaxConcurrentTasks > 0 && cfg.MaxConcurrentTasks > maxCfg.MaxConcurrentTasks {
		violations = append(violations, fmt.Sprintf("max_concurrent_tasks exceeds tier limit %d", maxCfg.MaxConcurrentTasks))
	}
	if cfg.MaxCasesPerMinute > 0 && maxCfg.MaxCasesPerMinute > 0 && cfg.MaxCasesPerMinute > maxCfg.MaxCasesPerMinute {
		violations = append(violations, fmt.Sprintf("max_cases_per_minute exceeds tier limit %d", maxCfg.MaxCasesPerMinute))
	}
	return violations
}

func tierMaxConfig(tier TenantTier) TenantConfig {
	switch tier {
	case TenantTierPremium:
		return TenantConfig{MaxActiveCases: 250000, MaxConcurrentTasks: 75000, MaxCasesPerMinute: 15000}
	case TenantTierEnterprise:
		return TenantConfig{MaxActiveCases: 1000000, MaxConcurrentTasks: 250000, MaxCasesPerMinute: 50000}
	default:
		return TenantConfig{MaxActiveCases: 100000, MaxConcurrentTasks: 25000, MaxCasesPerMinute: 5000}
	}
}

func normalizeTenantConfig(tier TenantTier, cfg TenantConfig) TenantConfig {
	defaults := tierMaxConfig(tier)
	if cfg.MaxActiveCases <= 0 {
		cfg.MaxActiveCases = defaults.MaxActiveCases
	}
	if cfg.MaxConcurrentTasks <= 0 {
		cfg.MaxConcurrentTasks = defaults.MaxConcurrentTasks
	}
	if cfg.MaxCasesPerMinute <= 0 {
		cfg.MaxCasesPerMinute = defaults.MaxCasesPerMinute
	}
	if cfg.SLAMultiplier <= 0 {
		cfg.SLAMultiplier = 1
	}
	return cfg
}

// Lightweight tenant-scoped query helpers used by isolation tests.
func ListTenantCaseIDs(ctx context.Context, db *sqlx.DB) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("ListTenantCaseIDs: db is nil")
	}
	q, args, err := AssertTenantScope(ctx, `SELECT id::text FROM cases ORDER BY created_at ASC`, nil)
	if err != nil {
		return nil, fmt.Errorf("ListTenantCaseIDs: %w", err)
	}
	rows := make([]string, 0)
	if err := db.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, fmt.Errorf("ListTenantCaseIDs: query: %w", err)
	}
	return rows, nil
}

func ListTenantTaskIDs(ctx context.Context, db *sqlx.DB) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("ListTenantTaskIDs: db is nil")
	}
	q, args, err := AssertTenantScope(ctx, `SELECT id::text FROM tasks ORDER BY created_at ASC`, nil)
	if err != nil {
		return nil, fmt.Errorf("ListTenantTaskIDs: %w", err)
	}
	rows := make([]string, 0)
	if err := db.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, fmt.Errorf("ListTenantTaskIDs: query: %w", err)
	}
	return rows, nil
}

func ListTenantEventIDs(ctx context.Context, db *sqlx.DB) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("ListTenantEventIDs: db is nil")
	}
	q, args, err := AssertTenantScope(ctx, `SELECT id::text FROM events_outbox ORDER BY created_at ASC`, nil)
	if err != nil {
		return nil, fmt.Errorf("ListTenantEventIDs: %w", err)
	}
	rows := make([]string, 0)
	if err := db.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, fmt.Errorf("ListTenantEventIDs: query: %w", err)
	}
	return rows, nil
}

func ListTenantNotificationIDs(ctx context.Context, db *sqlx.DB) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("ListTenantNotificationIDs: db is nil")
	}
	q, args, err := AssertTenantScope(ctx, `SELECT id::text FROM notification_queue ORDER BY created_at ASC`, nil)
	if err != nil {
		return nil, fmt.Errorf("ListTenantNotificationIDs: %w", err)
	}
	rows := make([]string, 0)
	if err := db.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, fmt.Errorf("ListTenantNotificationIDs: query: %w", err)
	}
	return rows, nil
}

func ListTenantDLQIDs(ctx context.Context, db *sqlx.DB) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("ListTenantDLQIDs: db is nil")
	}
	q, args, err := AssertTenantScope(ctx, `SELECT dlq_id::text FROM task_dlq ORDER BY moved_at ASC`, nil)
	if err != nil {
		return nil, fmt.Errorf("ListTenantDLQIDs: %w", err)
	}
	rows := make([]string, 0)
	if err := db.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, fmt.Errorf("ListTenantDLQIDs: query: %w", err)
	}
	return rows, nil
}
