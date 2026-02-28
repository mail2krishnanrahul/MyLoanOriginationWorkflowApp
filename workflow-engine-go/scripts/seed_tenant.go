//go:build ignore

package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dbURL := "postgres://myappuser:password@localhost:5432/LoanOriginationDB?sslmode=disable"
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("Unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	query := `INSERT INTO tenants (tenant_id, tenant_code, name, status, tier, config) 
	VALUES ('00000000-0000-0000-0000-000000000000', 'DEFAULT', 'Default System Tenant', 'ACTIVE', 'ENTERPRISE', '{"max_active_cases":10000, "max_concurrent_tasks":1000, "max_cases_per_minute":100, "feature_flags":{"compensation_enabled":true, "dlq_requeue_enabled":true, "notification_enabled":true, "sub_case_enabled":true, "sla_enforcement_enabled":true}}'::jsonb) 
	ON CONFLICT DO NOTHING;`

	_, err = pool.Exec(ctx, query)
	if err != nil {
		slog.Error("Insert failed", "error", err)
		os.Exit(1)
	}

	slog.Info("Tenant seeded successfully")
}
