package document

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestDocumentRetentionJobRun(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(sqlmock.Sqlmock, *mockDocumentStorage)
		wantErr bool
	}{
		{
			name: "happy path archive and delete",
			setup: func(mock sqlmock.Sqlmock, storage *mockDocumentStorage) {
				mock.ExpectBegin()
				rows := sqlmock.NewRows([]string{"document_id", "case_id", "document_type_code", "storage_path", "retention_policy", "uploaded_at"}).
					AddRow("doc-archive", "case-1", "INCOME_PROOF", "bucket-a/case-1/doc-archive.pdf", "ARCHIVE", time.Now().AddDate(-8, 0, 0)).
					AddRow("doc-delete", "case-1", "ID_DOCUMENT", "bucket-a/case-1/doc-delete.pdf", "DELETE", time.Now().AddDate(-8, 0, 0))
				mock.ExpectQuery(regexp.QuoteMeta("FROM case_documents cd")).
					WillReturnRows(rows)

				mock.ExpectExec(regexp.QuoteMeta("UPDATE case_documents")).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO events_outbox")).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO webhook_delivery_queue")).
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec(regexp.QuoteMeta("UPDATE case_documents")).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO events_outbox")).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO webhook_delivery_queue")).
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectCommit()

				storage.downloadFn = func(ctx context.Context, path string) (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewBufferString("payload")), nil
				}
				storage.uploadFn = func(ctx context.Context, bucket string, key string, content io.Reader, metadata map[string]string) (string, string, error) {
					_, _ = io.ReadAll(content)
					return bucket + "/" + key, "s3://" + bucket + "/" + key, nil
				}
				storage.deleteFn = func(ctx context.Context, path string) error {
					return nil
				}
			},
		},
		{
			name: "edge legal hold rows excluded",
			setup: func(mock sqlmock.Sqlmock, storage *mockDocumentStorage) {
				mock.ExpectBegin()
				rows := sqlmock.NewRows([]string{"document_id", "case_id", "document_type_code", "storage_path", "retention_policy", "uploaded_at"})
				mock.ExpectQuery(regexp.QuoteMeta("FROM case_documents cd")).
					WillReturnRows(rows)
				mock.ExpectCommit()
			},
		},
		{
			name: "failure storage delete error continues batch",
			setup: func(mock sqlmock.Sqlmock, storage *mockDocumentStorage) {
				mock.ExpectBegin()
				rows := sqlmock.NewRows([]string{"document_id", "case_id", "document_type_code", "storage_path", "retention_policy", "uploaded_at"}).
					AddRow("doc-delete", "case-1", "ID_DOCUMENT", "bucket-a/case-1/doc-delete.pdf", "DELETE", time.Now().AddDate(-8, 0, 0))
				mock.ExpectQuery(regexp.QuoteMeta("FROM case_documents cd")).
					WillReturnRows(rows)
				mock.ExpectCommit()

				storage.deleteFn = func(ctx context.Context, path string) error {
					return errors.New("delete failed")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()

			storage := &mockDocumentStorage{
				uploadFn: func(ctx context.Context, bucket string, key string, content io.Reader, metadata map[string]string) (string, string, error) {
					return bucket + "/" + key, "s3://" + bucket + "/" + key, nil
				},
				downloadFn: func(ctx context.Context, path string) (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewBufferString("payload")), nil
				},
				deleteFn: func(ctx context.Context, path string) error {
					return nil
				},
			}
			tt.setup(mock, storage)

			job := NewDocumentRetentionJob(db, storage, "archive-bucket", time.Hour, 100, nil)
			err = job.Run(context.Background())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestEnforceDocumentRetention(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(sqlmock.Sqlmock)
		wantArchive int
		wantDelete  int
		wantErr     bool
	}{
		{
			name: "happy path",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(regexp.QuoteMeta("WITH candidates AS")).
					WillReturnRows(sqlmock.NewRows([]string{"archived_count", "deleted_count"}).AddRow(5, 7))
				mock.ExpectCommit()
			},
			wantArchive: 5,
			wantDelete:  7,
		},
		{
			name: "edge zero candidates",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(regexp.QuoteMeta("WITH candidates AS")).
					WillReturnRows(sqlmock.NewRows([]string{"archived_count", "deleted_count"}).AddRow(0, 0))
				mock.ExpectCommit()
			},
			wantArchive: 0,
			wantDelete:  0,
		},
		{
			name: "failure query error",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(regexp.QuoteMeta("WITH candidates AS")).
					WillReturnError(errors.New("update failed"))
				mock.ExpectRollback()
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

			archived, deleted, err := EnforceDocumentRetention(context.Background(), db)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantArchive, archived)
				assert.Equal(t, tt.wantDelete, deleted)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
