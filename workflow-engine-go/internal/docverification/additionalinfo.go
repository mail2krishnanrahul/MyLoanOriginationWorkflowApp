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

// RequestAdditionalInfo creates an additional info request and publishes
// ADDITIONAL_INFO_REQUESTED so the banker is notified.
func RequestAdditionalInfo(
	ctx context.Context,
	pool *pgxpool.Pool,
	caseID, tenantID, requestedByUserID string,
	input model.AdditionalInfoRequestInput,
) (*model.AdditionalInfoRequest, error) {
	if len(input.RequestedDocuments) == 0 {
		return nil, fmt.Errorf("RequestAdditionalInfo: at least one requested document is required")
	}
	if input.DueDate.Before(time.Now().UTC()) {
		return nil, fmt.Errorf("RequestAdditionalInfo: due_date must be in the future")
	}

	docsJSON, err := json.Marshal(input.RequestedDocuments)
	if err != nil {
		return nil, fmt.Errorf("RequestAdditionalInfo: marshal docs: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("RequestAdditionalInfo: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var requestID string
	err = tx.QueryRow(ctx, `
		INSERT INTO additional_info_requests (
			tenant_id, case_id, requested_by,
			requested_documents, message_to_banker, due_date, status
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid,
			$4::jsonb, $5, $6, 'PENDING'
		)
		RETURNING request_id::text
	`, tenantID, caseID, requestedByUserID,
		docsJSON, input.MessageToBanker, input.DueDate,
	).Scan(&requestID)
	if err != nil {
		return nil, fmt.Errorf("RequestAdditionalInfo: insert: %w", err)
	}

	payload := map[string]interface{}{
		"case_id":             caseID,
		"request_id":          requestID,
		"requested_documents": input.RequestedDocuments,
		"message_to_banker":   input.MessageToBanker,
		"due_date":            input.DueDate,
		"requested_by":        requestedByUserID,
		"timestamp":           time.Now().UTC(),
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventAdditionalInfoRequested, payload); err != nil {
		return nil, fmt.Errorf("RequestAdditionalInfo: publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("RequestAdditionalInfo: commit: %w", err)
	}

	slog.Info("additional info requested",
		"case_id", caseID,
		"request_id", requestID,
		"docs_count", len(input.RequestedDocuments),
		"due_date", input.DueDate)

	msg := input.MessageToBanker
	return &model.AdditionalInfoRequest{
		RequestID:          requestID,
		TenantID:           tenantID,
		CaseID:             caseID,
		RequestedBy:        requestedByUserID,
		RequestedDocuments: input.RequestedDocuments,
		MessageToBanker:    &msg,
		DueDate:            input.DueDate,
		Status:             "PENDING",
	}, nil
}

// BankerSubmitsAdditionalInfo records the banker's resubmission against an
// open additional info request, then publishes BANKER_RESUBMISSION_RECEIVED.
func BankerSubmitsAdditionalInfo(
	ctx context.Context,
	pool *pgxpool.Pool,
	requestID, tenantID string,
	resubmission model.BankerResubmission,
) error {
	if len(resubmission.SubmittedEcmIDs) == 0 {
		return fmt.Errorf("BankerSubmitsAdditionalInfo: at least one submitted ecm_id is required")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("BankerSubmitsAdditionalInfo: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var caseID string
	now := time.Now().UTC()
	err = tx.QueryRow(ctx, `
		UPDATE additional_info_requests
		SET status           = 'RECEIVED',
		    submitted_ecm_ids = $3::text[],
		    banker_notes     = $4,
		    received_at      = $5,
		    updated_at       = $5
		WHERE request_id = $1::uuid
		  AND tenant_id  = $2::uuid
		  AND status = 'PENDING'
		RETURNING case_id::text
	`, requestID, tenantID, resubmission.SubmittedEcmIDs, resubmission.BankerNotes, now,
	).Scan(&caseID)
	if err != nil {
		return fmt.Errorf("BankerSubmitsAdditionalInfo: update request: %w", err)
	}

	payload := map[string]interface{}{
		"case_id":           caseID,
		"request_id":        requestID,
		"submitted_ecm_ids": resubmission.SubmittedEcmIDs,
		"received_at":       now,
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventBankerResubmissionReceived, payload); err != nil {
		return fmt.Errorf("BankerSubmitsAdditionalInfo: publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("BankerSubmitsAdditionalInfo: commit: %w", err)
	}

	slog.Info("banker resubmission received",
		"case_id", caseID,
		"request_id", requestID,
		"ecm_ids_count", len(resubmission.SubmittedEcmIDs))
	return nil
}

// GetAdditionalInfoRequests returns all requests for a case.
func GetAdditionalInfoRequests(ctx context.Context, pool *pgxpool.Pool, caseID, tenantID string) ([]model.AdditionalInfoRequest, error) {
	rows, err := pool.Query(ctx, `
		SELECT request_id::text, tenant_id::text, case_id::text, requested_by::text,
		       requested_documents, message_to_banker, due_date, status,
		       banker_notes, submitted_ecm_ids, received_at, created_at, updated_at
		FROM additional_info_requests
		WHERE case_id = $1::uuid AND tenant_id = $2::uuid
		ORDER BY created_at DESC
	`, caseID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("GetAdditionalInfoRequests: query: %w", err)
	}
	defer rows.Close()

	var requests []model.AdditionalInfoRequest
	for rows.Next() {
		var req model.AdditionalInfoRequest
		var docsRaw []byte
		if err := rows.Scan(
			&req.RequestID, &req.TenantID, &req.CaseID, &req.RequestedBy,
			&docsRaw, &req.MessageToBanker, &req.DueDate, &req.Status,
			&req.BankerNotes, &req.SubmittedEcmIDs, &req.ReceivedAt,
			&req.CreatedAt, &req.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("GetAdditionalInfoRequests: scan: %w", err)
		}
		if len(docsRaw) > 0 {
			_ = json.Unmarshal(docsRaw, &req.RequestedDocuments)
		}
		requests = append(requests, req)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetAdditionalInfoRequests: rows: %w", err)
	}
	return requests, nil
}
