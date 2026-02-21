package database

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// RunMigrations applies all "up" migrations
func RunMigrations(dbURL string, migrationsDir string) error {
	sourceURL := fmt.Sprintf("file://%s", migrationsDir)

	slog.Info("Running database migrations...", "source", sourceURL)

	m, err := migrate.New(sourceURL, dbURL)
	if err != nil {
		return fmt.Errorf("failed to initialize migrator: %w", err)
	}
	defer m.Close()
	defer m.Close()

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			slog.Info("Database migrations are already up to date")
			return nil
		}
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	slog.Info("Database migrations applied successfully")
	return nil
}

// RollbackMigration rolls back the last N migrations
func RollbackMigration(dbURL string, migrationsDir string, steps int) error {
	sourceURL := fmt.Sprintf("file://%s", migrationsDir)

	slog.Info("Rolling back database migrations...", "steps", steps)

	m, err := migrate.New(sourceURL, dbURL)
	if err != nil {
		return fmt.Errorf("failed to initialize migrator: %w", err)
	}
	defer m.Close()

	if err := m.Steps(-steps); err != nil {
		return fmt.Errorf("failed to rollback migrations: %w", err)
	}

	slog.Info("Database migrations rolled back successfully")
	return nil
}

// GetMigrationVersion returns the current migration version and dirty status
func GetMigrationVersion(dbURL string, migrationsDir string) (uint, bool, error) {
	sourceURL := fmt.Sprintf("file://%s", migrationsDir)

	m, err := migrate.New(sourceURL, dbURL)
	if err != nil {
		return 0, false, fmt.Errorf("failed to initialize migrator: %w", err)
	}
	defer m.Close()

	version, dirty, err := m.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil // Database is fresh
		}
		return 0, false, fmt.Errorf("failed to get migration version: %w", err)
	}

	return version, dirty, nil
}

// ForceVersion forces the migration version to a specific number to resolve dirty states
func ForceVersion(dbURL string, migrationsDir string, version int) error {
	sourceURL := fmt.Sprintf("file://%s", migrationsDir)

	m, err := migrate.New(sourceURL, dbURL)
	if err != nil {
		return fmt.Errorf("failed to initialize migrator: %w", err)
	}
	defer m.Close()

	if err := m.Force(version); err != nil {
		return fmt.Errorf("failed to force version: %w", err)
	}

	slog.Info("Successfully forced migration version to target", "version", version)
	return nil
}
