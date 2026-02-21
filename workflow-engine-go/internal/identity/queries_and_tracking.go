package identity

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// ---------------------------------------------------------------------------
// 9. Query functions (User/Team) continued
// ---------------------------------------------------------------------------

func (s *IdentityService) GetUser(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
) (model.User, error) {
	var user model.User
	err := db.GetContext(ctx, &user, `
		SELECT * FROM users WHERE user_id = $1::uuid AND tenant_id = $2::uuid
	`, userID, tenantID)

	if err != nil {
		if err == sql.ErrNoRows {
			return model.User{}, model.ErrUserNotFound
		}
		return model.User{}, fmt.Errorf("GetUser: query: %w", err)
	}
	return user, nil
}

func (s *IdentityService) GetUserByExternalID(
	ctx context.Context,
	db *sqlx.DB,
	externalID string,
	tenantID string,
) (model.User, error) {
	var user model.User
	err := db.GetContext(ctx, &user, `
		SELECT * FROM users WHERE external_id = $1 AND tenant_id = $2::uuid
	`, externalID, tenantID)

	if err != nil {
		if err == sql.ErrNoRows {
			return model.User{}, model.ErrUserNotFound
		}
		return model.User{}, fmt.Errorf("GetUserByExternalID: query: %w", err)
	}
	return user, nil
}

func (s *IdentityService) ListTeams(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	filters model.ListTeamsFilters,
	page, size int,
) ([]model.Team, int, error) {
	if size > 200 {
		size = 200
	}
	offset := (page - 1) * size

	var whereClauses []string
	var args []interface{}

	whereClauses = append(whereClauses, "tenant_id = $1::uuid")
	args = append(args, tenantID)

	if filters.Status != nil {
		args = append(args, *filters.Status)
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if filters.TeamType != nil {
		args = append(args, *filters.TeamType)
		whereClauses = append(whereClauses, fmt.Sprintf("team_type = $%d", len(args)))
	}
	if filters.ParentTeamID != nil {
		args = append(args, *filters.ParentTeamID)
		whereClauses = append(whereClauses, fmt.Sprintf("parent_team_id = $%d::uuid", len(args)))
	}

	where := "WHERE " + strings.Join(whereClauses, " AND ")

	query := fmt.Sprintf(`
		SELECT * FROM teams
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)+1, len(args)+2)

	args = append(args, size, offset)

	var teams []model.Team
	err := db.SelectContext(ctx, &teams, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("ListTeams: query: %w", err)
	}

	var total int
	countQuery := fmt.Sprintf(`SELECT count(*) FROM teams %s`, where)
	err = db.QueryRowxContext(ctx, countQuery, args[:len(args)-2]...).Scan(&total)
	if err != nil {
		total = len(teams)
	}

	return teams, total, nil
}

func (s *IdentityService) GetTeamMembers(
	ctx context.Context,
	db *sqlx.DB,
	teamID string,
	tenantID string,
	page, size int,
) ([]model.TeamMember, int, error) {
	if size > 200 {
		size = 200
	}
	offset := (page - 1) * size

	var members []model.TeamMember
	err := db.SelectContext(ctx, &members, `
		SELECT * FROM team_members
		WHERE team_id = $1::uuid AND tenant_id = $2::uuid
		ORDER BY joined_at DESC
		LIMIT $3 OFFSET $4
	`, teamID, tenantID, size, offset)

	if err != nil {
		return nil, 0, fmt.Errorf("GetTeamMembers: select: %w", err)
	}

	var total int
	err = db.QueryRowxContext(ctx, `SELECT count(*) FROM team_members WHERE team_id = $1::uuid AND tenant_id = $2::uuid`, teamID, tenantID).Scan(&total)
	if err != nil {
		total = len(members)
	}

	return members, total, nil
}

// ---------------------------------------------------------------------------
// 10. LoginTracker - Buffered background flusher
// ---------------------------------------------------------------------------

type LoginTracker struct {
	db       *sqlx.DB
	buffer   sync.Map // map[string]time.Time (userID -> lastLogin)
	interval time.Duration
	logger   *slog.Logger
}

func NewLoginTracker(
	db *sqlx.DB,
	interval time.Duration,
	logger *slog.Logger,
) *LoginTracker {
	return &LoginTracker{
		db:       db,
		interval: interval,
		logger:   logger,
	}
}

// Record implements Sub-Capability 10 buffering semantics.
func (lt *LoginTracker) Record(ctx context.Context, userID string) {
	lt.buffer.Store(userID, time.Now().UTC())
}

// Run executes the periodic DB flush of the sync.Map.
func (lt *LoginTracker) Run(ctx context.Context) error {
	ticker := time.NewTicker(lt.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			lt.flush(context.Background()) // final flush on shutdown
			return ctx.Err()
		case <-ticker.C:
			lt.flush(ctx)
		}
	}
}

func (lt *LoginTracker) flush(ctx context.Context) {
	var keys []string
	var times []time.Time

	// Extract everything from map atomically-ish and delete instantly
	lt.buffer.Range(func(key, value interface{}) bool {
		uid := key.(string)
		t := value.(time.Time)

		keys = append(keys, uid)
		times = append(times, t)

		lt.buffer.Delete(key)
		return true
	})

	if len(keys) == 0 {
		return
	}

	// We use an UNNEST construct to batch-update
	// This maps the arrays so PG doesn't have to evaluate a gigantic `CASE WHEN` statement
	query := `
		UPDATE users u
		SET last_login_at = latest.login_time
		FROM (
			SELECT unnest($1::uuid[]) AS user_id, unnest($2::timestamptz[]) AS login_time
		) AS latest
		WHERE u.user_id = latest.user_id
	`
	_, err := lt.db.ExecContext(ctx, query, pq.Array(keys), pq.Array(times))
	if err != nil {
		lt.logger.Error("failed to flush login tracker buffer", "error", err, "dropped_events", len(keys))
		// Optional: We dropped writes. We could re-store them into `lt.buffer`,
		// but tracking login_at is low criticality and generally safe to drop if DB is severely degraded.
	} else {
		lt.logger.Debug("flushed login buffer", "count", len(keys))
	}
}

// RecordUserLogin ties into Sub-Capability 2 logic, ensuring the user is ACTIVE
// before delegating to the LoginTracker.
func (s *IdentityService) RecordUserLogin(
	ctx context.Context,
	db *sqlx.DB,
	tracker *LoginTracker,
	userID string,
	tenantID string,
) error {
	var status string
	err := db.GetContext(ctx, &status, `
		SELECT status FROM users WHERE user_id = $1::uuid AND tenant_id = $2::uuid
	`, userID, tenantID)

	if err != nil {
		if err == sql.ErrNoRows {
			return model.ErrUserNotFound
		}
		return fmt.Errorf("RecordUserLogin: select: %w", err)
	}

	if status == string(model.UserStatusSuspended) {
		return model.ErrUserSuspended
	}
	if status == string(model.UserStatusDeactivated) {
		return model.ErrUserDeactivated
	}

	// Not blocked; delegate via tracker
	tracker.Record(ctx, userID)

	return nil
}
