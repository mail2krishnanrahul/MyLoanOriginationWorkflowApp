package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"workflow-engine/internal/multitenancy"
	"workflow-engine/pkg/model"

	"crypto/rand"
	"crypto/sha256"

	"github.com/jmoiron/sqlx"
)

const (
	caseStatusOpen                = "OPEN"
	caseStatusInProgress          = "IN_PROGRESS"
	caseStatusPendingInformation  = "PENDING_INFORMATION"
	caseStatusVerified            = "VERIFIED"
	caseStatusCancelled           = "CANCELLED"
	caseStatusCompleted           = "COMPLETED"
	documentVerificationCaseType  = "DOCUMENT_VERIFICATION"
	documentVerificationStageCode = "DOCUMENT_VERIFICATION"
	documentVerificationActivity  = "DOCUMENT_VERIFICATION_ACTIVITY"
	auditActorIngestionService    = "deal-ingestion-service"
)

type dealIngestionService struct {
	db     *sqlx.DB
	logger *slog.Logger
}

func NewDealIngestionService(db *sqlx.DB, logger *slog.Logger) *dealIngestionService {
	if logger == nil {
		logger = slog.Default()
	}
	return &dealIngestionService{db: db, logger: logger}
}

func (s *dealIngestionService) IngestDeal(
	ctx context.Context,
	req IngestDealRequest,
	idempotencyKey string,
) (int, *IngestResponse, *ProblemDetail) {
	if s == nil || s.db == nil {
		return httpStatusProblem(http.StatusInternalServerError, "Service Misconfigured", "ingestion service is not configured")
	}

	tenantID, err := multitenancy.TenantFromContext(ctx)
	if err != nil {
		return httpStatusProblem(http.StatusBadRequest, "Tenant Context Missing", err.Error())
	}

	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey != "" {
		cachedStatus, cachedResp, found, cacheErr := s.getCachedIdempotentResponse(ctx, tenantID, idempotencyKey)
		if cacheErr != nil {
			return httpStatusProblem(http.StatusInternalServerError, "Idempotency Lookup Failure", cacheErr.Error())
		}
		if found {
			return cachedStatus, &cachedResp, nil
		}
	}

	snapshotTimestamp, problem := parseSnapshotTimestamp(req.SnapshotTimestamp)
	if problem != nil {
		return 422, nil, problem
	}

	req.SourceSystem = strings.ToUpper(strings.TrimSpace(req.SourceSystem))
	if req.SourceSystem == "" {
		return 422, nil, &ProblemDetail{
			Title:  "Snapshot Schema Validation Failure",
			Status: 422,
			Detail: "sourceSystem is required",
		}
	}
	if !isAllowedSourceSystem(req.SourceSystem) {
		return 422, nil, &ProblemDetail{
			Title:  "Snapshot Schema Validation Failure",
			Status: 422,
			Detail: "sourceSystem must be one of ORIGINATION, MANUAL_OVERRIDE, MIGRATION",
		}
	}

	if len(req.Snapshot) == 0 {
		return 422, nil, &ProblemDetail{
			Title:  "Snapshot Schema Validation Failure",
			Status: 422,
			Detail: "snapshot is required",
		}
	}

	var deal BusinessLendingDeal
	if err := json.Unmarshal(req.Snapshot, &deal); err != nil {
		return 422, nil, &ProblemDetail{
			Title:  "Snapshot Schema Validation Failure",
			Status: 422,
			Detail: "snapshot is not valid BusinessLendingDeal JSON",
		}
	}
	if schemaProblem := validateBusinessLendingDealRequiredFields(deal); schemaProblem != nil {
		return 422, nil, schemaProblem
	}

	currentHash, err := computeSnapshotHash(req.Snapshot)
	if err != nil {
		return httpStatusProblem(http.StatusInternalServerError, "Snapshot Hashing Failure", err.Error())
	}
	warnings := validateSnapshotWarnings(deal)

	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return httpStatusProblem(http.StatusInternalServerError, "Transaction Start Failure", err.Error())
	}
	defer func() {
		_ = tx.Rollback()
	}()

	locked, lockErr := s.tryAcquireDealLock(ctx, tx, tenantID, deal.DealID)
	if lockErr != nil {
		return httpStatusProblem(http.StatusInternalServerError, "Concurrent Lock Failure", lockErr.Error())
	}
		if !locked {
			return 409, nil, &ProblemDetail{
				Title:  "Concurrent Ingestion Conflict",
				Status: 409,
				Detail: fmt.Sprintf("another ingestion for dealId=%s is currently in-flight", deal.DealID),
			}
		}

	now := time.Now().UTC()
	resp := IngestResponse{
		IngestID:     mustUUID(),
		DealID:       deal.DealID,
		SnapshotHash: currentHash,
		CaseAction:   DealCaseActionNone,
		ProcessedAt:  now,
		Warnings:     append([]string(nil), warnings...),
	}

	existing, existingErr := s.getIngestedDealRecordForUpdate(ctx, tx, tenantID, deal.DealID)
	if existingErr != nil {
		return httpStatusProblem(http.StatusInternalServerError, "Ingest Record Lookup Failure", existingErr.Error())
	}

	var diff *DealDiff
	var caseID *string

	switch {
	case existing == nil:
		if err := s.insertIngestedDealRecord(ctx, tx, tenantID, deal.DealID, currentHash, req.Snapshot, snapshotTimestamp, now); err != nil {
			return httpStatusProblem(http.StatusInternalServerError, "Ingest Record Insert Failure", err.Error())
		}
		resp.Outcome = DealIngestOutcomeFirstIngestion

	case existing.SnapshotHash == currentHash:
		if err := s.touchIngestedDealRecord(ctx, tx, tenantID, deal.DealID, now); err != nil {
			return httpStatusProblem(http.StatusInternalServerError, "Ingest Record Update Failure", err.Error())
		}
		resp.Outcome = DealIngestOutcomeIdempotentNoOp
		resp.CaseAction = DealCaseActionNone

		if historyErr := s.insertIngestionHistory(ctx, tx, tenantID, deal.DealID, req, resp, snapshotTimestamp); historyErr != nil {
			return httpStatusProblem(http.StatusInternalServerError, "Ingestion History Failure", historyErr.Error())
		}
		if idempotencyKey != "" {
			if cacheErr := s.upsertIdempotencyCache(ctx, tx, tenantID, idempotencyKey, http.StatusAccepted, resp); cacheErr != nil {
				return httpStatusProblem(http.StatusInternalServerError, "Idempotency Cache Failure", cacheErr.Error())
			}
		}
		if err := tx.Commit(); err != nil {
			return httpStatusProblem(http.StatusInternalServerError, "Transaction Commit Failure", err.Error())
		}
		s.logger.Info("idempotent re-delivery for deal", "deal_id", deal.DealID)
		return http.StatusAccepted, &resp, nil

	default:
		if snapshotTimestamp.Before(existing.LastSnapshotTimestamp) {
			resp.Outcome = DealIngestOutcomeIdempotentNoOp
			resp.Warnings = append(resp.Warnings, "Late delivery rejected: incoming snapshot is older than stored snapshot.")
			resp.CaseAction = DealCaseActionNone

			if historyErr := s.insertIngestionHistory(ctx, tx, tenantID, deal.DealID, req, resp, snapshotTimestamp); historyErr != nil {
				return httpStatusProblem(http.StatusInternalServerError, "Ingestion History Failure", historyErr.Error())
			}
			if idempotencyKey != "" {
				if cacheErr := s.upsertIdempotencyCache(ctx, tx, tenantID, idempotencyKey, http.StatusAccepted, resp); cacheErr != nil {
					return httpStatusProblem(http.StatusInternalServerError, "Idempotency Cache Failure", cacheErr.Error())
				}
			}
			if err := tx.Commit(); err != nil {
				return httpStatusProblem(http.StatusInternalServerError, "Transaction Commit Failure", err.Error())
			}
			return http.StatusAccepted, &resp, nil
		}

		if err := s.updateIngestedDealRecord(ctx, tx, tenantID, deal.DealID, currentHash, req.Snapshot, snapshotTimestamp, now); err != nil {
			return httpStatusProblem(http.StatusInternalServerError, "Ingest Record Update Failure", err.Error())
		}

		resp.Outcome = DealIngestOutcomeUpdated
		diff, err = computeDealDiff(existing.DealSnapshot, req.Snapshot)
		if err != nil {
			return httpStatusProblem(http.StatusInternalServerError, "Snapshot Diff Failure", err.Error())
		}
		resp.Diff = diff
	}

	action, resolvedCaseID, exceptionDetail, actionErr := s.determineAndApplyCaseAction(ctx, tx, tenantID, deal, req.Snapshot, currentHash, resp.Outcome, diff, now)
	if actionErr != nil {
		if errors.Is(actionErr, errDuplicateOpenCases) {
			return 500, nil, &ProblemDetail{
				Title:  "Case Integrity Violation — Duplicate Open Cases",
				Status: 500,
				Detail: actionErr.Error(),
			}
		}
		return httpStatusProblem(http.StatusInternalServerError, "Case Action Failure", actionErr.Error())
	}

	resp.CaseAction = action
	if resolvedCaseID != "" {
		caseID = &resolvedCaseID
		resp.CaseID = caseID
	}
	if action == DealCaseActionDealChangeFlagged {
		if resp.Metadata == nil {
			resp.Metadata = make(map[string]interface{})
		}
		resp.Metadata["notifyAssignee"] = true
	}
	if exceptionDetail != nil {
		resp.Outcome = DealIngestOutcomeException
		resp.ExceptionDetail = exceptionDetail
		resp.CaseAction = DealCaseActionNone
	}

	if historyErr := s.insertIngestionHistory(ctx, tx, tenantID, deal.DealID, req, resp, snapshotTimestamp); historyErr != nil {
		return httpStatusProblem(http.StatusInternalServerError, "Ingestion History Failure", historyErr.Error())
	}
	if idempotencyKey != "" {
		if cacheErr := s.upsertIdempotencyCache(ctx, tx, tenantID, idempotencyKey, http.StatusAccepted, resp); cacheErr != nil {
			return httpStatusProblem(http.StatusInternalServerError, "Idempotency Cache Failure", cacheErr.Error())
		}
	}

	if err := tx.Commit(); err != nil {
		return httpStatusProblem(http.StatusInternalServerError, "Transaction Commit Failure", err.Error())
	}

	return http.StatusAccepted, &resp, nil
}

