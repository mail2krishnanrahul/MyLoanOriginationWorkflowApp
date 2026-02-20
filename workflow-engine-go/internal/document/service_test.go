package document

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"regexp"
	"testing"
	"time"

	"workflow-engine/pkg/model"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

type mockDocumentStorage struct {
	uploadFn   func(context.Context, string, string, io.Reader, map[string]string) (string, string, error)
	downloadFn func(context.Context, string) (io.ReadCloser, error)
	deleteFn   func(context.Context, string) error
	presignFn  func(context.Context, string, time.Duration) (string, error)
}

func (m mockDocumentStorage) Upload(ctx context.Context, bucket string, key string, content io.Reader, metadata map[string]string) (string, string, error) {
	return m.uploadFn(ctx, bucket, key, content, metadata)
}

func (m mockDocumentStorage) Download(ctx context.Context, path string) (io.ReadCloser, error) {
	if m.downloadFn == nil {
		return io.NopCloser(bytes.NewBuffer(nil)), nil
	}
	return m.downloadFn(ctx, path)
}

func (m mockDocumentStorage) Delete(ctx context.Context, path string) error {
	if m.deleteFn == nil {
		return nil
	}
	return m.deleteFn(ctx, path)
}

func (m mockDocumentStorage) GeneratePresignedURL(ctx context.Context, path string, expiration time.Duration) (string, error) {
	if m.presignFn == nil {
		return "https://example.com", nil
	}
	return m.presignFn(ctx, path, expiration)
}

