package scim

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// RecordSCIMAudit inserts an append-only SCIM audit row inside caller transaction.
func RecordSCIMAudit(
	ctx context.Context,
	tx *sqlx.Tx,
	entry SCIMAuditEntry,
) error {
	if tx == nil {
		return fmt.Errorf("RecordSCIMAudit: tx is nil")
	}
	entry.TenantID = strings.TrimSpace(entry.TenantID)
	entry.Operation = strings.ToUpper(strings.TrimSpace(entry.Operation))
	entry.ResourceType = strings.ToUpper(strings.TrimSpace(entry.ResourceType))
	if entry.Operation == "" || entry.ResourceType == "" || entry.TenantID == "" {
		return fmt.Errorf("RecordSCIMAudit: tenant_id, operation and resource_type are required")
	}
	if entry.RequestAttributes == nil {
		entry.RequestAttributes = []string{}
	}
	if entry.OccurredAt.IsZero() {
		entry.OccurredAt = time.Now().UTC()
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO scim_audit_log (
			tenant_id,
			token_id,
			operation,
			resource_type,
			resource_id,
			http_status,
			filter_expression,
			request_attributes,
			operations_count,
			duration_ms,
			ip_address,
			user_agent,
			occurred_at
		)
		VALUES (
			$1::uuid,
			$2::uuid,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			$11,
			$12,
			$13
		)
	`,
		entry.TenantID,
		entry.TokenID,
		entry.Operation,
		entry.ResourceType,
		entry.ResourceID,
		entry.HTTPStatus,
		entry.FilterExpression,
		entry.RequestAttributes,
		entry.OperationsCount,
		entry.DurationMS,
		entry.IPAddress,
		entry.UserAgent,
		entry.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf("RecordSCIMAudit: insert audit: %w", err)
	}
	return nil
}

// GetSCIMAuditLog returns paginated SCIM audit entries within the requested time window.
func GetSCIMAuditLog(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	filters SCIMAuditFilters,
	page, size int,
) ([]SCIMAuditEntry, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("GetSCIMAuditLog: db is nil")
	}
	resolvedTenantID, err := resolveTenantID(ctx, tenantID, "GetSCIMAuditLog")
	if err != nil {
		return nil, 0, fmt.Errorf("GetSCIMAuditLog: %w", err)
	}
	if filters.From.IsZero() || filters.To.IsZero() {
		return nil, 0, fmt.Errorf("GetSCIMAuditLog: from and to are required")
	}
	from := filters.From.UTC()
	to := filters.To.UTC()
	if to.Before(from) {
		return nil, 0, fmt.Errorf("GetSCIMAuditLog: invalid time range")
	}
	if to.Sub(from) > 30*24*time.Hour {
		return nil, 0, fmt.Errorf("GetSCIMAuditLog: time range exceeds 30 days")
	}

	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	offset := (page - 1) * size

	args := []interface{}{resolvedTenantID, from, to}
	where := []string{
		"tenant_id = $1::uuid",
		"occurred_at >= $2",
		"occurred_at <= $3",
	}

	if tokenID := strings.TrimSpace(filters.TokenID); tokenID != "" {
		args = append(args, tokenID)
		where = append(where, fmt.Sprintf("token_id = $%d::uuid", len(args)))
	}
	if op := strings.ToUpper(strings.TrimSpace(filters.Operation)); op != "" {
		args = append(args, op)
		where = append(where, fmt.Sprintf("operation = $%d", len(args)))
	}
	if rt := strings.ToUpper(strings.TrimSpace(filters.ResourceType)); rt != "" {
		args = append(args, rt)
		where = append(where, fmt.Sprintf("resource_type = $%d", len(args)))
	}
	if rid := strings.TrimSpace(filters.ResourceID); rid != "" {
		args = append(args, rid)
		where = append(where, fmt.Sprintf("resource_id = $%d", len(args)))
	}

	args = append(args, size, offset)
	query := fmt.Sprintf(`
		SELECT
			audit_id::text AS audit_id,
			tenant_id::text AS tenant_id,
			token_id::text AS token_id,
			operation,
			resource_type,
			resource_id,
			http_status,
			filter_expression,
			request_attributes,
			operations_count,
			duration_ms,
			ip_address,
			user_agent,
			occurred_at,
			COUNT(*) OVER() AS total_count
		FROM scim_audit_log
		WHERE %s
		ORDER BY occurred_at DESC, audit_id DESC
		LIMIT $%d OFFSET $%d
	`, strings.Join(where, " AND "), len(args)-1, len(args))

	type row struct {
		SCIMAuditEntry
		TotalCount int `db:"total_count"`
	}
	rows := make([]row, 0)
	if err := db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, 0, fmt.Errorf("GetSCIMAuditLog: query audit log: %w", err)
	}

	out := make([]SCIMAuditEntry, 0, len(rows))
	total := 0
	for i := range rows {
		if i == 0 {
			total = rows[i].TotalCount
		}
		out = append(out, rows[i].SCIMAuditEntry)
	}
	return out, total, nil
}

func beginAuditTx(ctx context.Context, db *sqlx.DB, fn string) (*sqlx.Tx, error) {
	if db == nil {
		return nil, fmt.Errorf("%s: db is nil", fn)
	}
	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("%s: begin tx: %w", fn, err)
	}
	return tx, nil
}