func (s *dealIngestionService) GetIngestionHistory(ctx context.Context, dealID string) (IngestionHistoryResponse, *ProblemDetail) {
	if s == nil || s.db == nil {
		return IngestionHistoryResponse{}, &ProblemDetail{Title: "Service Misconfigured", Status: 500}
	}
	dealID = strings.TrimSpace(dealID)
	if dealID == "" {
		return IngestionHistoryResponse{}, &ProblemDetail{Title: "Deal Identifier Missing", Status: 422, Detail: "dealId path parameter is required"}
	}
	tenantID, err := multitenancy.TenantFromContext(ctx)
	if err != nil {
		return IngestionHistoryResponse{}, &ProblemDetail{Title: "Tenant Context Missing", Status: 400, Detail: err.Error()}
	}

	type row struct {
		IngestID        string          `db:"ingest_id"`
		SnapshotTS      time.Time       `db:"snapshot_timestamp"`
		ReceivedAt      time.Time       `db:"received_at"`
		SnapshotHash    string          `db:"snapshot_hash"`
		Outcome         string          `db:"outcome"`
		CaseAction      string          `db:"case_action"`
		DiffSummary     json.RawMessage `db:"diff_summary"`
		Warnings        json.RawMessage `db:"warnings"`
		SourceSystem    sql.NullString  `db:"source_system"`
		CorrelationID   sql.NullString  `db:"correlation_id"`
		CaseID          *string         `db:"case_id"`
		ExceptionDetail json.RawMessage `db:"exception_detail"`
	}

	rows := make([]row, 0)
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT
			ingest_id::text AS ingest_id,
			snapshot_timestamp,
			received_at,
			snapshot_hash,
			outcome,
			case_action,
			diff_summary,
			warnings,
			source_system,
			correlation_id,
			case_id::text AS case_id,
			exception_detail
		FROM deal_ingestion_history
		WHERE tenant_id = $1::uuid
		  AND deal_id = $2
		ORDER BY received_at DESC
	`, tenantID, dealID); err != nil {
		return IngestionHistoryResponse{}, &ProblemDetail{Title: "History Lookup Failure", Status: 500, Detail: err.Error()}
	}

	result := IngestionHistoryResponse{DealID: dealID, Ingestions: make([]IngestionRecord, 0, len(rows))}
	for _, record := range rows {
		ingestion := IngestionRecord{
			IngestID:          record.IngestID,
			SnapshotTimestamp: record.SnapshotTS,
			ReceivedAt:        record.ReceivedAt,
			SnapshotHash:      record.SnapshotHash,
			Outcome:           DealIngestOutcome(record.Outcome),
			CaseAction:        DealCaseAction(record.CaseAction),
			CaseID:            record.CaseID,
		}
		if len(record.DiffSummary) > 0 && string(record.DiffSummary) != "null" {
			var diff DealDiff
			if err := json.Unmarshal(record.DiffSummary, &diff); err == nil {
				ingestion.DiffSummary = &diff
			}
		}
		if len(record.Warnings) > 0 && string(record.Warnings) != "null" {
			var warnings []string
			if err := json.Unmarshal(record.Warnings, &warnings); err == nil {
				ingestion.Warnings = warnings
			}
		}
		if record.SourceSystem.Valid {
			ingestion.SourceSystem = record.SourceSystem.String
		}
		if record.CorrelationID.Valid {
			ingestion.CorrelationID = record.CorrelationID.String
		}
		if len(record.ExceptionDetail) > 0 && string(record.ExceptionDetail) != "null" {
			var exception IngestExceptionDetail
			if err := json.Unmarshal(record.ExceptionDetail, &exception); err == nil {
				ingestion.ExceptionDetail = &exception
			}
		}
		result.Ingestions = append(result.Ingestions, ingestion)
	}

	return result, nil
}

type ingestedDealRecord struct {
	SnapshotHash           string    `db:"snapshot_hash"`
	DealSnapshot           []byte    `db:"deal_snapshot"`
	LastSnapshotTimestamp  time.Time `db:"last_snapshot_timestamp"`
}

func (s *dealIngestionService) getIngestedDealRecordForUpdate(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	dealID string,
) (*ingestedDealRecord, error) {
	var row ingestedDealRecord
	err := tx.GetContext(ctx, &row, `
		SELECT snapshot_hash, deal_snapshot, last_snapshot_timestamp
		FROM ingested_deals
		WHERE tenant_id = $1::uuid
		  AND deal_id = $2
		FOR UPDATE
	`, tenantID, dealID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (s *dealIngestionService) insertIngestedDealRecord(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	dealID string,
	hash string,
	snapshot json.RawMessage,
	snapshotTimestamp time.Time,
	now time.Time,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO ingested_deals (
			tenant_id,
			deal_id,
			snapshot_hash,
			deal_snapshot,
			first_seen_at,
			last_seen_at,
			last_snapshot_timestamp
		) VALUES (
			$1::uuid,
			$2,
			$3,
			$4::jsonb,
			$5,
			$5,
			$6
		)
	`, tenantID, dealID, hash, snapshot, now, snapshotTimestamp)
	return err
}

