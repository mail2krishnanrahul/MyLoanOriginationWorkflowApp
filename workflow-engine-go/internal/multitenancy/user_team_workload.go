package multitenancy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

func assertOperationalReportAccess(ctx context.Context, db *sqlx.DB, tenantID string) error {
	actorID := actorUserIDFromContext(ctx)
	if strings.TrimSpace(actorID) == "" {
		return permissionDeniedError(PermissionReportOperational)
	}
	if err := AssertPermission(ctx, db, actorID, tenantID, PermissionReportOperational); err == nil {
		return nil
	} else if !errors.Is(err, ErrPermissionDenied) {
		return err
	}
	if err := AssertPermission(ctx, db, actorID, tenantID, PermissionTaskReassign); err == nil {
		return nil
	} else if !errors.Is(err, ErrPermissionDenied) {
		return err
	}
	return permissionDeniedError(PermissionReportOperational)
}

// GetUserWorkload returns workload counters and risk markers for one user.
func GetUserWorkload(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
) (UserWorkload, error) {
	if db == nil {
		return UserWorkload{}, fmt.Errorf("GetUserWorkload: db is nil")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return UserWorkload{}, fmt.Errorf("GetUserWorkload: userID is required")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "GetUserWorkload")
	if err != nil {
		return UserWorkload{}, fmt.Errorf("GetUserWorkload: %w", err)
	}
	if err := assertOperationalReportAccess(ctx, db, resolvedTenantID); err != nil {
		return UserWorkload{}, fmt.Errorf("GetUserWorkload: %w", err)
	}

	var workload UserWorkload
	workload.UserID = userID
	if err := db.GetContext(ctx, &workload, `
		SELECT
			$2::text AS user_id,
			COUNT(*) FILTER (
				WHERE assigned_user_id = $2::uuid
				  AND status = 'PENDING'
			)::int AS pending_count,
			COUNT(*) FILTER (
				WHERE assigned_user_id = $2::uuid
				  AND status = 'IN_PROGRESS'
			)::int AS in_progress_count,
			COUNT(*) FILTER (
				WHERE assigned_user_id = $2::uuid
				  AND status = 'COMPLETED'
				  AND completed_at >= date_trunc('day', now() AT TIME ZONE 'UTC')
			)::int AS completed_today_count,
			COALESCE(
				MAX(EXTRACT(EPOCH FROM (now() - created_at))) FILTER (
					WHERE assigned_user_id = $2::uuid
					  AND status = 'PENDING'
				),
				0
			)::bigint AS oldest_pending_age_seconds,
			COUNT(*) FILTER (
				WHERE assigned_user_id = $2::uuid
				  AND status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL')
				  AND due_at IS NOT NULL
				  AND due_at <= now() + interval '4 hours'
			)::int AS sla_at_risk_count
		FROM tasks
		WHERE tenant_id = $1::uuid
	`, resolvedTenantID, userID); err != nil {
		return UserWorkload{}, fmt.Errorf("GetUserWorkload: query workload: %w", err)
	}

	logUserTeamInfo("user workload fetched", "tenant_id", resolvedTenantID, "user_id", userID, "pending_count", workload.PendingCount, "in_progress_count", workload.InProgressCount)
	return workload, nil
}

