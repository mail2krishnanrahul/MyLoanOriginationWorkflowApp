//go:build ignore

package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	connString := "postgres://myappuser:password@localhost:5432/LoanOriginationDB?sslmode=disable"
	db, err := pgxpool.New(context.Background(), connString)
	if err != nil {
		slog.Error("Unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	b, err := os.ReadFile("../IngestDealRequirements/SampleJSON.json")
	if err != nil {
		slog.Error("failed to read SampleJSON", "error", err)
		os.Exit(1)
	}

	tag, err := db.Exec(context.Background(),
		"UPDATE cases SET metadata = $1 WHERE reference_number = 'LOAN-2026-00004'",
		string(b),
	)
	if err != nil {
		slog.Error("failed to update", "error", err)
		os.Exit(1)
	}

	slog.Info("Rows updated", "count", tag.RowsAffected())
}