func (s *dealIngestionService) touchIngestedDealRecord(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	dealID string,
	now time.Time,
) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE ingested_deals
		SET last_seen_at = $3,
		    updated_at = $3
		WHERE tenant_id = $1::uuid
		  AND deal_id = $2
	`, tenantID, dealID, now)
	return err
}

func (s *dealIngestionService) updateIngestedDealRecord(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	dealID string,
	hash string,
	snapshot json.RawMessage,
	snapshotTimestamp time.Time,
	now time.Time,
) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE ingested_deals
		SET snapshot_hash = $3,
		    deal_snapshot = $4::jsonb,
		    last_seen_at = $5,
		    last_snapshot_timestamp = $6,
		    updated_at = $5
		WHERE tenant_id = $1::uuid
		  AND deal_id = $2
	`, tenantID, dealID, hash, snapshot, now, snapshotTimestamp)
	return err
}

func (s *dealIngestionService) insertIngestionHistory(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	dealID string,
	req IngestDealRequest,
	resp IngestResponse,
	snapshotTimestamp time.Time,
) error {
	warningsJSON, _ := json.Marshal(resp.Warnings)
	var diffJSON []byte
	if resp.Diff != nil {
		diffJSON, _ = json.Marshal(resp.Diff)
	}
	var exceptionJSON []byte
	if resp.ExceptionDetail != nil {
		exceptionJSON, _ = json.Marshal(resp.ExceptionDetail)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO deal_ingestion_history (
			ingest_id,
			tenant_id,
			deal_id,
			snapshot_timestamp,
			received_at,
			processed_at,
			snapshot_hash,
			outcome,
			case_action,
			case_id,
			diff_summary,
			warnings,
			source_system,
			correlation_id,
			exception_detail
		) VALUES (
			$1::uuid,
			$2::uuid,
			$3,
			$4,
			now(),
			$5,
			$6,
			$7,
			$8,
			$9::uuid,
			$10::jsonb,
			$11::jsonb,
			$12,
			$13,
			$14::jsonb
		)
	`, resp.IngestID, tenantID, dealID, snapshotTimestamp, resp.ProcessedAt, resp.SnapshotHash, string(resp.Outcome), string(resp.CaseAction), nullableUUID(resp.CaseID), diffJSON, warningsJSON, req.SourceSystem, req.CorrelationID, exceptionJSON)
	return err
}

func nullableUUID(v *string) interface{} {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	return *v
}

func (s *dealIngestionService) getCachedIdempotentResponse(
	ctx context.Context,
	tenantID string,
	idempotencyKey string,
) (int, IngestResponse, bool, error) {
	type row struct {
		HTTPStatus      int             `db:"http_status"`
		ResponsePayload json.RawMessage `db:"response_payload"`
	}
	var cache row
	err := s.db.GetContext(ctx, &cache, `
		SELECT http_status, response_payload
		FROM deal_ingest_idempotency_cache
		WHERE tenant_id = $1::uuid
		  AND idempotency_key = $2
		  AND expires_at > now()
	`, tenantID, idempotencyKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, IngestResponse{}, false, nil
		}
		return 0, IngestResponse{}, false, err
	}
	var response IngestResponse
	if err := json.Unmarshal(cache.ResponsePayload, &response); err != nil {
		return 0, IngestResponse{}, false, err
	}
	return cache.HTTPStatus, response, true, nil
}

func (s *dealIngestionService) upsertIdempotencyCache(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	idempotencyKey string,
	httpStatus int,
	response IngestResponse,
) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO deal_ingest_idempotency_cache (
			tenant_id,
			idempotency_key,
			http_status,
			response_payload,
			expires_at
		) VALUES (
			$1::uuid,
			$2,
			$3,
			$4::jsonb,
			now() + interval '24 hours'
		)
		ON CONFLICT (tenant_id, idempotency_key)
		DO UPDATE SET
			http_status = EXCLUDED.http_status,
			response_payload = EXCLUDED.response_payload,
			expires_at = EXCLUDED.expires_at,
			created_at = now()
	`, tenantID, idempotencyKey, httpStatus, payload)
	return err
}