// GetTeamWorkload returns per-member counts and team queue pressure.
func GetTeamWorkload(
	ctx context.Context,
	db *sqlx.DB,
	teamID string,
	tenantID string,
) (TeamWorkload, error) {
	if db == nil {
		return TeamWorkload{}, fmt.Errorf("GetTeamWorkload: db is nil")
	}
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return TeamWorkload{}, fmt.Errorf("GetTeamWorkload: teamID is required")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "GetTeamWorkload")
	if err != nil {
		return TeamWorkload{}, fmt.Errorf("GetTeamWorkload: %w", err)
	}
	if err := assertOperationalReportAccess(ctx, db, resolvedTenantID); err != nil {
		return TeamWorkload{}, fmt.Errorf("GetTeamWorkload: %w", err)
	}

	var teamExists bool
	if err := db.GetContext(ctx, &teamExists, `
		SELECT EXISTS (
			SELECT 1
			FROM teams
			WHERE tenant_id = $1::uuid
			  AND team_id = $2::uuid
		)
	`, resolvedTenantID, teamID); err != nil {
		return TeamWorkload{}, fmt.Errorf("GetTeamWorkload: check team existence: %w", err)
	}
	if !teamExists {
		return TeamWorkload{}, fmt.Errorf("GetTeamWorkload: %w", ErrTeamNotFound)
	}

	members := make([]TeamMemberWorkload, 0)
	if err := db.SelectContext(ctx, &members, `
		SELECT
			tm.user_id::text AS user_id,
			u.display_name,
			COUNT(*) FILTER (
				WHERE t.assigned_user_id = tm.user_id
				  AND t.status = 'PENDING'
			)::int AS pending_count,
			COUNT(*) FILTER (
				WHERE t.assigned_user_id = tm.user_id
				  AND t.status = 'IN_PROGRESS'
			)::int AS in_progress_count,
			COUNT(*) FILTER (
				WHERE t.assigned_user_id = tm.user_id
				  AND t.status = 'COMPLETED'
				  AND t.completed_at >= date_trunc('day', now() AT TIME ZONE 'UTC')
			)::int AS completed_today_count
		FROM team_members tm
		JOIN users u
		  ON u.tenant_id = tm.tenant_id
		 AND u.user_id = tm.user_id
		LEFT JOIN tasks t
		  ON t.tenant_id = tm.tenant_id
		 AND t.assigned_user_id = tm.user_id
		 AND (t.assigned_team_id = tm.team_id OR t.assigned_team_id IS NULL)
		WHERE tm.tenant_id = $1::uuid
		  AND tm.team_id = $2::uuid
		GROUP BY tm.user_id, u.display_name
		ORDER BY u.display_name ASC
	`, resolvedTenantID, teamID); err != nil {
		return TeamWorkload{}, fmt.Errorf("GetTeamWorkload: query member workload: %w", err)
	}

	type teamAgg struct {
		UnassignedPoolTaskCount   int   `db:"unassigned_pool_task_count"`
		TeamQueueDepth            int   `db:"team_queue_depth"`
		OldestUnassignedAgeSeconds int64 `db:"oldest_unassigned_age_seconds"`
	}
	var agg teamAgg
	if err := db.GetContext(ctx, &agg, `
		SELECT
			COUNT(*) FILTER (
				WHERE assigned_team_id = $2::uuid
				  AND assigned_user_id IS NULL
				  AND status = 'PENDING'
			)::int AS unassigned_pool_task_count,
			COUNT(*) FILTER (
				WHERE assigned_team_id = $2::uuid
				  AND status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL')
			)::int AS team_queue_depth,
			COALESCE(
				MAX(EXTRACT(EPOCH FROM (now() - created_at))) FILTER (
					WHERE assigned_team_id = $2::uuid
					  AND assigned_user_id IS NULL
					  AND status = 'PENDING'
				),
				0
			)::bigint AS oldest_unassigned_age_seconds
		FROM tasks
		WHERE tenant_id = $1::uuid
	`, resolvedTenantID, teamID); err != nil {
		return TeamWorkload{}, fmt.Errorf("GetTeamWorkload: query aggregate workload: %w", err)
	}

	out := TeamWorkload{
		TeamID:                     teamID,
		Members:                    members,
		UnassignedPoolTaskCount:    agg.UnassignedPoolTaskCount,
		TeamQueueDepth:             agg.TeamQueueDepth,
		OldestUnassignedAgeSeconds: agg.OldestUnassignedAgeSeconds,
	}
	logUserTeamInfo("team workload fetched", "tenant_id", resolvedTenantID, "team_id", teamID, "member_count", len(out.Members), "queue_depth", out.TeamQueueDepth)
	return out, nil
}

