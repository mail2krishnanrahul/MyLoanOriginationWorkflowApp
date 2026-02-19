package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

type CircuitBreaker struct {
	db                *sqlx.DB
	thresholdFailures int
	cooldownDuration  time.Duration
	halfOpenAttempts  int
	logger            *slog.Logger
	publisher         EventPublisher
	nowFunc           func() time.Time
}

func NewCircuitBreaker(
	db *sqlx.DB,
	thresholdFailures int,
	cooldownDuration time.Duration,
	halfOpenAttempts int,
	logger *slog.Logger,
	publisher EventPublisher,
) *CircuitBreaker {
	if thresholdFailures <= 0 {
		thresholdFailures = 10
	}
	if cooldownDuration <= 0 {
		cooldownDuration = 5 * time.Minute
	}
	if halfOpenAttempts <= 0 {
		halfOpenAttempts = 3
	}
	if logger == nil {
		logger = slog.Default()
	}
	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}
	return &CircuitBreaker{
		db:                db,
		thresholdFailures: thresholdFailures,
		cooldownDuration:  cooldownDuration,
		halfOpenAttempts:  halfOpenAttempts,
		logger:            logger,
		publisher:         publisher,
		nowFunc:           func() time.Time { return time.Now().UTC() },
	}
}

type breakerSnapshot struct {
	Channel           model.NotificationChannel  `db:"channel"`
	State             model.CircuitBreakerStateType `db:"state"`
	FailureCount      int                        `db:"failure_count"`
	SuccessCount      int                        `db:"success_count"`
	LastFailureAt     sql.NullTime               `db:"last_failure_at"`
	OpenedAt          sql.NullTime               `db:"opened_at"`
	HalfOpenAt        sql.NullTime               `db:"half_open_at"`
	ThresholdFailures int                        `db:"threshold_failures"`
	CooldownSeconds   int                        `db:"cooldown_seconds"`
}

// CheckState returns the current state for a channel and decides whether to allow a send attempt.
func (cb *CircuitBreaker) CheckState(
	ctx context.Context,
	channel string,
) (allow bool, err error) {
	if cb == nil || cb.db == nil {
		return false, fmt.Errorf("CheckState: circuit breaker db is nil")
	}
	if strings.TrimSpace(channel) == "" {
		return false, fmt.Errorf("CheckState: channel is required")
	}

	tx, err := cb.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, fmt.Errorf("CheckState: begin tx: %w", err)
	}
	defer tx.Rollback()

	snapshot, err := cb.ensureAndLockState(ctx, tx, model.NotificationChannel(strings.ToUpper(strings.TrimSpace(channel))))
	if err != nil {
		return false, fmt.Errorf("CheckState: load state: %w", err)
	}

	now := cb.now()
	allow = true
	switch snapshot.State {
	case model.CircuitBreakerStateOpen:
		openedAt := snapshot.OpenedAt.Time
		if snapshot.OpenedAt.Valid && now.Sub(openedAt) >= time.Duration(snapshot.CooldownSeconds)*time.Second {
			if err := cb.updateStateRow(ctx, tx, snapshot.Channel, model.CircuitBreakerStateHalfOpen, 0, 0, snapshot.LastFailureAt, sql.NullTime{Time: now, Valid: true}, sql.NullTime{Time: now, Valid: true}); err != nil {
				return false, fmt.Errorf("CheckState: transition OPEN->HALF_OPEN: %w", err)
			}
			allow = true
		} else {
			allow = false
		}
	case model.CircuitBreakerStateHalfOpen, model.CircuitBreakerStateClosed:
		allow = true
	default:
		allow = true
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("CheckState: commit: %w", err)
	}
	return allow, nil
}

