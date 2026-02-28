package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// Connect to local DB exposed on 5432 by docker-compose
	connString := "postgres://postgres:postgres@localhost:5432/workflow?sslmode=disable"
	db, err := pgxpool.New(context.Background(), connString)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	defer db.Close()

	// Read SampleJSON.json
	b, err := os.ReadFile("./IngestDealRequirements/SampleJSON.json")
	if err != nil {
		log.Fatalf("failed to read SampleJSON: %v\n", err)
	}

	// Update the metadata for LOAN-2026-00004
	tag, err := db.Exec(context.Background(),
		"UPDATE cases SET metadata = $1 WHERE reference_number = 'LOAN-2026-00004'",
		string(b),
	)
	if err != nil {
		log.Fatalf("failed to update: %v\n", err)
	}

	fmt.Printf("Updated %d rows.\n", tag.RowsAffected())
}