// ListUsersWithWorkload returns active users with counters using one aggregated query.
func ListUsersWithWorkload(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	teamID string,
	page, size int,
) ([]UserWorkloadRow, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("ListUsersWithWorkload: db is nil")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "ListUsersWithWorkload")
	if err != nil {
		return nil, 0, fmt.Errorf("ListUsersWithWorkload: %w", err)
	}
	if err := assertOperationalReportAccess(ctx, db, resolvedTenantID); err != nil {
		return nil, 0, fmt.Errorf("ListUsersWithWorkload: %w", err)
	}
	page, size = sanitizePagination(page, size)
	offset := (page - 1) * size
	teamID = strings.TrimSpace(teamID)

	type row struct {
		UserWorkloadRow
		TotalCount int `db:"total_count"`
	}
	rows := make([]row, 0)
	if err := db.SelectContext(ctx, &rows, `
		WITH filtered_users AS (
			SELECT
				u.user_id,
				u.username,
				u.display_name,
				u.email,
				COALESCE(tm.team_id::text, '') AS team_id
			FROM users u
			LEFT JOIN team_members tm
			  ON tm.tenant_id = u.tenant_id
			 AND tm.user_id = u.user_id
			WHERE u.tenant_id = $1::uuid
			  AND u.status = 'ACTIVE'
			  AND ($2 = '' OR tm.team_id = $2::uuid)
		)
		SELECT
			fu.user_id::text AS user_id,
			fu.username,
			fu.display_name,
			fu.email,
			fu.team_id,
			COUNT(*) FILTER (
				WHERE t.status = 'PENDING'
				  AND t.assigned_user_id = fu.user_id
			)::int AS pending_count,
			COUNT(*) FILTER (
				WHERE t.status = 'IN_PROGRESS'
				  AND t.assigned_user_id = fu.user_id
			)::int AS in_progress_count,
			COUNT(*) FILTER (
				WHERE t.status = 'COMPLETED'
				  AND t.assigned_user_id = fu.user_id
				  AND t.completed_at >= date_trunc('day', now() AT TIME ZONE 'UTC')
			)::int AS completed_today_count,
			COUNT(*) FILTER (
				WHERE t.assigned_user_id = fu.user_id
				  AND t.status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL')
				  AND t.due_at IS NOT NULL
				  AND t.due_at <= now() + interval '4 hours'
			)::int AS sla_at_risk_count,
			COUNT(*) OVER() AS total_count
		FROM filtered_users fu
		LEFT JOIN tasks t
		  ON t.tenant_id = $1::uuid
		 AND t.assigned_user_id = fu.user_id
		GROUP BY fu.user_id, fu.username, fu.display_name, fu.email, fu.team_id
		ORDER BY fu.display_name ASC, fu.username ASC
		LIMIT $3 OFFSET $4
	`, resolvedTenantID, teamID, size, offset); err != nil {
		return nil, 0, fmt.Errorf("ListUsersWithWorkload: query workload rows: %w", err)
	}

	out := make([]UserWorkloadRow, 0, len(rows))
	total := 0
	for i := range rows {
		if i == 0 {
			total = rows[i].TotalCount
		}
		out = append(out, rows[i].UserWorkloadRow)
	}
	logUserTeamInfo("users with workload listed", "tenant_id", resolvedTenantID, "team_id", teamID, "count", len(out), "total", total, "page", page, "size", size)
	return out, total, nil
}