// RecordSuccess updates the circuit breaker on successful send.
func (cb *CircuitBreaker) RecordSuccess(
	ctx context.Context,
	tx *sqlx.Tx,
	channel string,
) error {
	if cb == nil {
		return fmt.Errorf("RecordSuccess: circuit breaker is nil")
	}
	if tx == nil {
		return fmt.Errorf("RecordSuccess: tx is nil")
	}
	if strings.TrimSpace(channel) == "" {
		return fmt.Errorf("RecordSuccess: channel is required")
	}

	snapshot, err := cb.ensureAndLockState(ctx, tx, model.NotificationChannel(strings.ToUpper(strings.TrimSpace(channel))))
	if err != nil {
		return fmt.Errorf("RecordSuccess: lock state: %w", err)
	}

	now := cb.now()
	switch snapshot.State {
	case model.CircuitBreakerStateHalfOpen:
		successCount := snapshot.SuccessCount + 1
		if successCount >= cb.halfOpenAttempts {
			if err := cb.updateStateRow(ctx, tx, snapshot.Channel, model.CircuitBreakerStateClosed, 0, 0, sql.NullTime{}, sql.NullTime{}, sql.NullTime{}); err != nil {
				return fmt.Errorf("RecordSuccess: close half-open breaker: %w", err)
			}
			return nil
		}
		if err := cb.updateStateRow(ctx, tx, snapshot.Channel, model.CircuitBreakerStateHalfOpen, 0, successCount, snapshot.LastFailureAt, snapshot.OpenedAt, snapshot.HalfOpenAt); err != nil {
			return fmt.Errorf("RecordSuccess: increment half-open success: %w", err)
		}
		return nil

	case model.CircuitBreakerStateOpen:
		if err := cb.updateStateRow(ctx, tx, snapshot.Channel, model.CircuitBreakerStateHalfOpen, 0, 1, snapshot.LastFailureAt, sql.NullTime{Time: now, Valid: true}, sql.NullTime{Time: now, Valid: true}); err != nil {
			return fmt.Errorf("RecordSuccess: open->half-open on success: %w", err)
		}
		return nil

	case model.CircuitBreakerStateClosed:
		if err := cb.updateStateRow(ctx, tx, snapshot.Channel, model.CircuitBreakerStateClosed, 0, snapshot.SuccessCount+1, snapshot.LastFailureAt, snapshot.OpenedAt, snapshot.HalfOpenAt); err != nil {
			return fmt.Errorf("RecordSuccess: update closed state: %w", err)
		}
		return nil
	}

	if err := cb.updateStateRow(ctx, tx, snapshot.Channel, model.CircuitBreakerStateClosed, 0, snapshot.SuccessCount+1, snapshot.LastFailureAt, snapshot.OpenedAt, snapshot.HalfOpenAt); err != nil {
		return fmt.Errorf("RecordSuccess: normalize state: %w", err)
	}
	return nil
}

// RecordFailure updates the circuit breaker on failed send.
func (cb *CircuitBreaker) RecordFailure(
	ctx context.Context,
	tx *sqlx.Tx,
	channel string,
) error {
	if cb == nil {
		return fmt.Errorf("RecordFailure: circuit breaker is nil")
	}
	if tx == nil {
		return fmt.Errorf("RecordFailure: tx is nil")
	}
	if strings.TrimSpace(channel) == "" {
		return fmt.Errorf("RecordFailure: channel is required")
	}

	snapshot, err := cb.ensureAndLockState(ctx, tx, model.NotificationChannel(strings.ToUpper(strings.TrimSpace(channel))))
	if err != nil {
		return fmt.Errorf("RecordFailure: lock state: %w", err)
	}

	now := cb.now()
	threshold := snapshot.ThresholdFailures
	if threshold <= 0 {
		threshold = cb.thresholdFailures
	}

	openBreaker := false
	switch snapshot.State {
	case model.CircuitBreakerStateHalfOpen:
		openBreaker = true
		snapshot.FailureCount = threshold
		snapshot.SuccessCount = 0

	case model.CircuitBreakerStateOpen:
		snapshot.FailureCount++

	case model.CircuitBreakerStateClosed:
		if snapshot.LastFailureAt.Valid && now.Sub(snapshot.LastFailureAt.Time) <= time.Minute {
			snapshot.FailureCount++
		} else {
			snapshot.FailureCount = 1
		}
		snapshot.SuccessCount = 0
		if snapshot.FailureCount >= threshold {
			openBreaker = true
		}
	}

	lastFailure := sql.NullTime{Time: now, Valid: true}
	openedAt := snapshot.OpenedAt
	halfOpenAt := snapshot.HalfOpenAt
	nextState := snapshot.State

	if openBreaker {
		nextState = model.CircuitBreakerStateOpen
		openedAt = sql.NullTime{Time: now, Valid: true}
		halfOpenAt = sql.NullTime{}
	}

	if err := cb.updateStateRow(ctx, tx, snapshot.Channel, nextState, snapshot.FailureCount, snapshot.SuccessCount, lastFailure, openedAt, halfOpenAt); err != nil {
		return fmt.Errorf("RecordFailure: update state: %w", err)
	}

	if openBreaker {
		if err := cb.publishBreakerOpenedEvent(ctx, tx, snapshot.Channel); err != nil {
			return fmt.Errorf("RecordFailure: publish breaker event: %w", err)
		}
	}
	return nil
}

