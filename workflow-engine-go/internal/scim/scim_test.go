package scim

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"workflow-engine/internal/multitenancy"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSCIMSQLXMock(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	cleanup := func() {
		_ = db.Close()
	}
	return db, mock, cleanup
}

type captureStringArg struct {
	value *string
}

func (a captureStringArg) Match(v driver.Value) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	if a.value != nil {
		*a.value = s
	}
	return true
}

func withClaimsContext(tenantID, tokenID string, scopes ...string) context.Context {
	ctx := context.Background()
	ctx = multitenancy.WithTenant(ctx, tenantID)
	ctx = WithSCIMClaims(ctx, SCIMTokenClaims{TenantID: tenantID, TokenID: tokenID, Scopes: scopes})
	return ctx
}

func TestCreateSCIMToken_RawTokenReturnedOnceAndValidateSucceeds(t *testing.T) {
	db, mock, cleanup := newSCIMSQLXMock(t)
	defer cleanup()
	SetSCIMTokenUsageTracker(&SCIMTokenUsageTracker{})
	defer SetSCIMTokenUsageTracker(nil)

	tenantID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	capturedHash := ""

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)INSERT INTO scim_tokens`).
		WithArgs(
			tenantID,
			captureStringArg{value: &capturedHash},
			"Okta provisioning token",
			"{\"users:read\",\"users:write\"}",
			sqlmock.AnyArg(),
			"bootstrap",
		).
		WillReturnRows(sqlmock.NewRows([]string{"token_id"}).AddRow("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
	mock.ExpectExec(`(?s)INSERT INTO events_outbox`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	raw, err := CreateSCIMToken(context.Background(), db, tenantID, "Okta provisioning token", []string{"users:read", "users:write"}, nil, "bootstrap")
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	assert.NotEqual(t, raw, capturedHash)
	assert.Len(t, capturedHash, 64)

	mock.ExpectQuery(`(?s)SELECT token_id::text AS token_id`).
		WithArgs(tokenHash(raw)).
		WillReturnRows(sqlmock.NewRows([]string{"token_id", "tenant_id", "scopes_json", "status", "expires_at"}).
			AddRow("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", tenantID, `["users:read","users:write"]`, "ACTIVE", nil))

	claims, err := ValidateSCIMToken(context.Background(), db, raw)
	require.NoError(t, err)
	assert.Equal(t, tenantID, claims.TenantID)
	assert.Equal(t, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", claims.TokenID)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestValidateSCIMToken_ExpiredAndNotFoundSameError(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(sqlmock.Sqlmock)
	}{
		{
			name: "expired token",
			setupMock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(`(?s)SELECT token_id::text AS token_id`).
					WithArgs(tokenHash("raw-token")).
					WillReturnRows(sqlmock.NewRows([]string{"token_id", "tenant_id", "scopes_json", "status", "expires_at"}).
						AddRow("token-1", "tenant-1", `["users:read"]`, "ACTIVE", time.Now().UTC().Add(-time.Minute)))
			},
		},
		{
			name: "token not found",
			setupMock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(`(?s)SELECT token_id::text AS token_id`).
					WithArgs(tokenHash("raw-token")).
					WillReturnError(sql.ErrNoRows)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, cleanup := newSCIMSQLXMock(t)
			defer cleanup()
			SetSCIMTokenUsageTracker(&SCIMTokenUsageTracker{})
			defer SetSCIMTokenUsageTracker(nil)
			tt.setupMock(mock)

			_, err := ValidateSCIMToken(context.Background(), db, "raw-token")
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrSCIMTokenInvalid))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestGetUser_IfNoneMatchReturns304(t *testing.T) {
	db, mock, cleanup := newSCIMSQLXMock(t)
	defer cleanup()

	tenantID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	userID := "11111111-1111-1111-1111-111111111111"

	mock.ExpectQuery(`(?s)SELECT\s+u.user_id::text AS user_id`).
		WithArgs(tenantID, userID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "tenant_id", "username", "email", "display_name", "status", "auth_provider", "external_id", "locale", "timezone", "created_at", "updated_at", "version"}).
			AddRow(userID, tenantID, "jsmith", "jsmith@example.com", "John Smith", "ACTIVE", "OIDC", nil, "en-US", "UTC", time.Now().UTC(), time.Now().UTC(), 5))
	mock.ExpectQuery(`(?s)SELECT r.role_code`).WithArgs(tenantID, userID).WillReturnRows(sqlmock.NewRows([]string{"role_code"}))
	mock.ExpectQuery(`(?s)SELECT team_id::text`).WithArgs(tenantID, userID).WillReturnError(sql.ErrNoRows)

	h := newSCIMHandler(db, nil)
	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users/"+userID, nil)
	req = req.WithContext(withClaimsContext(tenantID, "token-1", "users:read"))
	req.Header.Set("If-None-Match", `W/"5"`)
	w := httptest.NewRecorder()

	h.GetUser(w, req)

	assert.Equal(t, http.StatusNotModified, w.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReplaceUser_IfMatchMismatchReturns412(t *testing.T) {
	db, mock, cleanup := newSCIMSQLXMock(t)
	defer cleanup()

	tenantID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	userID := "11111111-1111-1111-1111-111111111111"

	mock.ExpectQuery(`(?s)SELECT version\s+FROM users`).WithArgs(tenantID, userID).WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(7))

	h := newSCIMHandler(db, nil)
	payload := `{"schemas":["` + SchemaCoreUser + `"],"userName":"jsmith","emails":[{"value":"jsmith@example.com","primary":true}]}`
	req := httptest.NewRequest(http.MethodPut, "/scim/v2/Users/"+userID, strings.NewReader(payload))
	req = req.WithContext(withClaimsContext(tenantID, "token-1", "users:write"))
	req.Header.Set("If-Match", `W/"6"`)
	w := httptest.NewRecorder()

	h.ReplaceUser(w, req)

	assert.Equal(t, http.StatusPreconditionFailed, w.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestParseSCIMFilter_TooManyNodes(t *testing.T) {
	parts := make([]string, 0, 51)
	for i := 0; i < 51; i++ {
		parts = append(parts, `userName eq "user`+strconv.Itoa(i)+`"`)
	}
	expr := strings.Join(parts, " and ")
	_, err := ParseSCIMFilter(expr)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidSCIMFilter))
	assert.Contains(t, strings.ToLower(err.Error()), "toomany")
}

func TestParseSCIMFilter_UnknownAttribute(t *testing.T) {
	filter, err := ParseSCIMFilter(`unknownAttr eq "x"`)
	require.NoError(t, err)
	_, _, err = filter.ToSQL(SCIMResourceTypeUser)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidSCIMFilter))
}

func TestParseSCIMFilter_ComparisonOperators(t *testing.T) {
	tests := []struct {
		name         string
		expression   string
		wantRegex    string
		wantArg      string
	}{
		{name: "eq userName", expression: `userName eq "jsmith"`, wantRegex: `LOWER\(u.username\) = LOWER\(\$1\)`, wantArg: "jsmith"},
		{name: "co displayName", expression: `displayName co "smith"`, wantRegex: `u.display_name ILIKE \$1`, wantArg: "%smith%"},
		{name: "and compound", expression: `userName eq "jsmith" and active eq true`, wantRegex: `AND`, wantArg: "jsmith"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseSCIMFilter(tt.expression)
			require.NoError(t, err)
			clause, args, err := filter.ToSQL(SCIMResourceTypeUser)
			require.NoError(t, err)
			assert.Regexp(t, regexp.MustCompile(tt.wantRegex), clause)
			if tt.wantArg != "" {
				require.NotEmpty(t, args)
				assert.Equal(t, tt.wantArg, args[0])
			}
		})
	}
}

func TestBulkOperation_FailOnErrorsAbort(t *testing.T) {
	db, mock, cleanup := newSCIMSQLXMock(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT status, metadata\s+FROM scim_tokens`).
		WillReturnRows(sqlmock.NewRows([]string{"status", "metadata"}).AddRow("ACTIVE", []byte(`{}`)))
	mock.ExpectQuery(`(?s)INSERT INTO scim_token_rate_limit_counters`).
		WillReturnRows(sqlmock.NewRows([]string{"request_count"}).AddRow(2))
	mock.ExpectCommit()

	h := newSCIMHandler(db, nil)
	bulk := SCIMBulkRequest{
		Schemas:      []string{SchemaBulkRequest},
		FailOnErrors: 1,
		Operations: []SCIMBulkOperation{
			{Method: "POST", Path: "/Unknown", Data: json.RawMessage(`{}`)},
			{Method: "POST", Path: "/Users", Data: json.RawMessage(`{"userName":"a"}`)},
		},
	}
	body, _ := json.Marshal(bulk)
	req := httptest.NewRequest(http.MethodPost, "/scim/v2/Bulk", bytes.NewReader(body))
	req = req.WithContext(withClaimsContext("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "users:write", "groups:write"))
	w := httptest.NewRecorder()

	h.BulkOperation(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp SCIMBulkResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Operations, 1)
	assert.Equal(t, "400", resp.Operations[0].Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBulkOperation_TooManyOperations(t *testing.T) {
	h := newSCIMHandler(nil, nil)
	ops := make([]SCIMBulkOperation, 1001)
	for i := range ops {
		ops[i] = SCIMBulkOperation{Method: "POST", Path: "/Users", Data: json.RawMessage(`{}`)}
	}
	bulk := SCIMBulkRequest{Schemas: []string{SchemaBulkRequest}, Operations: ops}
	body, _ := json.Marshal(bulk)
	req := httptest.NewRequest(http.MethodPost, "/scim/v2/Bulk", bytes.NewReader(body))
	req = req.WithContext(withClaimsContext("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "token", "users:write", "groups:write"))
	w := httptest.NewRecorder()

	h.BulkOperation(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tooMany")
}

func TestBulkOperation_BulkIDReferenceResolution(t *testing.T) {
	bulkIDs := map[string]string{
		"bulkId:user-1": "11111111-1111-1111-1111-111111111111",
	}
	path := resolveBulkPath("/Users/bulkId:user-1", bulkIDs)
	assert.Equal(t, "/Users/11111111-1111-1111-1111-111111111111", path)

	data := resolveBulkData(json.RawMessage(`{"members":[{"value":"bulkId:user-1"}]}`), bulkIDs)
	assert.Contains(t, string(data), "11111111-1111-1111-1111-111111111111")
}

func TestEnforceSCIMRateLimit_AtLimitExceeded(t *testing.T) {
	db, mock, cleanup := newSCIMSQLXMock(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT status, metadata\s+FROM scim_tokens`).
		WithArgs("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb").
		WillReturnRows(sqlmock.NewRows([]string{"status", "metadata"}).AddRow("ACTIVE", []byte(`{}`)))
	mock.ExpectQuery(`(?s)INSERT INTO scim_token_rate_limit_counters`).
		WithArgs("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", sqlmock.AnyArg(), 1).
		WillReturnRows(sqlmock.NewRows([]string{"request_count"}).AddRow(301))
	mock.ExpectRollback()

	err := EnforceSCIMRateLimit(context.Background(), db, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSCIMRateLimitExceeded))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSCIMMiddleware_MissingAuthorization(t *testing.T) {
	h := SCIMMiddleware(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, scimContentType, w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), SchemaError)
}

func TestSCIMScopeMiddleware_InsufficientScope(t *testing.T) {
	nextCalled := false
	h := SCIMScopeMiddleware("groups:write", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Groups", nil)
	req = req.WithContext(withClaimsContext("tenant", "token", "users:read"))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, nextCalled)
	assert.Contains(t, w.Body.String(), SchemaError)
}

func TestGetSchemas_ReturnsAllDocuments(t *testing.T) {
	h := newSCIMHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Schemas", nil)
	w := httptest.NewRecorder()

	h.GetSchemas(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp SCIMListResponse[SCIMSchemaDocument]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 3, resp.TotalResults)
	ids := make(map[string]struct{})
	for _, doc := range resp.Resources {
		ids[doc.ID] = struct{}{}
	}
	_, hasUser := ids[SchemaCoreUser]
	_, hasGroup := ids[SchemaCoreGroup]
	_, hasExt := ids[SchemaWorkflowUserExtension]
	assert.True(t, hasUser && hasGroup && hasExt)
}

func TestGetServiceProviderConfig_Values(t *testing.T) {
	h := newSCIMHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/scim/v2/ServiceProviderConfig", nil)
	w := httptest.NewRecorder()

	h.GetServiceProviderConfig(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var cfg SCIMServiceProviderConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cfg))
	assert.True(t, cfg.Bulk.Supported)
	assert.Equal(t, 1000, cfg.Bulk.MaxOperations)
	assert.True(t, cfg.ETag.Supported)
}

func TestReplaceGroup_ReconcileMembersSingleTransactionBatchedWrites(t *testing.T) {
	db, mock, cleanup := newSCIMSQLXMock(t)
	defer cleanup()

	tenantID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	groupID := "11111111-1111-1111-1111-111111111111"

	mock.ExpectQuery(`(?s)SELECT version\s+FROM teams`).
		WithArgs(tenantID, groupID).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(2))

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE teams\s+SET display_name =`).
		WithArgs("Operations Team", sqlmock.AnyArg(), tenantID, groupID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT user_id::text\s+FROM team_members`).
		WithArgs(tenantID, groupID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow("u1").AddRow("u2"))
	mock.ExpectQuery(`(?s)SELECT user_id::text\s+FROM users`).
		WithArgs(tenantID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow("u3").AddRow("u4"))
	mock.ExpectExec(`(?s)INSERT INTO team_members`).
		WithArgs(groupID, tenantID, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`(?s)DELETE FROM team_members`).
		WithArgs(tenantID, groupID, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(`(?s)SELECT\s+t.team_id::text AS team_id`).
		WithArgs(tenantID, groupID).
		WillReturnRows(sqlmock.NewRows([]string{"team_id", "tenant_id", "external_id", "display_name", "created_at", "updated_at", "version", "member_id", "member_name"}).
			AddRow(groupID, tenantID, nil, "Operations Team", time.Now().UTC(), time.Now().UTC(), 3, "u2", "User 2").
			AddRow(groupID, tenantID, nil, "Operations Team", time.Now().UTC(), time.Now().UTC(), 3, "u3", "User 3").
			AddRow(groupID, tenantID, nil, "Operations Team", time.Now().UTC(), time.Now().UTC(), 3, "u4", "User 4"))

	h := newSCIMHandler(db, nil)
	payload := `{"schemas":["` + SchemaCoreGroup + `"],"displayName":"Operations Team","members":[{"value":"u2"},{"value":"u3"},{"value":"u4"}]}`
	req := httptest.NewRequest(http.MethodPut, "/scim/v2/Groups/"+groupID, strings.NewReader(payload))
	req = req.WithContext(withClaimsContext(tenantID, "token-1", "groups:write"))
	req.Header.Set("If-Match", `W/"2"`)
	w := httptest.NewRecorder()

	h.ReplaceGroup(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