func TestUploadDocument(t *testing.T) {
	tests := []struct {
		name    string
		file    []byte
		setup   func(sqlmock.Sqlmock)
		storage mockDocumentStorage
		wantErr bool
		wantID  string
	}{
		{
			name: "happy path upload",
			file: []byte("pdf-bytes"),
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta("FROM cases c")).
					WillReturnRows(sqlmock.NewRows([]string{"case_type_code", "case_type_version", "current_stage_code"}).
						AddRow("HOME_LOAN", 1, "INITIAL_REVIEW"))

				mock.ExpectQuery(regexp.QuoteMeta("FROM document_types")).
					WillReturnRows(sqlmock.NewRows([]string{
						"id", "case_type_code", "case_type_version", "document_type_code", "display_name", "description",
						"allowed_extensions_json", "max_size_mb", "required_at_stage", "required_count_min", "required_count_max",
						"is_sensitive", "requires_verification", "verification_role", "retention_days", "retention_policy",
						"allowed_viewers_json", "created_at", "updated_at",
					}).AddRow(
						"dt-1", "HOME_LOAN", 1, "INCOME_PROOF", "Income Proof", nil,
						`["pdf","jpg"]`, 10, "INITIAL_REVIEW", 1, 3,
						true, false, nil, 2555, "ARCHIVE", `["UNDERWRITER"]`,
						time.Now().UTC(), time.Now().UTC(),
					))

				mock.ExpectQuery(regexp.QuoteMeta("SELECT gen_random_uuid()::text")).
					WillReturnRows(sqlmock.NewRows([]string{"gen_random_uuid"}).AddRow("doc-1"))

				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO case_documents")).
					WillReturnResult(sqlmock.NewResult(0, 1))

				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO events_outbox")).
					WillReturnResult(sqlmock.NewResult(0, 1))

				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO webhook_delivery_queue")).
					WillReturnResult(sqlmock.NewResult(0, 0))

				mock.ExpectQuery(regexp.QuoteMeta("FROM document_requests")).
					WillReturnError(sql.ErrNoRows)

				mock.ExpectQuery(regexp.QuoteMeta("FROM case_documents")).
					WillReturnRows(sqlmock.NewRows([]string{
						"id", "case_id", "task_id", "stage_code", "case_type_code", "case_type_version",
						"document_type_code", "filename", "file_extension", "file_size_bytes", "storage_provider",
						"storage_path", "storage_url", "checksum_sha256", "uploaded_by", "uploaded_at", "status",
						"rejection_reason", "verified_by", "verified_at", "metadata", "version", "superseded_by_document_id",
						"legal_hold", "created_at", "updated_at",
					}).AddRow(
						"doc-1", "case-1", nil, "INITIAL_REVIEW", "HOME_LOAN", 1,
						"INCOME_PROOF", "income.pdf", "pdf", int64(9), "LOCAL",
						"bucket-a/case-1/doc-1.pdf", "file:///tmp/doc-1.pdf", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"user-1", time.Now().UTC(), "UPLOADED", nil, nil, nil, []byte(`{}`), 1, nil, false, time.Now().UTC(), time.Now().UTC(),
					))
			},
			storage: mockDocumentStorage{
				uploadFn: func(ctx context.Context, bucket string, key string, content io.Reader, metadata map[string]string) (string, string, error) {
					_, _ = io.ReadAll(content)
					return "bucket-a/case-1/doc-1.pdf", "file:///tmp/doc-1.pdf", nil
				},
			},
			wantID: "doc-1",
		},
		{
			name: "edge upload exceeds max size",
			file: bytes.Repeat([]byte("a"), 2*1024*1024),
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta("FROM cases c")).
					WillReturnRows(sqlmock.NewRows([]string{"case_type_code", "case_type_version", "current_stage_code"}).
						AddRow("HOME_LOAN", 1, "INITIAL_REVIEW"))

				mock.ExpectQuery(regexp.QuoteMeta("FROM document_types")).
					WillReturnRows(sqlmock.NewRows([]string{
						"id", "case_type_code", "case_type_version", "document_type_code", "display_name", "description",
						"allowed_extensions_json", "max_size_mb", "required_at_stage", "required_count_min", "required_count_max",
						"is_sensitive", "requires_verification", "verification_role", "retention_days", "retention_policy",
						"allowed_viewers_json", "created_at", "updated_at",
					}).AddRow(
						"dt-1", "HOME_LOAN", 1, "INCOME_PROOF", "Income Proof", nil,
						`["pdf"]`, 1, "INITIAL_REVIEW", 1, 3,
						true, false, nil, 2555, "ARCHIVE", `["UNDERWRITER"]`,
						time.Now().UTC(), time.Now().UTC(),
					))
			},
			storage: mockDocumentStorage{
				uploadFn: func(ctx context.Context, bucket string, key string, content io.Reader, metadata map[string]string) (string, string, error) {
					return "", "", nil
				},
			},
			wantErr: true,
		},
		{
			name: "failure storage upload timeout",
			file: []byte("pdf-bytes"),
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta("FROM cases c")).
					WillReturnRows(sqlmock.NewRows([]string{"case_type_code", "case_type_version", "current_stage_code"}).
						AddRow("HOME_LOAN", 1, "INITIAL_REVIEW"))

				mock.ExpectQuery(regexp.QuoteMeta("FROM document_types")).
					WillReturnRows(sqlmock.NewRows([]string{
						"id", "case_type_code", "case_type_version", "document_type_code", "display_name", "description",
						"allowed_extensions_json", "max_size_mb", "required_at_stage", "required_count_min", "required_count_max",
						"is_sensitive", "requires_verification", "verification_role", "retention_days", "retention_policy",
						"allowed_viewers_json", "created_at", "updated_at",
					}).AddRow(
						"dt-1", "HOME_LOAN", 1, "INCOME_PROOF", "Income Proof", nil,
						`["pdf"]`, 10, "INITIAL_REVIEW", 1, 3,
						true, false, nil, 2555, "ARCHIVE", `["UNDERWRITER"]`,
						time.Now().UTC(), time.Now().UTC(),
					))

				mock.ExpectQuery(regexp.QuoteMeta("SELECT gen_random_uuid()::text")).
					WillReturnRows(sqlmock.NewRows([]string{"gen_random_uuid"}).AddRow("doc-2"))
			},
			storage: mockDocumentStorage{
				uploadFn: func(ctx context.Context, bucket string, key string, content io.Reader, metadata map[string]string) (string, string, error) {
					return "", "", errors.New("timeout")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()

			mock.ExpectBegin()
			tx, err := db.BeginTxx(context.Background(), nil)
			assert.NoError(t, err)
			tt.setup(mock)
			mock.ExpectRollback()

			doc, err := UploadDocument(
				context.Background(),
				tx,
				tt.storage,
				"case-1",
				"INCOME_PROOF",
				bytes.NewReader(tt.file),
				DocumentUploadMetadata{
					Filename:        "income.pdf",
					StorageBucket:   "bucket-a",
					StorageProvider: model.DocumentStorageProviderLocal,
					UploadedBy:      "user-1",
				},
			)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantID, doc.ID)
			}

			_ = tx.Rollback()
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestApproveDocument(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			name: "happy path approve",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta("FROM case_documents")).
					WillReturnRows(sqlmock.NewRows([]string{"case_id", "case_type_code", "case_type_version", "document_type_code", "status"}).
						AddRow("case-1", "HOME_LOAN", 1, "INCOME_PROOF", "UPLOADED"))
				mock.ExpectExec(regexp.QuoteMeta("UPDATE case_documents")).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(regexp.QuoteMeta("UPDATE document_verification_tasks")).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectQuery(regexp.QuoteMeta("FROM document_requests")).
					WillReturnError(sql.ErrNoRows)
				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO events_outbox")).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO webhook_delivery_queue")).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
		},
		{
			name: "edge already verified",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta("FROM case_documents")).
					WillReturnRows(sqlmock.NewRows([]string{"case_id", "case_type_code", "case_type_version", "document_type_code", "status"}).
						AddRow("case-1", "HOME_LOAN", 1, "INCOME_PROOF", "VERIFIED"))
			},
			wantErr: true,
		},
		{
			name: "failure update error",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta("FROM case_documents")).
					WillReturnRows(sqlmock.NewRows([]string{"case_id", "case_type_code", "case_type_version", "document_type_code", "status"}).
						AddRow("case-1", "HOME_LOAN", 1, "INCOME_PROOF", "UPLOADED"))
				mock.ExpectExec(regexp.QuoteMeta("UPDATE case_documents")).
					WillReturnError(errors.New("update failed"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()

			mock.ExpectBegin()
			tx, err := db.BeginTxx(context.Background(), nil)
			assert.NoError(t, err)
			tt.setup(mock)
			mock.ExpectRollback()

			err = ApproveDocument(context.Background(), tx, "doc-1", "verifier-1")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			_ = tx.Rollback()
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRejectDocument(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			name: "happy path reject",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta("FROM case_documents")).
					WillReturnRows(sqlmock.NewRows([]string{"case_id", "case_type_code", "case_type_version", "document_type_code", "status"}).
						AddRow("case-1", "HOME_LOAN", 1, "INCOME_PROOF", "UPLOADED"))
				mock.ExpectExec(regexp.QuoteMeta("UPDATE case_documents")).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(regexp.QuoteMeta("UPDATE document_verification_tasks")).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectQuery(regexp.QuoteMeta("FROM document_requests")).
					WillReturnError(sql.ErrNoRows)
				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO events_outbox")).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO webhook_delivery_queue")).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
		},
		{
			name: "edge reject archived document blocked",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta("FROM case_documents")).
					WillReturnRows(sqlmock.NewRows([]string{"case_id", "case_type_code", "case_type_version", "document_type_code", "status"}).
						AddRow("case-1", "HOME_LOAN", 1, "INCOME_PROOF", "ARCHIVED"))
			},
			wantErr: true,
		},
		{
			name: "failure update mapping error",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta("FROM case_documents")).
					WillReturnRows(sqlmock.NewRows([]string{"case_id", "case_type_code", "case_type_version", "document_type_code", "status"}).
						AddRow("case-1", "HOME_LOAN", 1, "INCOME_PROOF", "UPLOADED"))
				mock.ExpectExec(regexp.QuoteMeta("UPDATE case_documents")).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(regexp.QuoteMeta("UPDATE document_verification_tasks")).
					WillReturnError(errors.New("mapping update failed"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()

			mock.ExpectBegin()
			tx, err := db.BeginTxx(context.Background(), nil)
			assert.NoError(t, err)
			tt.setup(mock)
			mock.ExpectRollback()

			err = RejectDocument(context.Background(), tx, "doc-1", "verifier-1", "blurred image")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			_ = tx.Rollback()
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCheckDocumentRequirements(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(sqlmock.Sqlmock)
		wantFulfilled bool
		wantMissing   int
		wantErr       bool
	}{
		{
			name: "happy all fulfilled",
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "case_type_code", "case_type_version", "document_type_code", "display_name", "description",
					"allowed_extensions_json", "max_size_mb", "required_at_stage", "required_count_min", "required_count_max",
					"is_sensitive", "requires_verification", "verification_role", "retention_days", "retention_policy",
					"allowed_viewers_json", "current_count", "request_status",
				})
				mock.ExpectQuery(regexp.QuoteMeta("FROM document_requests dr")).
					WillReturnRows(rows)
			},
			wantFulfilled: true,
			wantMissing:   0,
		},
		{
			name: "edge missing required docs",
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "case_type_code", "case_type_version", "document_type_code", "display_name", "description",
					"allowed_extensions_json", "max_size_mb", "required_at_stage", "required_count_min", "required_count_max",
					"is_sensitive", "requires_verification", "verification_role", "retention_days", "retention_policy",
					"allowed_viewers_json", "current_count", "request_status",
				}).AddRow(
					"dt-1", "HOME_LOAN", 1, "INCOME_PROOF", "Income Proof", nil,
					`["pdf"]`, 10, "INITIAL_REVIEW", 1, 3, true, false, nil, 2555, "ARCHIVE", `["UNDERWRITER"]`, 0, "PENDING",
				)
				mock.ExpectQuery(regexp.QuoteMeta("FROM document_requests dr")).
					WillReturnRows(rows)
			},
			wantFulfilled: false,
			wantMissing:   1,
		},
		{
			name: "failure query error",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta("FROM document_requests dr")).
					WillReturnError(errors.New("db unavailable"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()
			tt.setup(mock)

			fulfilled, missing, err := CheckDocumentRequirements(context.Background(), db, "case-1", "INITIAL_REVIEW")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantFulfilled, fulfilled)
				assert.Len(t, missing, tt.wantMissing)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestDocumentLifecycleEndToEnd(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()

	storage := mockDocumentStorage{
		uploadFn: func(ctx context.Context, bucket string, key string, content io.Reader, metadata map[string]string) (string, string, error) {
			_, _ = io.ReadAll(content)
			return bucket + "/" + key, "file:///tmp/" + key, nil
		},
	}

	// 1) Upload initial document
	mock.ExpectBegin()
	txUpload, err := db.BeginTxx(context.Background(), nil)
	assert.NoError(t, err)
	mock.ExpectQuery(regexp.QuoteMeta("FROM cases c")).
		WillReturnRows(sqlmock.NewRows([]string{"case_type_code", "case_type_version", "current_stage_code"}).
			AddRow("HOME_LOAN", 1, "INITIAL_REVIEW"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM document_types")).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "case_type_code", "case_type_version", "document_type_code", "display_name", "description",
			"allowed_extensions_json", "max_size_mb", "required_at_stage", "required_count_min", "required_count_max",
			"is_sensitive", "requires_verification", "verification_role", "retention_days", "retention_policy",
			"allowed_viewers_json", "created_at", "updated_at",
		}).AddRow("dt-1", "HOME_LOAN", 1, "INCOME_PROOF", "Income Proof", nil, `["pdf"]`, 10, "INITIAL_REVIEW", 1, 3, true, false, nil, 2555, "DELETE", `["UNDERWRITER"]`, time.Now().UTC(), time.Now().UTC()))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT gen_random_uuid()::text")).
		WillReturnRows(sqlmock.NewRows([]string{"gen_random_uuid"}).AddRow("doc-v1"))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO case_documents")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO events_outbox")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO webhook_delivery_queue")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("FROM document_requests")).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta("FROM case_documents")).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "case_id", "task_id", "stage_code", "case_type_code", "case_type_version",
			"document_type_code", "filename", "file_extension", "file_size_bytes", "storage_provider",
			"storage_path", "storage_url", "checksum_sha256", "uploaded_by", "uploaded_at", "status",
			"rejection_reason", "verified_by", "verified_at", "metadata", "version", "superseded_by_document_id",
			"legal_hold", "created_at", "updated_at",
		}).AddRow(
			"doc-v1", "case-1", nil, "INITIAL_REVIEW", "HOME_LOAN", 1,
			"INCOME_PROOF", "income-v1.pdf", "pdf", int64(9), "LOCAL", "bucket-a/case-1/doc-v1.pdf",
			"file:///tmp/case-1/doc-v1.pdf", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"user-1", time.Now().UTC(), "UPLOADED", nil, nil, nil, []byte(`{}`), 1, nil, false, time.Now().UTC(), time.Now().UTC(),
		))
	mock.ExpectRollback()
	docV1, err := UploadDocument(context.Background(), txUpload, storage, "case-1", "INCOME_PROOF", bytes.NewBufferString("v1"), DocumentUploadMetadata{
		Filename:        "income-v1.pdf",
		StorageProvider: model.DocumentStorageProviderLocal,
		StorageBucket:   "bucket-a",
		UploadedBy:      "user-1",
	})
	assert.NoError(t, err)
	assert.Equal(t, "doc-v1", docV1.ID)
	_ = txUpload.Rollback()

	// 2) Approve
	mock.ExpectBegin()
	txApprove, err := db.BeginTxx(context.Background(), nil)
	assert.NoError(t, err)
	mock.ExpectQuery(regexp.QuoteMeta("FROM case_documents")).
		WillReturnRows(sqlmock.NewRows([]string{"case_id", "case_type_code", "case_type_version", "document_type_code", "status"}).
			AddRow("case-1", "HOME_LOAN", 1, "INCOME_PROOF", "UPLOADED"))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE case_documents")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE document_verification_tasks")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("FROM document_requests")).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO events_outbox")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO webhook_delivery_queue")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	assert.NoError(t, ApproveDocument(context.Background(), txApprove, "doc-v1", "verifier-1"))
	_ = txApprove.Rollback()

	// 3) Requirements now fulfilled check
	mock.ExpectQuery(regexp.QuoteMeta("FROM document_requests dr")).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "case_type_code", "case_type_version", "document_type_code", "display_name", "description",
			"allowed_extensions_json", "max_size_mb", "required_at_stage", "required_count_min", "required_count_max",
			"is_sensitive", "requires_verification", "verification_role", "retention_days", "retention_policy",
			"allowed_viewers_json", "current_count", "request_status",
		}))
	fulfilled, missing, err := CheckDocumentRequirements(context.Background(), db, "case-1", "INITIAL_REVIEW")
	assert.NoError(t, err)
	assert.True(t, fulfilled)
	assert.Len(t, missing, 0)

	// 4) Upload new version (old archived)
	mock.ExpectBegin()
	txV2, err := db.BeginTxx(context.Background(), nil)
	assert.NoError(t, err)
	mock.ExpectQuery(regexp.QuoteMeta("FROM cases c")).
		WillReturnRows(sqlmock.NewRows([]string{"case_type_code", "case_type_version", "current_stage_code"}).
			AddRow("HOME_LOAN", 1, "INITIAL_REVIEW"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM document_types")).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "case_type_code", "case_type_version", "document_type_code", "display_name", "description",
			"allowed_extensions_json", "max_size_mb", "required_at_stage", "required_count_min", "required_count_max",
			"is_sensitive", "requires_verification", "verification_role", "retention_days", "retention_policy",
			"allowed_viewers_json", "created_at", "updated_at",
		}).AddRow("dt-1", "HOME_LOAN", 1, "INCOME_PROOF", "Income Proof", nil, `["pdf"]`, 10, "INITIAL_REVIEW", 1, 3, true, false, nil, 2555, "DELETE", `["UNDERWRITER"]`, time.Now().UTC(), time.Now().UTC()))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT gen_random_uuid()::text")).
		WillReturnRows(sqlmock.NewRows([]string{"gen_random_uuid"}).AddRow("doc-v2"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT version, status")).
		WillReturnRows(sqlmock.NewRows([]string{"version", "status"}).AddRow(1, "VERIFIED"))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO case_documents")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE case_documents")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO events_outbox")).WillReturnResult(sqlmock.NewResult(0, 1)) // version created
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO webhook_delivery_queue")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO events_outbox")).WillReturnResult(sqlmock.NewResult(0, 1)) // uploaded
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO webhook_delivery_queue")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("FROM document_requests")).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta("FROM case_documents")).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "case_id", "task_id", "stage_code", "case_type_code", "case_type_version",
			"document_type_code", "filename", "file_extension", "file_size_bytes", "storage_provider",
			"storage_path", "storage_url", "checksum_sha256", "uploaded_by", "uploaded_at", "status",
			"rejection_reason", "verified_by", "verified_at", "metadata", "version", "superseded_by_document_id",
			"legal_hold", "created_at", "updated_at",
		}).AddRow(
			"doc-v2", "case-1", nil, "INITIAL_REVIEW", "HOME_LOAN", 1,
			"INCOME_PROOF", "income-v2.pdf", "pdf", int64(9), "LOCAL", "bucket-a/case-1/doc-v2.pdf",
			"file:///tmp/case-1/doc-v2.pdf", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"user-1", time.Now().UTC(), "UPLOADED", nil, nil, nil, []byte(`{}`), 2, nil, false, time.Now().UTC(), time.Now().UTC(),
		))
	mock.ExpectRollback()
	docV2, err := UploadDocument(context.Background(), txV2, storage, "case-1", "INCOME_PROOF", bytes.NewBufferString("v2"), DocumentUploadMetadata{
		Filename:             "income-v2.pdf",
		StorageProvider:      model.DocumentStorageProviderLocal,
		StorageBucket:        "bucket-a",
		UploadedBy:           "user-1",
		SupersedesDocumentID: &docV1.ID,
	})
	assert.NoError(t, err)
	assert.Equal(t, 2, docV2.Version)
	_ = txV2.Rollback()

	// 5) Retention sweep marks deletion after policy horizon.
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("WITH candidates AS")).
		WillReturnRows(sqlmock.NewRows([]string{"archived_count", "deleted_count"}).AddRow(0, 1))
	mock.ExpectCommit()
	archived, deleted, err := EnforceDocumentRetention(context.Background(), db)
	assert.NoError(t, err)
	assert.Equal(t, 0, archived)
	assert.Equal(t, 1, deleted)

	assert.NoError(t, mock.ExpectationsWereMet())
}
