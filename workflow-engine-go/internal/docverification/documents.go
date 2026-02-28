package docverification

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FetchedDocument is the payload from an ECM fetch operation.
type FetchedDocument struct {
	EcmDocumentID    string
	EcmReference     string
	DocumentCategory string
	DocumentName     string
	DocumentType     string
	PageCount        int
	EcmURL           string
	ReceivedAt       time.Time
}

// FetchCaseDocuments records documents that were fetched from ECM into
// the case_documents table, then publishes CASE_DOCUMENTS_FETCHED.
// The EcmURL is stored but NEVER included in the published event payload.
func FetchCaseDocuments(ctx context.Context, pool *pgxpool.Pool, caseID, tenantID string, docs []FetchedDocument) error {
	if len(docs) == 0 {
		slog.Warn("FetchCaseDocuments: called with empty document list", "case_id", caseID)
		return nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("FetchCaseDocuments: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, d := range docs {
		if d.EcmURL == "" {
			return fmt.Errorf("FetchCaseDocuments: ecm_url is required for document %s", d.EcmDocumentID)
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO case_documents (
				tenant_id, case_id, ecm_document_id, ecm_reference,
				document_category, document_name, document_type, page_count,
				ecm_url, received_at, status
			) VALUES (
				$1::uuid, $2::uuid, $3, $4,
				$5, $6, $7, $8,
				$9, $10, 'PENDING_REVIEW'
			)
			ON CONFLICT (tenant_id, case_id, ecm_document_id) DO UPDATE
			    SET ecm_url           = EXCLUDED.ecm_url,
			        document_name     = EXCLUDED.document_name,
			        page_count        = EXCLUDED.page_count,
			        received_at       = EXCLUDED.received_at,
			        updated_at        = now()
		`,
			tenantID, caseID, d.EcmDocumentID, d.EcmReference,
			d.DocumentCategory, d.DocumentName, d.DocumentType, d.PageCount,
			d.EcmURL, d.ReceivedAt,
		)
		if err != nil {
			return fmt.Errorf("FetchCaseDocuments: insert document %s: %w", d.EcmDocumentID, err)
		}
	}

	// Event payload intentionally omits ecm_url.
	safeDocList := make([]map[string]interface{}, len(docs))
	for i, d := range docs {
		safeDocList[i] = map[string]interface{}{
			"ecm_document_id":   d.EcmDocumentID,
			"document_category": d.DocumentCategory,
			"document_name":     d.DocumentName,
			"page_count":        d.PageCount,
		}
	}
	payload := map[string]interface{}{
		"case_id":        caseID,
		"document_count": len(docs),
		"documents":      safeDocList,
		"fetched_at":     time.Now().UTC(),
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventCaseDocumentsFetched, payload); err != nil {
		return fmt.Errorf("FetchCaseDocuments: publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("FetchCaseDocuments: commit: %w", err)
	}

	slog.Info("case documents fetched",
		"case_id", caseID,
		"document_count", len(docs))
	return nil
}

// GetCaseDocuments returns all case_documents for a case (without ecm_url).
func GetCaseDocuments(ctx context.Context, pool *pgxpool.Pool, caseID, tenantID string) ([]model.CaseDocument, error) {
	rows, err := pool.Query(ctx, `
		SELECT
			document_id::text, tenant_id::text, case_id::text,
			ecm_document_id, ecm_reference,
			document_category, document_name, document_type, page_count,
			received_at, status,
			reviewed_by::text, reviewed_at,
			metadata, created_at, updated_at
		FROM case_documents
		WHERE tenant_id = $1::uuid AND case_id = $2::uuid
		ORDER BY received_at ASC
	`, tenantID, caseID)
	if err != nil {
		return nil, fmt.Errorf("GetCaseDocuments: query: %w", err)
	}
	defer rows.Close()

	var docs []model.CaseDocument
	for rows.Next() {
		var d model.CaseDocument
		var metaRaw []byte
		if err := rows.Scan(
			&d.DocumentID, &d.TenantID, &d.CaseID,
			&d.EcmDocumentID, &d.EcmReference,
			&d.DocumentCategory, &d.DocumentName, &d.DocumentType, &d.PageCount,
			&d.ReceivedAt, &d.Status,
			&d.ReviewedBy, &d.ReviewedAt,
			&metaRaw, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("GetCaseDocuments: scan: %w", err)
		}
		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &d.Metadata)
		}
		// EcmURL is deliberately not loaded here — it's read-sensitive.
		docs = append(docs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetCaseDocuments: rows: %w", err)
	}
	return docs, nil
}

// UpdateDocumentStatus transitions a case_document's status.
func UpdateDocumentStatus(ctx context.Context, pool *pgxpool.Pool, documentID, tenantID, newStatus, reviewedByUserID string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("UpdateDocumentStatus: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var caseID string
	err = tx.QueryRow(ctx, `
		UPDATE case_documents
		SET status      = $3,
		    reviewed_by = $4::uuid,
		    reviewed_at = now(),
		    updated_at  = now()
		WHERE document_id = $1::uuid AND tenant_id = $2::uuid
		RETURNING case_id::text
	`, documentID, tenantID, newStatus, reviewedByUserID).Scan(&caseID)
	if err != nil {
		return fmt.Errorf("UpdateDocumentStatus: update: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("UpdateDocumentStatus: commit: %w", err)
	}
	slog.Debug("document status updated",
		"document_id", documentID, "new_status", newStatus, "case_id", caseID)
	return nil
}
