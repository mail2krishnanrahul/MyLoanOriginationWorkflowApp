package database

import (
	"context"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // Use pgx as the underlying driver
	"github.com/jmoiron/sqlx"
)

// Config represents the database connection configuration
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
	MaxConns int
	MinConns int
}

// ConnectionString returns the PostgreSQL DSN
func (c Config) ConnectionString() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.User, c.Password, c.Host, c.Port, c.Database, c.SSLMode,
	)
}

// NewConnection creates a new database connection pool using Config fields
func NewConnection(cfg Config) (*sqlx.DB, error) {
	return NewConnectionFromURL(cfg.ConnectionString(), cfg)
}

// NewConnectionFromURL creates a new database connection pool using a raw DSN string
func NewConnectionFromURL(dsn string, cfg Config) (*sqlx.DB, error) {
	var db *sqlx.DB
	var err error

	// Retry logic for DB connection (useful for docker-compose startup sequencing)
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		db, err = sqlx.Connect("pgx", dsn)
		if err == nil {
			break
		}
		fmt.Printf("Attempt %d: Failed to connect to database (%s). Retrying in 2 seconds...\n", i+1, dsn)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to open database connection after %d retries: %w", maxRetries, err)
	}

	// Configure the connection pool
	maxConns := cfg.MaxConns
	if maxConns == 0 {
		maxConns = 25
	}
	db.SetMaxOpenConns(maxConns)

	minConns := cfg.MinConns
	if minConns == 0 {
		minConns = 5
	}
	db.SetMaxIdleConns(minConns)
	db.SetConnMaxLifetime(15 * time.Minute)

	return db, nil
}

// HealthCheck verifies the database connection is alive
func HealthCheck(ctx context.Context, db *sqlx.DB) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("database health check failed: %w", err)
	}
	return nil
}
