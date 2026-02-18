package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/internal/sla"
	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

type ApprovalExpirySweepJob struct {
	db             *sqlx.DB
	eventPublisher EventPublisher
	evaluator      *ApprovalPolicyEvaluator
	sweepInterval  time.Duration
	batchSize      int
	logger         *slog.Logger
}

func NewApprovalExpirySweepJob(
	db *sqlx.DB,
	publisher EventPublisher,
	evaluator *ApprovalPolicyEvaluator,
	interval time.Duration,
	batchSize int,
	logger *slog.Logger,
) *ApprovalExpirySweepJob {
	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = time.Minute
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	if evaluator == nil {
		evaluator = NewApprovalPolicyEvaluator(db, logger, publisher)
	}
	return &ApprovalExpirySweepJob{
		db:             db,
		eventPublisher: publisher,
		evaluator:      evaluator,
		sweepInterval:  interval,
		batchSize:      batchSize,
		logger:         logger,
	}
}

type expiryCandidate struct {
	RequestID            string                `db:"request_id"`
	ApprovalGateID       string                `db:"approval_gate_id"`
	CaseID               string                `db:"case_id"`
	TaskID               string                `db:"task_id"`
	ApproverID           string                `db:"approver_id"`
	Status               model.ApprovalRequestStatus `db:"status"`
	Tier                 sql.NullInt64         `db:"tier"`
	ExpiresAt            time.Time             `db:"expires_at"`
	TimeoutAction        model.TimeoutAction   `db:"on_timeout_action"`
	ApprovalTimeoutHours float64               `db:"approval_timeout_hours"`
	FallbackRole         sql.NullString        `db:"fallback_supervisor_role"`
	CalendarID           sql.NullString        `db:"calendar_id"`
}

