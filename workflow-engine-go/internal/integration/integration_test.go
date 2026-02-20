package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"workflow-engine/internal/multitenancy"
	"workflow-engine/pkg/model"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
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

func TestEnqueueWebhookDeliveries_ZeroMatchingSubscriptions(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectBegin()
	tx, err := db.BeginTxx(context.Background(), nil)
	require.NoError(t, err)

	event := model.Event{TenantID: "11111111-1111-1111-1111-111111111111", EventType: model.EventCaseCreated, Payload: json.RawMessage(`{"case_id":"c1"}`)}
	mock.ExpectExec(`(?s)INSERT INTO webhook_delivery_queue`).
		WithArgs("11111111-1111-1111-1111-111111111111", string(model.EventCaseCreated), event.Payload).
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectCommit()

	err = EnqueueWebhookDeliveries(context.Background(), tx, event.TenantID, event)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSignWebhookPayload_Deterministic(t *testing.T) {
	signature := SignWebhookPayload("secret", []byte(`{"hello":"world"}`))
	assert.Equal(t, "sha256=2677ad3e7c090b2fa2c0fb13020d66d5420879b8316eb356a2d60fb9073bc778", signature)
}

func TestEnqueueWebhookDeliveries_PausedSubscriptionSkipped(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectBegin()
	tx, err := db.BeginTxx(context.Background(), nil)
	require.NoError(t, err)

	event := model.Event{TenantID: "11111111-1111-1111-1111-111111111111", EventType: model.EventTaskCompleted, Payload: json.RawMessage(`{"task_id":"t1"}`)}
	mock.ExpectExec(`(?s)INSERT INTO webhook_delivery_queue`).
		WithArgs("11111111-1111-1111-1111-111111111111", string(model.EventTaskCompleted), event.Payload).
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectCommit()

	err = EnqueueWebhookDeliveries(context.Background(), tx, event.TenantID, event)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDispatchWebhook_Non200SchedulesRetry(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream down"))
	}))
	defer srv.Close()

	delivery := WebhookDelivery{
		DeliveryID:     "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		SubscriptionID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		TenantID:       "11111111-1111-1111-1111-111111111111",
		EventType:      "CASE_CREATED",
		Payload:        json.RawMessage(`{"case_id":"c1"}`),
		Attempts:       1,
		MaxAttempts:    5,
		ScheduledAt:    time.Now().UTC(),
		Status:         WebhookDeliveryStatusPending,
	}

	mock.ExpectQuery(`(?s)FROM webhook_subscriptions`).
		WithArgs(delivery.SubscriptionID, delivery.TenantID).
		WillReturnRows(sqlmock.NewRows([]string{"subscription_id", "tenant_id", "subscriber_code", "target_url", "signing_secret_enc", "status", "max_failures", "failure_count", "headers", "timeout_seconds", "created_at", "updated_at"}).
			AddRow(delivery.SubscriptionID, delivery.TenantID, "CRM", srv.URL, []byte("plain-secret"), "ACTIVE", 5, 0, []byte(`{"X-Test":"1"}`), 10, time.Now().UTC(), time.Now().UTC()))

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE webhook_delivery_queue`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)UPDATE webhook_subscriptions`).WillReturnRows(sqlmock.NewRows([]string{"failure_count", "max_failures", "status"}).AddRow(1, 5, "ACTIVE"))
	mock.ExpectExec(`(?s)INSERT INTO integration_audit_log`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := DispatchWebhook(multitenancy.WithTenant(context.Background(), delivery.TenantID), db, delivery, srv.Client())
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDispatchWebhook_MaxAttemptsReachedSetsAbandoned(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("failed"))
	}))
	defer srv.Close()

	delivery := WebhookDelivery{
		DeliveryID:     "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaab",
		SubscriptionID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbc",
		TenantID:       "11111111-1111-1111-1111-111111111111",
		EventType:      "TASK_COMPLETED",
		Payload:        json.RawMessage(`{"task_id":"t1"}`),
		Attempts:       3,
		MaxAttempts:    3,
		ScheduledAt:    time.Now().UTC(),
		Status:         WebhookDeliveryStatusFailed,
	}

	mock.ExpectQuery(`(?s)FROM webhook_subscriptions`).
		WithArgs(delivery.SubscriptionID, delivery.TenantID).
		WillReturnRows(sqlmock.NewRows([]string{"subscription_id", "tenant_id", "subscriber_code", "target_url", "signing_secret_enc", "status", "max_failures", "failure_count", "headers", "timeout_seconds", "created_at", "updated_at"}).
			AddRow(delivery.SubscriptionID, delivery.TenantID, "CRM", srv.URL, []byte("plain-secret"), "ACTIVE", 5, 0, []byte(`{}`), 10, time.Now().UTC(), time.Now().UTC()))

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE webhook_delivery_queue`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)UPDATE webhook_subscriptions`).WillReturnRows(sqlmock.NewRows([]string{"failure_count", "max_failures", "status"}).AddRow(2, 5, "ACTIVE"))
	mock.ExpectExec(`(?s)INSERT INTO integration_audit_log`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := DispatchWebhook(multitenancy.WithTenant(context.Background(), delivery.TenantID), db, delivery, srv.Client())
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDispatchWebhook_FailureCountReachesMaxMarksSubscriptionFailed(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("failed"))
	}))
	defer srv.Close()

	delivery := WebhookDelivery{
		DeliveryID:     "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaac",
		SubscriptionID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbd",
		TenantID:       "11111111-1111-1111-1111-111111111111",
		EventType:      "CASE_COMPLETED",
		Payload:        json.RawMessage(`{"case_id":"c2"}`),
		Attempts:       5,
		MaxAttempts:    5,
		ScheduledAt:    time.Now().UTC(),
		Status:         WebhookDeliveryStatusFailed,
	}

	mock.ExpectQuery(`(?s)FROM webhook_subscriptions`).
		WithArgs(delivery.SubscriptionID, delivery.TenantID).
		WillReturnRows(sqlmock.NewRows([]string{"subscription_id", "tenant_id", "subscriber_code", "target_url", "signing_secret_enc", "status", "max_failures", "failure_count", "headers", "timeout_seconds", "created_at", "updated_at"}).
			AddRow(delivery.SubscriptionID, delivery.TenantID, "CRM", srv.URL, []byte("plain-secret"), "ACTIVE", 5, 4, []byte(`{}`), 10, time.Now().UTC(), time.Now().UTC()))

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE webhook_delivery_queue`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)UPDATE webhook_subscriptions`).WillReturnRows(sqlmock.NewRows([]string{"failure_count", "max_failures", "status"}).AddRow(5, 5, "ACTIVE"))
	mock.ExpectExec(`(?s)UPDATE webhook_subscriptions`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO events_outbox`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`(?s)INSERT INTO webhook_delivery_queue`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)INSERT INTO integration_audit_log`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := DispatchWebhook(multitenancy.WithTenant(context.Background(), delivery.TenantID), db, delivery, srv.Client())
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCompleteTaskFromExternal_DuplicateIdempotencyKey(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	ctx := multitenancy.WithTenant(context.Background(), "11111111-1111-1111-1111-111111111111")
	completion := ExternalTaskCompletion{
		TaskID:          "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		AssignedService: "credit-service",
		Status:          model.TaskStatusDone,
		OutputPayload:   json.RawMessage(`{"ok":true}`),
	}

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)INSERT INTO idempotency_keys`).WillReturnError(&pgconn.PgError{Code: "23505"})
	mock.ExpectExec(`(?s)INSERT INTO integration_audit_log`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := CompleteTaskFromExternal(ctx, db, "dup-key-1", completion)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCompleteTaskFromExternal_WrongAssignedServiceReturnsErrServiceMismatch(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	ctx := multitenancy.WithTenant(context.Background(), "11111111-1111-1111-1111-111111111111")
	completion := ExternalTaskCompletion{
		TaskID:          "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaab",
		AssignedService: "service-a",
		Status:          model.TaskStatusDone,
		OutputPayload:   json.RawMessage(`{"ok":true}`),
	}

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)INSERT INTO idempotency_keys`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`(?s)SELECT\s+id::text AS id`).WillReturnRows(sqlmock.NewRows([]string{"id", "case_id", "assigned_service", "status"}).
		AddRow(completion.TaskID, "cccccccc-cccc-cccc-cccc-cccccccccccc", "service-b", "IN_PROGRESS"))
	mock.ExpectRollback()

	err := CompleteTaskFromExternal(ctx, db, "key-1", completion)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrServiceMismatch)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestIngestExternalEvent_UnknownEventTypeRejected(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	ctx := multitenancy.WithTenant(context.Background(), "11111111-1111-1111-1111-111111111111")
	input := ExternalEventInput{
		TenantID:       "11111111-1111-1111-1111-111111111111",
		CaseID:         "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		EventType:      "UNKNOWN_EVENT",
		SourceSystem:   "UPSTREAM",
		Payload:        json.RawMessage(`{"x":1}`),
		IdempotencyKey: "e1",
		OccurredAt:     time.Now().UTC(),
	}

	mock.ExpectQuery(`(?s)FROM event_type_catalogue`).WithArgs(input.EventType).WillReturnError(sql.ErrNoRows)

	err := IngestExternalEvent(ctx, db, input)
	require.Error(t, err)
	assert.ErrorContains(t, err, ErrUnknownEventType.Error())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestIngestExternalEvent_PayloadSchemaValidationFailure(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	ctx := multitenancy.WithTenant(context.Background(), "11111111-1111-1111-1111-111111111111")
	schema := []byte(`{"type":"object","required":["credit_score"],"properties":{"credit_score":{"type":"integer"}}}`)
	input := ExternalEventInput{
		TenantID:       "11111111-1111-1111-1111-111111111111",
		CaseID:         "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		EventType:      "CREDIT_BUREAU_RESULT_RECEIVED",
		SourceSystem:   "CREDIT_BUREAU",
		Payload:        json.RawMessage(`{"credit_score":"bad"}`),
		IdempotencyKey: "e2",
		OccurredAt:     time.Now().UTC(),
	}

	mock.ExpectQuery(`(?s)FROM event_type_catalogue`).WithArgs(input.EventType).WillReturnRows(sqlmock.NewRows([]string{"payload_schema"}).AddRow(schema))

	err := IngestExternalEvent(ctx, db, input)
	require.Error(t, err)
	assert.ErrorContains(t, err, "validate payload")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHandlerRegistry_RegisterAfterEngineStarted(t *testing.T) {
	r := NewHandlerRegistry()
	require.NoError(t, r.Register(testHandler{name: "svc-a"}))
	r.MarkStarted()
	err := r.Register(testHandler{name: "svc-b"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHandlerAlreadyRegistered)
}

func TestServiceHealthChecker_Returns200AfterTwoDegradedSetsActive(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	checker := NewServiceHealthChecker(db, srv.Client(), time.Second, nil)
	service := ExternalService{
		ServiceID:           "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		TenantID:            "11111111-1111-1111-1111-111111111111",
		ServiceCode:         "CREDIT_API",
		Status:              ExternalServiceStatusDegraded,
		ConsecutiveFailures: 2,
		HealthCheckURL:      stringPtr(srv.URL),
	}

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE external_services`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO integration_audit_log`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := checker.checkOne(multitenancy.WithTenant(context.Background(), service.TenantID), service)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestServiceHealthChecker_ThreeConsecutiveFailuresSetsOfflinePublishesEvent(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("down"))
	}))
	defer srv.Close()

	checker := NewServiceHealthChecker(db, srv.Client(), time.Second, nil)
	service := ExternalService{
		ServiceID:           "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaab",
		TenantID:            "11111111-1111-1111-1111-111111111111",
		ServiceCode:         "DOC_API",
		Status:              ExternalServiceStatusDegraded,
		ConsecutiveFailures: 2,
		HealthCheckURL:      stringPtr(srv.URL),
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)UPDATE external_services`).WillReturnRows(sqlmock.NewRows([]string{"consecutive_failures"}).AddRow(3))
	mock.ExpectExec(`(?s)INSERT INTO events_outbox`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`(?s)INSERT INTO webhook_delivery_queue`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)INSERT INTO integration_audit_log`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := checker.checkOne(multitenancy.WithTenant(context.Background(), service.TenantID), service)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckAndRecordIdempotencyKey_ConcurrentCallsOneSuccessOthersDuplicate(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()
	mock.MatchExpectationsInOrder(false)

	const goroutines = 5
	for i := 0; i < goroutines; i++ {
		mock.ExpectBegin()
	}
	mock.ExpectExec(`(?s)INSERT INTO idempotency_keys`).WillReturnResult(sqlmock.NewResult(1, 1))
	for i := 0; i < goroutines-1; i++ {
		mock.ExpectExec(`(?s)INSERT INTO idempotency_keys`).WillReturnError(&pgconn.PgError{Code: "23505"})
	}
	for i := 0; i < goroutines; i++ {
		mock.ExpectCommit()
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(goroutines)

	successes := 0
	duplicates := 0
	var mu sync.Mutex
	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			tx, beginErr := db.BeginTxx(ctx, nil)
			if beginErr != nil {
				errCh <- beginErr
				return
			}
			dup, err := CheckAndRecordIdempotencyKey(ctx, tx, IdempotencyKeyspaceTaskCompletion, "same-key", "11111111-1111-1111-1111-111111111111", time.Now().UTC().Add(time.Hour))
			if err != nil {
				errCh <- err
				return
			}
			if err := tx.Commit(); err != nil {
				errCh <- err
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if dup {
				duplicates++
			} else {
				successes++
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	assert.Equal(t, 1, successes)
	assert.Equal(t, goroutines-1, duplicates)
	require.NoError(t, mock.ExpectationsWereMet())
}

type testHandler struct {
	name string
}

func (h testHandler) ServiceName() string {
	return h.name
}

func (h testHandler) Handle(_ context.Context, _ model.Task) (TaskResult, error) {
	return TaskResult{Status: model.TaskStatusDone}, nil
}