func (s *dealIngestionService) tryAcquireDealLock(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	dealID string,
) (bool, error) {
	var locked bool
	if err := tx.GetContext(ctx, &locked, `
		SELECT pg_try_advisory_xact_lock(hashtext($1))
	`, tenantID+":"+dealID); err != nil {
		return false, err
	}
	return locked, nil
}

var errDuplicateOpenCases = errors.New("multiple open cases exist for deal")

type linkedCase struct {
	CaseID string `db:"case_id"`
	Status string `db:"status"`
}

func (s *dealIngestionService) getOpenCasesByDealID(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	dealID string,
) ([]linkedCase, error) {
	rows := make([]linkedCase, 0)
	err := tx.SelectContext(ctx, &rows, `
		SELECT c.id::text AS case_id, c.status
		FROM case_deal_links l
		JOIN cases c ON c.id = l.case_id
		WHERE l.tenant_id = $1::uuid
		  AND l.deal_id = $2
		  AND c.status IN ($3, $4, $5, $6)
		ORDER BY c.created_at DESC
	`, tenantID, dealID, caseStatusOpen, caseStatusInProgress, caseStatusPendingInformation, caseStatusVerified)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *dealIngestionService) getLatestCaseByDealID(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	dealID string,
) (*linkedCase, error) {
	var row linkedCase
	err := tx.GetContext(ctx, &row, `
		SELECT c.id::text AS case_id, c.status
		FROM case_deal_links l
		JOIN cases c ON c.id = l.case_id
		WHERE l.tenant_id = $1::uuid
		  AND l.deal_id = $2
		ORDER BY c.created_at DESC
		LIMIT 1
	`, tenantID, dealID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func isClosedLikeStatus(status string) bool {
	status = strings.ToUpper(strings.TrimSpace(status))
	return status == "CLOSED" || status == caseStatusCompleted
}

func (s *dealIngestionService) determineAndApplyCaseAction(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	deal BusinessLendingDeal,
	rawSnapshot json.RawMessage,
	snapshotHash string,
	outcome DealIngestOutcome,
	diff *DealDiff,
	now time.Time,
) (DealCaseAction, string, *IngestExceptionDetail, error) {
	openCases, err := s.getOpenCasesByDealID(ctx, tx, tenantID, deal.DealID)
	if err != nil {
		return DealCaseActionNone, "", nil, err
	}
	if len(openCases) > 1 {
		return DealCaseActionNone, "", nil, fmt.Errorf("%w: dealId=%s", errDuplicateOpenCases, deal.DealID)
	}

	incomingStatus := strings.ToUpper(strings.TrimSpace(deal.Status))

	if incomingStatus == documentVerificationCaseType {
		if len(openCases) == 0 {
			latestCase, err := s.getLatestCaseByDealID(ctx, tx, tenantID, deal.DealID)
			if err != nil {
				return DealCaseActionNone, "", nil, err
			}

			if latestCase == nil {
				caseID, err := s.createDocumentVerificationCase(ctx, tx, tenantID, deal, rawSnapshot, snapshotHash, now)
				return DealCaseActionCreated, caseID, nil, err
			}

			latestStatus := strings.ToUpper(strings.TrimSpace(latestCase.Status))
			if isClosedLikeStatus(latestStatus) {
				caseID, err := s.createDocumentVerificationCase(ctx, tx, tenantID, deal, rawSnapshot, snapshotHash, now)
				return DealCaseActionClosedCaseRecreated, caseID, nil, err
			}
			if latestStatus == caseStatusCancelled {
				caseID, err := s.createDocumentVerificationCase(ctx, tx, tenantID, deal, rawSnapshot, snapshotHash, now)
				return DealCaseActionCreated, caseID, nil, err
			}

			return DealCaseActionNone, "", &IngestExceptionDetail{
				Code:            "UNHANDLED_SCENARIO",
				Description:     fmt.Sprintf("latest case status %s is not covered for re-open decision", latestCase.Status),
				SuggestedAction: "Review case lifecycle state and decide whether a new document verification case should be opened.",
			}, nil
		}

		openCase := openCases[0]
		if outcome == DealIngestOutcomeUpdated && diff != nil && diff.IsMaterial {
			if err := s.updateCaseSnapshot(ctx, tx, tenantID, openCase.CaseID, deal, rawSnapshot, snapshotHash, diff, true, now); err != nil {
				return DealCaseActionNone, "", nil, err
			}
			if err := s.addTasksForMaterialAdditions(ctx, tx, tenantID, openCase.CaseID, deal, diff, now); err != nil {
				return DealCaseActionNone, "", nil, err
			}
			return DealCaseActionDealChangeFlagged, openCase.CaseID, nil, nil
		}

		if outcome == DealIngestOutcomeUpdated {
			if err := s.updateCaseSnapshot(ctx, tx, tenantID, openCase.CaseID, deal, rawSnapshot, snapshotHash, nil, false, now); err != nil {
				return DealCaseActionNone, "", nil, err
			}
			return DealCaseActionSnapshotRefreshed, openCase.CaseID, nil, nil
		}

		return DealCaseActionNone, openCase.CaseID, nil, nil
	}

	if len(openCases) > 0 {
		openCase := openCases[0]
		if incomingStatus == "DECLINED" || incomingStatus == "CLOSED" {
			if err := s.cancelCaseForDealStatus(ctx, tx, tenantID, openCase.CaseID, incomingStatus, now); err != nil {
				return DealCaseActionNone, "", nil, err
			}
			return DealCaseActionCancelledDueToStatusTransition, openCase.CaseID, nil, nil
		}

		if err := s.updateCaseSnapshot(ctx, tx, tenantID, openCase.CaseID, deal, rawSnapshot, snapshotHash, nil, false, now); err != nil {
			return DealCaseActionNone, "", nil, err
		}
		return DealCaseActionSnapshotRefreshed, openCase.CaseID, nil, nil
	}

	return DealCaseActionNone, "", nil, nil
}

func (s *dealIngestionService) createDocumentVerificationCase(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	deal BusinessLendingDeal,
	rawSnapshot json.RawMessage,
	snapshotHash string,
	now time.Time,
) (string, error) {
	type caseTypeRow struct {
		CaseTypeID string `db:"case_type_id"`
		Version    int    `db:"version"`
	}
	var caseType caseTypeRow
	if err := tx.GetContext(ctx, &caseType, `
		SELECT id::text AS case_type_id, version
		FROM case_types
		WHERE code = $1
		  AND status = 'ACTIVE'
		  AND (tenant_id IS NULL OR tenant_id = $2::uuid)
		ORDER BY CASE WHEN tenant_id IS NULL THEN 1 ELSE 0 END, version DESC
		LIMIT 1
	`, documentVerificationCaseType, tenantID); err != nil {
		return "", fmt.Errorf("resolve DOCUMENT_VERIFICATION case type: %w", err)
	}

	priority := determineCasePriority(deal.UmbrellaLimit.ApprovedLimit.Amount)
	targetDate := addBusinessDays(now, businessDaysForPriority(priority))
	warningDate := subtractBusinessDays(targetDate, 1)

	metadata := map[string]interface{}{
		"deal_id":             deal.DealID,
		"deal_name":           deal.DealName,
		"customer_reference":  deal.CustomerReference,
		"trigger_deal_status": documentVerificationCaseType,
		"case_priority":       priority,
		"sla": map[string]string{
			"targetCompletionDate":   targetDate.Format("2006-01-02"),
			"warningThresholdDate":   warningDate.Format("2006-01-02"),
		},
	}
	metadataJSON, _ := json.Marshal(metadata)

	var caseID string
	if err := tx.GetContext(ctx, &caseID, `
		INSERT INTO cases (
			tenant_id,
			case_type_id,
			case_type_version,
			case_type_version_id,
			current_stage_code,
			current_stage_ordinal,
			status,
			metadata
		) VALUES (
			$1::uuid,
			$2::uuid,
			$3,
			$2::uuid,
			$4,
			1,
			$5,
			$6::jsonb
		)
		RETURNING id::text
	`, tenantID, caseType.CaseTypeID, caseType.Version, documentVerificationStageCode, caseStatusOpen, metadataJSON); err != nil {
		return "", fmt.Errorf("insert case: %w", err)
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO case_deal_links (
			case_id,
			tenant_id,
			deal_id,
			deal_name,
			customer_reference,
			trigger_deal_status,
			current_deal_status,
			umbrella_limit_amount,
			umbrella_limit_currency,
			deal_snapshot,
			deal_snapshot_hash,
			last_snapshot_refreshed_at,
			case_priority,
			sla_target_completion_date,
			sla_warning_threshold_date,
			notify_assignee
		) VALUES (
			$1::uuid,
			$2::uuid,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10::jsonb,
			$11,
			$12,
			$13,
			$14,
			$15,
			FALSE
		)
	`, caseID, tenantID, deal.DealID, deal.DealName, deal.CustomerReference, documentVerificationCaseType, strings.ToUpper(strings.TrimSpace(deal.Status)), deal.UmbrellaLimit.ApprovedLimit.Amount, deal.UmbrellaLimit.ApprovedLimit.Currency, rawSnapshot, snapshotHash, now, priority, targetDate.Format("2006-01-02"), warningDate.Format("2006-01-02"))
	if err != nil {
		return "", fmt.Errorf("insert case_deal_link: %w", err)
	}

	if err := s.appendCaseTimelineEvent(ctx, tx, caseID, "CASE_CREATED", map[string]interface{}{
		"deal_id":   deal.DealID,
		"deal_name": deal.DealName,
		"priority":  priority,
	}); err != nil {
		return "", err
	}

	tasks := generateTasksForDeal(deal)
	for _, task := range tasks {
		if err := s.insertGeneratedTask(ctx, tx, tenantID, caseID, priority, task, now); err != nil {
			return "", err
		}
	}

	return caseID, nil
}

func (s *dealIngestionService) updateCaseSnapshot(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	caseID string,
	deal BusinessLendingDeal,
	rawSnapshot json.RawMessage,
	snapshotHash string,
	diff *DealDiff,
	notifyAssignee bool,
	now time.Time,
) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE case_deal_links
		SET deal_name = $3,
		    customer_reference = $4,
		    current_deal_status = $5,
		    umbrella_limit_amount = $6,
		    umbrella_limit_currency = $7,
		    deal_snapshot = $8::jsonb,
		    deal_snapshot_hash = $9,
		    last_snapshot_refreshed_at = $10,
		    notify_assignee = $11,
		    updated_at = $10
		WHERE case_id = $1::uuid
		  AND tenant_id = $2::uuid
	`, caseID, tenantID, deal.DealName, deal.CustomerReference, strings.ToUpper(strings.TrimSpace(deal.Status)), deal.UmbrellaLimit.ApprovedLimit.Amount, deal.UmbrellaLimit.ApprovedLimit.Currency, rawSnapshot, snapshotHash, now, notifyAssignee)
	if err != nil {
		return fmt.Errorf("update case snapshot: %w", err)
	}

	if diff != nil {
		if err := s.appendCaseTimelineEvent(ctx, tx, caseID, "DEAL_MATERIAL_CHANGE_DETECTED", map[string]interface{}{
			"diff": diff,
		}); err != nil {
			return err
		}
	}

	summary := "Non-material snapshot refresh"
	if diff != nil {
		summary = "Material snapshot refresh"
	}
	if err := s.appendCaseTimelineEvent(ctx, tx, caseID, "DEAL_SNAPSHOT_UPDATED", map[string]interface{}{
		"summary": summary,
	}); err != nil {
		return err
	}

	return nil
}

func (s *dealIngestionService) addTasksForMaterialAdditions(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	caseID string,
	deal BusinessLendingDeal,
	diff *DealDiff,
	now time.Time,
) error {
	if diff == nil {
		return nil
	}

	priority := determineCasePriority(deal.UmbrellaLimit.ApprovedLimit.Amount)
	tasks := generateTasksForMaterialAdditions(deal, diff)
	for _, task := range tasks {
		if err := s.insertGeneratedTask(ctx, tx, tenantID, caseID, priority, task, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *dealIngestionService) insertGeneratedTask(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	caseID string,
	casePriority string,
	task GeneratedDealTask,
	now time.Time,
) error {
	inputPayload, _ := json.Marshal(map[string]interface{}{
		"taskType":   task.TaskType,
		"title":      task.Title,
		"contextRef": task.ContextRef,
	})
	metadata, _ := json.Marshal(map[string]interface{}{
		"taskType":   task.TaskType,
		"title":      task.Title,
		"mandatory":  task.Mandatory,
		"origin":     task.Origin,
		"contextRef": task.ContextRef,
		"generatedAt": now.Format(time.RFC3339Nano),
	})

	taskPriority := mapCasePriorityToTaskPriority(casePriority)
	idempotencyKey := fmt.Sprintf("deal-task:%s:%s:%s", caseID, task.TaskType, strings.TrimSpace(task.ContextRef.EntityID))

	_, err := tx.ExecContext(ctx, `
		INSERT INTO tasks (
			tenant_id,
			case_id,
			task_definition_code,
			activity_code,
			stage_code,
			status,
			priority,
			max_retries,
			input_payload,
			output_payload,
			metadata,
			idempotency_key,
			is_document_verification
		) VALUES (
			$1::uuid,
			$2::uuid,
			$3,
			$4,
			$5,
			$6,
			$7,
			3,
			$8::jsonb,
			'{}'::jsonb,
			$9::jsonb,
			$10,
			TRUE
		)
		ON CONFLICT (idempotency_key) DO NOTHING
	`, tenantID, caseID, task.TaskType, documentVerificationActivity, documentVerificationStageCode, string(model.TaskStatusPending), int(taskPriority), inputPayload, metadata, idempotencyKey)
	if err != nil {
		return fmt.Errorf("insert generated task %s: %w", task.TaskType, err)
	}
	return nil
}

func (s *dealIngestionService) cancelCaseForDealStatus(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	caseID string,
	incomingDealStatus string,
	now time.Time,
) error {
	var previousStatus string
	if err := tx.GetContext(ctx, &previousStatus, `
		SELECT status
		FROM cases
		WHERE id = $1::uuid
	`, caseID); err != nil {
		return fmt.Errorf("load existing case status for cancel: %w", err)
	}

	reason := fmt.Sprintf("Deal status changed to %s", incomingDealStatus)
	_, err := tx.ExecContext(ctx, `
		UPDATE cases
		SET status = $2,
		    withdrawal_reason = $3,
		    updated_at = $4,
		    row_version = row_version + 1
		WHERE id = $1::uuid
	`, caseID, caseStatusCancelled, reason, now)
	if err != nil {
		return fmt.Errorf("cancel case: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE case_deal_links
		SET current_deal_status = $3,
		    last_snapshot_refreshed_at = $4,
		    updated_at = $4
		WHERE case_id = $1::uuid
		  AND tenant_id = $2::uuid
	`, caseID, tenantID, strings.ToUpper(strings.TrimSpace(incomingDealStatus)), now)
	if err != nil {
		return fmt.Errorf("update case_deal_links after cancel: %w", err)
	}

	if err := s.appendCaseTimelineEvent(ctx, tx, caseID, "STATUS_TRANSITIONED", map[string]interface{}{
		"from":   previousStatus,
		"to":     caseStatusCancelled,
		"reason": reason,
	}); err != nil {
		return err
	}
	return nil
}

func (s *dealIngestionService) appendCaseTimelineEvent(
	ctx context.Context,
	tx *sqlx.Tx,
	caseID string,
	action string,
	detail interface{},
) error {
	changeDelta, _ := json.Marshal(detail)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO audit_trail (
			case_id,
			action,
			entity_type,
			entity_id,
			actor_id,
			actor_type,
			change_delta,
			metadata
		) VALUES (
			$1::uuid,
			$2,
			$3,
			NULL,
			$4,
			$5,
			$6::jsonb,
			'{}'::jsonb
		)
	`, caseID, action, string(model.AuditEntityCase), auditActorIngestionService, string(model.AuditActorAPI), changeDelta)
	if err != nil {
		return fmt.Errorf("append timeline event %s: %w", action, err)
	}
	return nil
}

func parseSnapshotTimestamp(raw string) (time.Time, *ProblemDetail) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, &ProblemDetail{
			Title:  "Snapshot Schema Validation Failure",
			Status: 422,
			Detail: "snapshotTimestamp is required",
		}
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, &ProblemDetail{
			Title:  "Snapshot Schema Validation Failure",
			Status: 422,
			Detail: "snapshotTimestamp must be a valid ISO 8601 datetime",
		}
	}
	_, offset := ts.Zone()
	if offset != 0 {
		return time.Time{}, &ProblemDetail{
			Title:  "Snapshot Schema Validation Failure",
			Status: 422,
			Detail: "snapshotTimestamp must be in UTC",
		}
	}
	return ts.UTC(), nil
}

func validateSnapshotWarnings(deal BusinessLendingDeal) []string {
	warnings := make([]string, 0)

	approved := deal.UmbrellaLimit.ApprovedLimit.Amount
	totalConsumption := 0.0
	for _, entity := range deal.BorrowingEntities {
		for _, facility := range entity.Facilities {
			totalConsumption += facility.UmbrellaConsumption.ConsumptionAmount.Amount
		}
	}
	if totalConsumption > approved {
		warnings = append(warnings,
			fmt.Sprintf("umbrella approved limit %.2f is below total facility consumption %.2f", approved, totalConsumption),
		)
	}

	trustByID := make(map[string]struct{}, len(deal.TrustStructures))
	for _, trust := range deal.TrustStructures {
		if strings.TrimSpace(trust.TrustID) != "" {
			trustByID[trust.TrustID] = struct{}{}
		}
	}

	for _, entity := range deal.BorrowingEntities {
		if strings.ToUpper(strings.TrimSpace(entity.BorrowingEntityType)) != "TRUST" {
			continue
		}
		if _, ok := trustByID[strings.TrimSpace(entity.TrustID)]; !ok {
			warnings = append(warnings,
				fmt.Sprintf("trust borrowingEntity %s references unknown trustId %s", entity.BorrowingEntityID, entity.TrustID),
			)
		}
		hasTrusteeObligor := false
		for _, obligor := range entity.Obligors {
			if strings.ToUpper(strings.TrimSpace(obligor.Capacity)) == "AS_TRUSTEE" {
				hasTrusteeObligor = true
				break
			}
		}
		if !hasTrusteeObligor {
			warnings = append(warnings,
				fmt.Sprintf("trust borrowingEntity %s has no obligor with capacity AS_TRUSTEE", entity.BorrowingEntityID),
			)
		}
	}

	return warnings
}

func computeSnapshotHash(snapshot json.RawMessage) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(snapshot))
	dec.UseNumber()
	var parsed interface{}
	if err := dec.Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode snapshot: %w", err)
	}
	canonical, err := canonicalizeJSON(parsed)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalizeJSON(v interface{}) ([]byte, error) {
	switch value := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(value))
		for k := range value {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf := &bytes.Buffer{}
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyBytes, _ := json.Marshal(k)
			buf.Write(keyBytes)
			buf.WriteByte(':')
			child, err := canonicalizeJSON(value[k])
			if err != nil {
				return nil, err
			}
			buf.Write(child)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []interface{}:
		buf := &bytes.Buffer{}
		buf.WriteByte('[')
		for i, entry := range value {
			if i > 0 {
				buf.WriteByte(',')
			}
			child, err := canonicalizeJSON(entry)
			if err != nil {
				return nil, err
			}
			buf.Write(child)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	case json.Number:
		return []byte(value.String()), nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return encoded, nil
	}
}

func mustUUID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("ingest-%d", time.Now().UTC().UnixNano())
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		raw[0:4],
		raw[4:6],
		raw[6:8],
		raw[8:10],
		raw[10:16],
	)
}