func (j *ApprovalExpirySweepJob) Run(ctx context.Context) error {
	if j.db == nil {
		return fmt.Errorf("ApprovalExpirySweepJob.Run: db is nil")
	}

	tx, err := j.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("ApprovalExpirySweepJob.Run: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var candidates []expiryCandidate
	err = tx.SelectContext(ctx, &candidates, `
		SELECT
			r.id::text AS request_id,
			r.approval_gate_id::text AS approval_gate_id,
			g.case_id::text AS case_id,
			g.task_id::text AS task_id,
			r.approver_id,
			r.status,
			r.tier,
			r.expires_at,
			g.on_timeout_action,
			g.approval_timeout_hours,
			g.fallback_supervisor_role,
			c.case_sla_calendar_id::text AS calendar_id
		FROM approval_requests r
		JOIN approval_gates g ON g.id = r.approval_gate_id
		JOIN cases c ON c.id = g.case_id
		WHERE r.status = 'PENDING'
		  AND r.expires_at < now()
		ORDER BY r.expires_at ASC
		LIMIT $1
		FOR UPDATE OF r SKIP LOCKED
	`, j.batchSize)
	if err != nil {
		return fmt.Errorf("ApprovalExpirySweepJob.Run: query candidates: %w", err)
	}

	now := time.Now().UTC()
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := j.processCandidate(ctx, tx, now, candidate); err != nil {
			j.logger.Error("approval expiry candidate failed", "request_id", candidate.RequestID, "error", err)
			continue
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ApprovalExpirySweepJob.Run: commit: %w", err)
	}

	j.logger.Info("approval expiry sweep completed", "candidates", len(candidates), "batch_size", j.batchSize)
	return nil
}

func (j *ApprovalExpirySweepJob) processCandidate(ctx context.Context, tx *sqlx.Tx, now time.Time, c expiryCandidate) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE approval_requests
		SET status = 'EXPIRED',
			decided_at = $2,
			decided_by = 'SYSTEM',
			updated_at = now(),
			version = version + 1
		WHERE id = $1::uuid
		  AND status = 'PENDING'
	`, c.RequestID, now)
	if err != nil {
		return fmt.Errorf("processCandidate: mark EXPIRED: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("processCandidate: rows affected: %w", err)
	}
	if affected == 0 {
		return nil
	}

	if err := insertApprovalAuditLog(ctx, tx, approvalAuditInput{
		RequestID:     c.RequestID,
		EventType:     model.ApprovalAuditEventExpired,
		ActorID:       "SYSTEM",
		PreviousState: model.ApprovalRequestStatusPending,
		NewState:      model.ApprovalRequestStatusExpired,
	}); err != nil {
		return fmt.Errorf("processCandidate: audit expired: %w", err)
	}

	timeoutActionTaken := c.TimeoutAction
	switch c.TimeoutAction {
	case model.TimeoutActionAutoApprove:
		if err := j.autoApprove(ctx, tx, now, c); err != nil {
			return fmt.Errorf("processCandidate: auto approve: %w", err)
		}

	case model.TimeoutActionAutoReject:
		if err := j.autoReject(ctx, tx, now, c); err != nil {
			return fmt.Errorf("processCandidate: auto reject: %w", err)
		}

	case model.TimeoutActionEscalate:
		if err := j.escalate(ctx, tx, now, c); err != nil {
			return fmt.Errorf("processCandidate: escalate: %w", err)
		}

	default:
		return fmt.Errorf("processCandidate: unsupported timeout action %s", c.TimeoutAction)
	}

	payload, err := json.Marshal(ApprovalEventPayload{
		GateID:        c.ApprovalGateID,
		RequestID:     c.RequestID,
		CaseID:        c.CaseID,
		TaskID:        c.TaskID,
		ApproverID:    c.ApproverID,
		RequestStatus: model.ApprovalRequestStatusExpired,
		TimeoutAction: timeoutActionTaken,
		Reason:        "approval_request_expired",
	})
	if err != nil {
		return fmt.Errorf("processCandidate: marshal event payload: %w", err)
	}
	caseID := c.CaseID
	taskID := c.TaskID
	if err := j.eventPublisher.PublishEvent(ctx, tx, model.Event{
		CaseID:    &caseID,
		TaskID:    &taskID,
		EventType: model.EventApprovalExpired,
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("processCandidate: publish APPROVAL_EXPIRED: %w", err)
	}

	if _, err := j.evaluator.EvaluateApprovalPolicy(ctx, tx, c.ApprovalGateID); err != nil {
		return fmt.Errorf("processCandidate: evaluate policy: %w", err)
	}

	return nil
}

func (j *ApprovalExpirySweepJob) autoApprove(ctx context.Context, tx *sqlx.Tx, now time.Time, c expiryCandidate) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE approval_requests
		SET status = 'APPROVED',
			decision = COALESCE(decision, 'AUTO_APPROVED: timeout reached'),
			decided_at = $2,
			decided_by = 'SYSTEM_AUTO_APPROVE',
			updated_at = now(),
			version = version + 1
		WHERE id = $1::uuid
		  AND status = 'EXPIRED'
	`, c.RequestID, now)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return nil
	}

	if err := insertApprovalAuditLog(ctx, tx, approvalAuditInput{
		RequestID:     c.RequestID,
		EventType:     model.ApprovalAuditEventAutoApproved,
		ActorID:       "SYSTEM_AUTO_APPROVE",
		DecisionText:  strPtr("AUTO_APPROVED: timeout reached"),
		PreviousState: model.ApprovalRequestStatusExpired,
		NewState:      model.ApprovalRequestStatusApproved,
	}); err != nil {
		return err
	}

	return nil
}