func (cb *CircuitBreaker) ensureAndLockState(ctx context.Context, tx *sqlx.Tx, channel model.NotificationChannel) (breakerSnapshot, error) {
	var snapshot breakerSnapshot
	err := tx.GetContext(ctx, &snapshot, `
		SELECT
			channel,
			state,
			failure_count,
			success_count,
			last_failure_at,
			opened_at,
			half_open_at,
			threshold_failures,
			cooldown_seconds
		FROM circuit_breaker_state
		WHERE channel = $1
		FOR UPDATE
	`, string(channel))
	if err == nil {
		if snapshot.ThresholdFailures <= 0 {
			snapshot.ThresholdFailures = cb.thresholdFailures
		}
		if snapshot.CooldownSeconds <= 0 {
			snapshot.CooldownSeconds = int(cb.cooldownDuration.Seconds())
		}
		return snapshot, nil
	}
	if err != sql.ErrNoRows {
		return breakerSnapshot{}, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO circuit_breaker_state (
			channel,
			state,
			failure_count,
			success_count,
			threshold_failures,
			cooldown_seconds,
			created_at,
			updated_at
		)
		VALUES ($1, 'CLOSED', 0, 0, $2, $3, now(), now())
	`, string(channel), cb.thresholdFailures, int(cb.cooldownDuration.Seconds()))
	if err != nil {
		return breakerSnapshot{}, err
	}

	err = tx.GetContext(ctx, &snapshot, `
		SELECT
			channel,
			state,
			failure_count,
			success_count,
			last_failure_at,
			opened_at,
			half_open_at,
			threshold_failures,
			cooldown_seconds
		FROM circuit_breaker_state
		WHERE channel = $1
		FOR UPDATE
	`, string(channel))
	if err != nil {
		return breakerSnapshot{}, err
	}
	return snapshot, nil
}

func (cb *CircuitBreaker) updateStateRow(
	ctx context.Context,
	tx *sqlx.Tx,
	channel model.NotificationChannel,
	state model.CircuitBreakerStateType,
	failureCount int,
	successCount int,
	lastFailure sql.NullTime,
	openedAt sql.NullTime,
	halfOpenAt sql.NullTime,
) error {
	var lastFailureArg interface{}
	if lastFailure.Valid {
		lastFailureArg = lastFailure.Time
	}
	var openedAtArg interface{}
	if openedAt.Valid {
		openedAtArg = openedAt.Time
	}
	var halfOpenAtArg interface{}
	if halfOpenAt.Valid {
		halfOpenAtArg = halfOpenAt.Time
	}

	_, err := tx.ExecContext(ctx, `
		UPDATE circuit_breaker_state
		SET state = $2,
			failure_count = $3,
			success_count = $4,
			last_failure_at = $5,
			opened_at = $6,
			half_open_at = $7,
			updated_at = now()
		WHERE channel = $1
	`, string(channel), string(state), failureCount, successCount, lastFailureArg, openedAtArg, halfOpenAtArg)
	if err != nil {
		return err
	}
	return nil
}

func (cb *CircuitBreaker) publishBreakerOpenedEvent(ctx context.Context, tx *sqlx.Tx, channel model.NotificationChannel) error {
	payload, err := json.Marshal(NotificationEventPayload{
		Channel: channel,
		Reason:  "threshold exceeded",
	})
	if err != nil {
		return fmt.Errorf("publishBreakerOpenedEvent: marshal payload: %w", err)
	}

	publisher := cb.publisher
	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}
	if err := publisher.PublishEvent(ctx, tx, model.Event{
		EventType:     model.EventCircuitBreakerOpened,
		Payload:       payload,
		Status:        model.EventStatusPending,
		TargetService: "notification-service",
	}); err != nil {
		return fmt.Errorf("publishBreakerOpenedEvent: %w", err)
	}
	cb.logger.Warn("notification circuit breaker opened", "channel", channel)
	return nil
}

func (cb *CircuitBreaker) now() time.Time {
	if cb != nil && cb.nowFunc != nil {
		return cb.nowFunc()
	}
	return time.Now().UTC()
}