func determineCasePriority(approvedAmount float64) string {
	if approvedAmount < 1_000_000 {
		return "NORMAL"
	}
	if approvedAmount <= 5_000_000 {
		return "HIGH"
	}
	return "URGENT"
}

func mapCasePriorityToTaskPriority(priority string) model.TaskPriority {
	switch strings.ToUpper(strings.TrimSpace(priority)) {
	case "URGENT":
		return model.TaskPriorityCritical
	case "HIGH":
		return model.TaskPriorityHigh
	case "LOW":
		return model.TaskPriorityLow
	default:
		return model.TaskPriorityNormal
	}
}

func businessDaysForPriority(priority string) int {
	switch strings.ToUpper(strings.TrimSpace(priority)) {
	case "URGENT":
		return 1
	case "HIGH":
		return 3
	default:
		return 5
	}
}

func addBusinessDays(start time.Time, days int) time.Time {
	if days <= 0 {
		return start
	}
	cursor := start.UTC()
	added := 0
	for added < days {
		cursor = cursor.AddDate(0, 0, 1)
		if cursor.Weekday() == time.Saturday || cursor.Weekday() == time.Sunday {
			continue
		}
		added++
	}
	return cursor
}

func subtractBusinessDays(start time.Time, days int) time.Time {
	if days <= 0 {
		return start
	}
	cursor := start.UTC()
	removed := 0
	for removed < days {
		cursor = cursor.AddDate(0, 0, -1)
		if cursor.Weekday() == time.Saturday || cursor.Weekday() == time.Sunday {
			continue
		}
		removed++
	}
	return cursor
}

