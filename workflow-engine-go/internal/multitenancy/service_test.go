package multitenancy

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSQLXMock(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	sqlxDB := sqlx.NewDb(db, "sqlmock")
	cleanup := func() {
		_ = db.Close()
	}
	return sqlxDB, mock, cleanup
}

func tenantConfigJSON(maxActive, maxTasks, maxPerMinute int) driver.Value {
	payload := map[string]interface{}{
		"max_active_cases":     maxActive,
		"max_concurrent_tasks": maxTasks,
		"max_cases_per_minute": maxPerMinute,
		"feature_flags": map[string]interface{}{
			"compensation_enabled":    true,
			"dlq_requeue_enabled":     true,
			"notification_enabled":    true,
			"sub_case_enabled":        true,
			"sla_enforcement_enabled": true,
		},
	}
	raw, _ := json.Marshal(payload)
	return raw
}

func TestTenantMiddleware_SuspendedTenant(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectQuery(`(?s)FROM tenants`).WithArgs("TENANT_SUSP").WillReturnRows(
		sqlmock.NewRows([]string{"tenant_id", "tenant_code", "name", "status", "tier", "config", "created_at", "updated_at"}).
			AddRow("11111111-1111-1111-1111-111111111111", "TENANT_SUSP", "Suspended", "SUSPENDED", "STANDARD", []byte(`{}`), time.Now().UTC(), time.Now().UTC()),
	)

	h := TenantMiddleware(db, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/cases", nil)
	req.Header.Set("X-Tenant-Code", "TENANT_SUSP")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.Contains(t, rr.Body.String(), ErrTenantSuspended.Error())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantMiddleware_OffboardedTenant(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectQuery(`(?s)FROM tenants`).WithArgs("TENANT_OFF").WillReturnRows(
		sqlmock.NewRows([]string{"tenant_id", "tenant_code", "name", "status", "tier", "config", "created_at", "updated_at"}).
			AddRow("22222222-2222-2222-2222-222222222222", "TENANT_OFF", "Offboarded", "OFFBOARDED", "STANDARD", []byte(`{}`), time.Now().UTC(), time.Now().UTC()),
	)

	h := TenantMiddleware(db, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/cases", nil)
	req.Header.Set("X-Tenant-Code", "TENANT_OFF")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.Contains(t, rr.Body.String(), ErrTenantOffboarded.Error())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnforceTenantCaseLimits_AtLimitRejectedAndBelowAllowed(t *testing.T) {
	tests := []struct {
		name        string
		activeCount int
		rateCount   int
		wantErr     bool
		errContains string
	}{
		{
			name:        "exactly at active limit rejected",
			activeCount: 5,
			rateCount:   1,
			wantErr:     true,
			errContains: ErrTenantCapacityExceeded.Error(),
		},
		{
			name:        "one below active limit allowed",
			activeCount: 4,
			rateCount:   1,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, cleanup := newSQLXMock(t)
			defer cleanup()

			mock.ExpectQuery(`(?s)FROM tenants`).WithArgs("33333333-3333-3333-3333-333333333333").WillReturnRows(
				sqlmock.NewRows([]string{"tenant_id", "tenant_code", "name", "status", "tier", "config", "created_at", "updated_at"}).
					AddRow("33333333-3333-3333-3333-333333333333", "TENANT_A", "Tenant A", "ACTIVE", "STANDARD", tenantConfigJSON(5, 10, 10), time.Now().UTC(), time.Now().UTC()),
			)
			mock.ExpectQuery(`(?s)FROM cases`).WithArgs("33333333-3333-3333-3333-333333333333").WillReturnRows(
				sqlmock.NewRows([]string{"count"}).AddRow(tt.activeCount),
			)
			if tt.wantErr {
				err := EnforceTenantCaseLimits(context.Background(), db, "33333333-3333-3333-3333-333333333333")
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				mock.ExpectBegin()
				mock.ExpectQuery(`(?s)INSERT INTO tenant_rate_limit_counters`).
					WithArgs("33333333-3333-3333-3333-333333333333", sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"case_count"}).AddRow(tt.rateCount))
				mock.ExpectCommit()

				err := EnforceTenantCaseLimits(context.Background(), db, "33333333-3333-3333-3333-333333333333")
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestEnforceTenantCaseLimits_ExpiredWindowDoesNotCount(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectQuery(`(?s)FROM tenants`).WithArgs("44444444-4444-4444-4444-444444444444").WillReturnRows(
		sqlmock.NewRows([]string{"tenant_id", "tenant_code", "name", "status", "tier", "config", "created_at", "updated_at"}).
			AddRow("44444444-4444-4444-4444-444444444444", "TENANT_B", "Tenant B", "ACTIVE", "STANDARD", tenantConfigJSON(10, 10, 2), time.Now().UTC(), time.Now().UTC()),
	)
	mock.ExpectQuery(`(?s)FROM cases`).WithArgs("44444444-4444-4444-4444-444444444444").WillReturnRows(
		sqlmock.NewRows([]string{"count"}).AddRow(1),
	)
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)INSERT INTO tenant_rate_limit_counters`).
		WithArgs("44444444-4444-4444-4444-444444444444", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"case_count"}).AddRow(1))
	mock.ExpectCommit()

	err := EnforceTenantCaseLimits(context.Background(), db, "44444444-4444-4444-4444-444444444444")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantFeatureEnabled_CacheHitNoDBCall(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	InvalidateAllTenantFeatureCache()
	SetTenantFeatureCacheTTL(1 * time.Minute)

	mock.ExpectQuery(`(?s)FROM tenants`).WithArgs("55555555-5555-5555-5555-555555555555").WillReturnRows(
		sqlmock.NewRows([]string{"tenant_id", "tenant_code", "name", "status", "tier", "config", "created_at", "updated_at"}).
			AddRow("55555555-5555-5555-5555-555555555555", "TENANT_C", "Tenant C", "ACTIVE", "STANDARD", tenantConfigJSON(10, 10, 10), time.Now().UTC(), time.Now().UTC()),
	)

	enabled, err := TenantFeatureEnabled(context.Background(), db, "55555555-5555-5555-5555-555555555555", TenantFeatureNotificationEnabled)
	require.NoError(t, err)
	assert.True(t, enabled)

	enabled, err = TenantFeatureEnabled(context.Background(), db, "55555555-5555-5555-5555-555555555555", TenantFeatureNotificationEnabled)
	require.NoError(t, err)
	assert.True(t, enabled)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantFeatureEnabled_CacheMissAfterTTLExpiry(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	InvalidateAllTenantFeatureCache()
	SetTenantFeatureCacheTTL(10 * time.Millisecond)

	mock.ExpectQuery(`(?s)FROM tenants`).WithArgs("66666666-6666-6666-6666-666666666666").WillReturnRows(
		sqlmock.NewRows([]string{"tenant_id", "tenant_code", "name", "status", "tier", "config", "created_at", "updated_at"}).
			AddRow("66666666-6666-6666-6666-666666666666", "TENANT_D", "Tenant D", "ACTIVE", "STANDARD", tenantConfigJSON(10, 10, 10), time.Now().UTC(), time.Now().UTC()),
	)
	mock.ExpectQuery(`(?s)FROM tenants`).WithArgs("66666666-6666-6666-6666-666666666666").WillReturnRows(
		sqlmock.NewRows([]string{"tenant_id", "tenant_code", "name", "status", "tier", "config", "created_at", "updated_at"}).
			AddRow("66666666-6666-6666-6666-666666666666", "TENANT_D", "Tenant D", "ACTIVE", "STANDARD", tenantConfigJSON(10, 10, 10), time.Now().UTC(), time.Now().UTC()),
	)

	_, err := TenantFeatureEnabled(context.Background(), db, "66666666-6666-6666-6666-666666666666", TenantFeatureSubCaseEnabled)
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)
	_, err = TenantFeatureEnabled(context.Background(), db, "66666666-6666-6666-6666-666666666666", TenantFeatureSubCaseEnabled)
	require.NoError(t, err)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOnboardTenant_InvalidCodeRejectedBeforeInsert(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	_, err := OnboardTenant(context.Background(), db, OnboardTenantInput{
		TenantCode: "bad-code",
		Name:       "Tenant X",
		Tier:       TenantTierStandard,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant_code")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOnboardTenant_ConfigExceedsTierMaximumRejected(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	_, err := OnboardTenant(context.Background(), db, OnboardTenantInput{
		TenantCode: "TENANT_TOO_BIG",
		Name:       "Tenant Big",
		Tier:       TenantTierStandard,
		Config: TenantConfig{
			MaxActiveCases: 1000000,
		},
	})
	require.Error(t, err)
	var validationErr *TenantConfigValidationError
	assert.True(t, errors.As(err, &validationErr))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOffboardTenant_ActiveCasesRemainingRejected(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM tenants`).WithArgs("77777777-7777-7777-7777-777777777777").WillReturnRows(
		sqlmock.NewRows([]string{"tenant_id", "tenant_code", "name", "status", "tier", "config", "created_at", "updated_at"}).
			AddRow("77777777-7777-7777-7777-777777777777", "TENANT_E", "Tenant E", "ACTIVE", "STANDARD", tenantConfigJSON(10, 10, 10), time.Now().UTC(), time.Now().UTC()),
	)
	mock.ExpectQuery(`(?s)FROM cases`).WithArgs("77777777-7777-7777-7777-777777777777").WillReturnRows(
		sqlmock.NewRows([]string{"count"}).AddRow(2),
	)
	mock.ExpectQuery(`(?s)SELECT reference_number`).WithArgs("77777777-7777-7777-7777-777777777777").WillReturnRows(
		sqlmock.NewRows([]string{"reference_number"}).AddRow("LOAN-2026-00001").AddRow("LOAN-2026-00002"),
	)
	mock.ExpectRollback()

	err := OffboardTenant(context.Background(), db, "77777777-7777-7777-7777-777777777777", "ops")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOAN-2026-00001")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAssertTenantScope_NoTenantInContextReturnsError(t *testing.T) {
	scoped, args, err := AssertTenantScope(context.Background(), "SELECT id FROM cases", nil)
	require.Error(t, err)
	assert.Empty(t, scoped)
	assert.Nil(t, args)
}

func TestIsCaseTypeVisibleToTenant_GlobalAndTenantSpecific(t *testing.T) {
	tests := []struct {
		name    string
		visible bool
	}{
		{name: "global type visible", visible: true},
		{name: "tenant specific type invisible to other tenant", visible: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, cleanup := newSQLXMock(t)
			defer cleanup()

			mock.ExpectQuery(`(?s)SELECT EXISTS.*FROM case_types WHERE code = \$1 AND status = 'ACTIVE' AND.*tenant_id IS NULL OR tenant_id = \$2::uuid`).
				WithArgs("HOME_LOAN", "88888888-8888-8888-8888-888888888888").
				WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(tt.visible))

			visible, err := IsCaseTypeVisibleToTenant(context.Background(), db, "home_loan", "88888888-8888-8888-8888-888888888888")
			require.NoError(t, err)
			assert.Equal(t, tt.visible, visible)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