// GetUnassignedTaskQueue returns tenant/team pending queue in assignment order.
func GetUnassignedTaskQueue(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	teamID string,
	page, size int,
) ([]Task, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("GetUnassignedTaskQueue: db is nil")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "GetUnassignedTaskQueue")
	if err != nil {
		return nil, 0, fmt.Errorf("GetUnassignedTaskQueue: %w", err)
	}
	if err := assertOperationalReportAccess(ctx, db, resolvedTenantID); err != nil {
		return nil, 0, fmt.Errorf("GetUnassignedTaskQueue: %w", err)
	}
	page, size = sanitizePagination(page, size)
	offset := (page - 1) * size
	teamID = strings.TrimSpace(teamID)

	type row struct {
		ID                 string         `db:"id"`
		TenantID           string         `db:"tenant_id"`
		CaseID             string         `db:"case_id"`
		TaskDefinitionCode string         `db:"task_definition_code"`
		ActivityCode       string         `db:"activity_code"`
		StageCode          string         `db:"stage_code"`
		Status             string         `db:"status"`
		Priority           int            `db:"priority"`
		AssignedService    *string        `db:"assigned_service"`
		AssignedAt         sql.NullTime   `db:"assigned_at"`
		StartedAt          sql.NullTime   `db:"started_at"`
		CompletedAt        sql.NullTime   `db:"completed_at"`
		DueAt              sql.NullTime   `db:"due_at"`
		RetryCount         int            `db:"retry_count"`
		MaxRetries         int            `db:"max_retries"`
		InputPayload       []byte         `db:"input_payload"`
		OutputPayload      []byte         `db:"output_payload"`
		Metadata           []byte         `db:"metadata"`
		ErrorDetail        []byte         `db:"error_detail"`
		IdempotencyKey     string         `db:"idempotency_key"`
		Version            int            `db:"version"`
		CreatedAt          sql.NullTime   `db:"created_at"`
		UpdatedAt          sql.NullTime   `db:"updated_at"`
		AssignedUserID     sql.NullString `db:"assigned_user_id"`
		AssignedTeamID     sql.NullString `db:"assigned_team_id"`
		TotalCount         int            `db:"total_count"`
	}

	rows := make([]row, 0)
	if err := db.SelectContext(ctx, &rows, `
		SELECT
			id::text AS id,
			tenant_id::text AS tenant_id,
			case_id::text AS case_id,
			task_definition_code,
			activity_code,
			stage_code,
			status,
			priority,
			assigned_service,
			assigned_at,
			started_at,
			completed_at,
			due_at,
			retry_count,
			max_retries,
			input_payload,
			output_payload,
			metadata,
			error_detail,
			idempotency_key,
			version,
			created_at,
			updated_at,
			assigned_user_id::text AS assigned_user_id,
			assigned_team_id::text AS assigned_team_id,
			COUNT(*) OVER() AS total_count
		FROM tasks
		WHERE tenant_id = $1::uuid
		  AND status = 'PENDING'
		  AND assigned_user_id IS NULL
		  AND (
			($2 = '' AND assigned_team_id IS NULL)
			OR
			($2 <> '' AND assigned_team_id = $2::uuid)
		  )
		ORDER BY priority DESC, created_at ASC
		LIMIT $3 OFFSET $4
	`, resolvedTenantID, teamID, size, offset); err != nil {
		return nil, 0, fmt.Errorf("GetUnassignedTaskQueue: query queue: %w", err)
	}

	out := make([]Task, 0, len(rows))
	total := 0
	for i := range rows {
		if i == 0 {
			total = rows[i].TotalCount
		}
		task := Task{
			ID:                 rows[i].ID,
			TenantID:           rows[i].TenantID,
			CaseID:             rows[i].CaseID,
			TaskDefinitionCode: rows[i].TaskDefinitionCode,
			ActivityCode:       rows[i].ActivityCode,
			StageCode:          rows[i].StageCode,
			Status:             model.TaskStatus(rows[i].Status),
			Priority:           model.TaskPriority(rows[i].Priority),
			AssignedService:    rows[i].AssignedService,
			RetryCount:         rows[i].RetryCount,
			MaxRetries:         rows[i].MaxRetries,
			InputPayload:       rows[i].InputPayload,
			OutputPayload:      rows[i].OutputPayload,
			Metadata:           rows[i].Metadata,
			ErrorDetail:        rows[i].ErrorDetail,
			IdempotencyKey:     rows[i].IdempotencyKey,
			Version:            rows[i].Version,
		}
		if rows[i].AssignedAt.Valid {
			t := rows[i].AssignedAt.Time
			task.AssignedAt = &t
		}
		if rows[i].StartedAt.Valid {
			t := rows[i].StartedAt.Time
			task.StartedAt = &t
		}
		if rows[i].CompletedAt.Valid {
			t := rows[i].CompletedAt.Time
			task.CompletedAt = &t
		}
		if rows[i].DueAt.Valid {
			t := rows[i].DueAt.Time
			task.DueAt = &t
		}
		if rows[i].CreatedAt.Valid {
			task.CreatedAt = rows[i].CreatedAt.Time
		}
		if rows[i].UpdatedAt.Valid {
			task.UpdatedAt = rows[i].UpdatedAt.Time
		}
		if rows[i].AssignedUserID.Valid {
			v := strings.TrimSpace(rows[i].AssignedUserID.String)
			if v != "" {
				task.AssignedUserID = &v
			}
		}
		if rows[i].AssignedTeamID.Valid {
			v := strings.TrimSpace(rows[i].AssignedTeamID.String)
			if v != "" {
				task.AssignedTeamID = &v
			}
		}
		out = append(out, task)
	}

	logUserTeamInfo("unassigned task queue fetched", "tenant_id", resolvedTenantID, "team_id", teamID, "count", len(out), "total", total, "page", page, "size", size)
	return out, total, nil
}
