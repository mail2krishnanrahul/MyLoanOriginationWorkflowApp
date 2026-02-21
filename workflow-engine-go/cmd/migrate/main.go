package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"workflow-engine/internal/database"

	"github.com/joho/godotenv"
)

func main() {
	// Attempt to load .env from parent directories if present, default to environment
	_ = godotenv.Load("../../.env", "../.env", ".env")

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "db/migrations"
	}

	flag.Parse()
	args := flag.Args()

	if len(args) < 1 {
		fmt.Println("Usage: migrate <command> [options]")
		fmt.Println("\nCommands:")
		fmt.Println("  up                Run all up migrations")
		fmt.Println("  down <steps>      Roll back the specified number of migrations")
		fmt.Println("  version           Print current migration version")
		fmt.Println("  force <version>   Force migration version (use to fix dirty state)")
		os.Exit(1)
	}

	cmd := args[0]

	switch cmd {
	case "up":
		if err := database.RunMigrations(dbURL, migrationsDir); err != nil {
			slog.Error("Migration failed", "error", err)
			os.Exit(1)
		}

	case "down":
		if len(args) < 2 {
			slog.Error("Please specify the number of steps to rollback (e.g. migrate down 1)")
			os.Exit(1)
		}
		steps, err := strconv.Atoi(args[1])
		if err != nil || steps < 1 {
			slog.Error("Invalid number of steps", "steps", args[1])
			os.Exit(1)
		}
		if err := database.RollbackMigration(dbURL, migrationsDir, steps); err != nil {
			slog.Error("Rollback failed", "error", err)
			os.Exit(1)
		}

	case "version":
		version, dirty, err := database.GetMigrationVersion(dbURL, migrationsDir)
		if err != nil {
			slog.Error("Failed to check version", "error", err)
			os.Exit(1)
		}
		fmt.Printf("Current migration version: %d (Dirty: %v)\n", version, dirty)

	case "force":
		if len(args) < 2 {
			slog.Error("Please specify the version to force (e.g. migrate force 2)")
			os.Exit(1)
		}
		version, err := strconv.Atoi(args[1])
		if err != nil || version < 0 {
			slog.Error("Invalid version", "version", args[1])
			os.Exit(1)
		}
		if err := database.ForceVersion(dbURL, migrationsDir, version); err != nil {
			slog.Error("Force version failed", "error", err)
			os.Exit(1)
		}

	default:
		slog.Error("Unknown command", "command", cmd)
		os.Exit(1)
	}
}
