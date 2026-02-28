//go:build ignore

package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5"
)

func main() {
	connUrl := "postgres://myappuser:password@localhost:5432/LoanOriginationDB?sslmode=disable"
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, connUrl)
	if err != nil {
		slog.Error("Unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	var tenantId string
	err = conn.QueryRow(ctx, "SELECT tenant_id FROM tenants LIMIT 1").Scan(&tenantId)
	if err != nil {
		slog.Error("QueryRow failed", "error", err)
		os.Exit(1)
	}

	slog.Info("TENANT_ID retrieved", "tenant_id", tenantId)
}