func (j *ApprovalExpirySweepJob) autoReject(ctx context.Context, tx *sqlx.Tx, now time.Time, c expiryCandidate) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE approval_requests
		SET status = 'REJECTED',
			decision = COALESCE(decision, 'AUTO_REJECTED: timeout reached'),
			decided_at = $2,
			decided_by = 'SYSTEM_AUTO_REJECT',
			updated_at = now(),
			version = version + 1
		WHERE id = $1::uuid
		  AND status = 'EXPIRED'
	`, c.RequestID, now)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return nil
	}

	if err := insertApprovalAuditLog(ctx, tx, approvalAuditInput{
		RequestID:     c.RequestID,
		EventType:     model.ApprovalAuditEventAutoRejected,
		ActorID:       "SYSTEM_AUTO_REJECT",
		DecisionText:  strPtr("AUTO_REJECTED: timeout reached"),
		PreviousState: model.ApprovalRequestStatusExpired,
		NewState:      model.ApprovalRequestStatusRejected,
	}); err != nil {
		return err
	}

	return nil
}

func (j *ApprovalExpirySweepJob) escalate(ctx context.Context, tx *sqlx.Tx, now time.Time, c expiryCandidate) error {
	escalatedTo, err := resolveEscalationTarget(ctx, tx, c.ApproverID, c.FallbackRole)
	if err != nil {
		return err
	}
	if escalatedTo == "" {
		return fmt.Errorf("escalate: no escalation target found")
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE approval_requests
		SET status = 'DELEGATED',
			delegated_to_id = $2,
			delegated_at = $3,
			updated_at = now(),
			version = version + 1
		WHERE id = $1::uuid
		  AND status = 'EXPIRED'
	`, c.RequestID, escalatedTo, now)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return nil
	}

	newExpiry, err := computeEscalatedExpiry(ctx, j.db, now, c.ApprovalTimeoutHours, c.CalendarID)
	if err != nil {
		return err
	}

	var tier interface{}
	if c.Tier.Valid {
		tier = int(c.Tier.Int64)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO approval_requests (
			approval_gate_id,
			approver_id,
			tier,
			status,
			evidence_refs,
			expires_at,
			delegation_chain
		)
		VALUES (
			$1::uuid,
			$2,
			$3,
			'PENDING',
			'[]'::jsonb,
			$4,
			jsonb_build_array(jsonb_build_object('from', $5, 'to', $2, 'at', $6))
		)
	`, c.ApprovalGateID, escalatedTo, tier, newExpiry, c.ApproverID, now)
	if err != nil {
		return err
	}

	if err := insertApprovalAuditLog(ctx, tx, approvalAuditInput{
		RequestID:     c.RequestID,
		EventType:     model.ApprovalAuditEventEscalated,
		ActorID:       "SYSTEM",
		DecisionText:  strPtr("ESCALATED: timeout reached"),
		PreviousState: model.ApprovalRequestStatusExpired,
		NewState:      model.ApprovalRequestStatusDelegated,
	}); err != nil {
		return err
	}

	return nil
}

func computeEscalatedExpiry(ctx context.Context, db *sqlx.DB, start time.Time, timeoutHours float64, calendarID sql.NullString) (time.Time, error) {
	duration := time.Duration(timeoutHours * float64(time.Hour))
	if duration <= 0 {
		duration = time.Hour
	}
	if db != nil && calendarID.Valid && calendarID.String != "" {
		dueAt, err := sla.AddBusinessHours(ctx, db, start, duration, calendarID.String)
		if err != nil {
			return time.Time{}, fmt.Errorf("computeEscalatedExpiry: %w", err)
		}
		return dueAt, nil
	}
	return start.Add(duration).UTC(), nil
}

func resolveEscalationTarget(ctx context.Context, tx *sqlx.Tx, approverID string, fallbackRole sql.NullString) (string, error) {
	var managerID sql.NullString
	err := tx.GetContext(ctx, &managerID, `
		SELECT manager_id
		FROM users
		WHERE id = $1
	`, approverID)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	if managerID.Valid && managerID.String != "" {
		return managerID.String, nil
	}

	if fallbackRole.Valid && fallbackRole.String != "" {
		var fallbackID string
		err := tx.GetContext(ctx, &fallbackID, `
			SELECT id
			FROM users
			WHERE role_code = $1
			  AND status = 'ACTIVE'
			ORDER BY created_at ASC
			LIMIT 1
		`, fallbackRole.String)
		if err == nil {
			return fallbackID, nil
		}
		if err != sql.ErrNoRows {
			return "", err
		}
	}

	return "", nil
}

func (j *ApprovalExpirySweepJob) Start(ctx context.Context) {
	ticker := time.NewTicker(j.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			j.logger.Info("approval expiry sweep stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			if err := j.Run(ctx); err != nil {
				j.logger.Error("approval expiry sweep failed", "error", err)
			}
		}
	}
}

type approvalAuditInput struct {
	RequestID     string
	EventType     model.ApprovalAuditEventType
	ActorID       string
	DecisionText  *string
	PreviousState model.ApprovalRequestStatus
	NewState      model.ApprovalRequestStatus
}

func insertApprovalAuditLog(ctx context.Context, tx *sqlx.Tx, in approvalAuditInput) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO approval_audit_log (
			approval_request_id,
			event_type,
			actor_id,
			decision_text,
			evidence_refs,
			previous_state,
			new_state
		)
		VALUES (
			$1::uuid,
			$2,
			$3,
			$4,
			'[]'::jsonb,
			$5,
			$6
		)
	`, in.RequestID, string(in.EventType), in.ActorID, in.DecisionText, string(in.PreviousState), string(in.NewState))
	if err != nil {
		return fmt.Errorf("insertApprovalAuditLog: %w", err)
	}
	return nil
}

func strPtr(v string) *string {
	return &v
}