func httpStatusProblem(status int, title string, detail string) (int, *IngestResponse, *ProblemDetail) {
	return status, nil, &ProblemDetail{Title: title, Status: status, Detail: detail}
}

func isAllowedSourceSystem(sourceSystem string) bool {
	switch strings.ToUpper(strings.TrimSpace(sourceSystem)) {
	case "ORIGINATION", "MANUAL_OVERRIDE", "MIGRATION":
		return true
	default:
		return false
	}
}

func validateBusinessLendingDealRequiredFields(deal BusinessLendingDeal) *ProblemDetail {
	if strings.TrimSpace(deal.DealID) == "" {
		return &ProblemDetail{
			Title:  "Deal Identifier Missing",
			Status: 422,
			Detail: "snapshot.dealId is required",
		}
	}
	if strings.TrimSpace(deal.DealName) == "" {
		return &ProblemDetail{
			Title:  "Snapshot Schema Validation Failure",
			Status: 422,
			Detail: "snapshot.dealName is required",
		}
	}
	if strings.TrimSpace(deal.Status) == "" {
		return &ProblemDetail{
			Title:  "Snapshot Schema Validation Failure",
			Status: 422,
			Detail: "snapshot.status is required",
		}
	}
	if strings.TrimSpace(deal.UmbrellaLimit.ApprovedLimit.Currency) == "" {
		return &ProblemDetail{
			Title:  "Snapshot Schema Validation Failure",
			Status: 422,
			Detail: "snapshot.umbrellaLimit.approvedLimit.currency is required",
		}
	}
	for i, entity := range deal.BorrowingEntities {
		if strings.TrimSpace(entity.BorrowingEntityID) == "" {
			return &ProblemDetail{
				Title:  "Snapshot Schema Validation Failure",
				Status: 422,
				Detail: fmt.Sprintf("snapshot.borrowingEntities[%d].borrowingEntityId is required", i),
			}
		}
		for j, facility := range entity.Facilities {
			if strings.TrimSpace(facility.FacilityID) == "" {
				return &ProblemDetail{
					Title:  "Snapshot Schema Validation Failure",
					Status: 422,
					Detail: fmt.Sprintf("snapshot.borrowingEntities[%d].facilities[%d].facilityId is required", i, j),
				}
			}
		}
	}
	return nil
}
