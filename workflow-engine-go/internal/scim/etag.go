package scim

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

// SCIMETag renders weak ETag form W/"<version>".
func SCIMETag(version int) string {
	if version < 1 {
		version = 1
	}
	return fmt.Sprintf("W/\"%d\"", version)
}

// ParseIfMatch parses If-Match header value W/"<version>".
func ParseIfMatch(header string) (version int, err error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, fmt.Errorf("ParseIfMatch: %w", ErrInvalidETag)
	}
	if header == "*" {
		return 0, fmt.Errorf("ParseIfMatch: %w", ErrInvalidETag)
	}
	header = strings.TrimPrefix(header, "W/")
	header = strings.TrimPrefix(header, "w/")
	if len(header) < 3 || !strings.HasPrefix(header, "\"") || !strings.HasSuffix(header, "\"") {
		return 0, fmt.Errorf("ParseIfMatch: %w", ErrInvalidETag)
	}
	raw := strings.Trim(header, "\"")
	v, parseErr := strconv.Atoi(raw)
	if parseErr != nil || v < 1 {
		return 0, fmt.Errorf("ParseIfMatch: %w", ErrInvalidETag)
	}
	return v, nil
}

// ValidateIfMatch checks a User If-Match header against current DB version.
func ValidateIfMatch(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
	header string,
) error {
	if db == nil {
		return fmt.Errorf("ValidateIfMatch: db is nil")
	}
	expected, err := ParseIfMatch(header)
	if err != nil {
		return fmt.Errorf("ValidateIfMatch: %w", err)
	}
	var current int
	if err := db.GetContext(ctx, &current, `
		SELECT version
		FROM users
		WHERE tenant_id = $1::uuid
		  AND user_id = $2::uuid
	`, strings.TrimSpace(tenantID), strings.TrimSpace(userID)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("ValidateIfMatch: %w", ErrETagMismatch)
		}
		return fmt.Errorf("ValidateIfMatch: query current version: %w", err)
	}
	if current != expected {
		return fmt.Errorf("ValidateIfMatch: %w", ErrETagMismatch)
	}
	return nil
}

func validateIfMatchTeam(
	ctx context.Context,
	db *sqlx.DB,
	teamID string,
	tenantID string,
	header string,
) error {
	if db == nil {
		return fmt.Errorf("validateIfMatchTeam: db is nil")
	}
	expected, err := ParseIfMatch(header)
	if err != nil {
		return fmt.Errorf("validateIfMatchTeam: %w", err)
	}
	var current int
	if err := db.GetContext(ctx, &current, `
		SELECT version
		FROM teams
		WHERE tenant_id = $1::uuid
		  AND team_id = $2::uuid
	`, strings.TrimSpace(tenantID), strings.TrimSpace(teamID)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("validateIfMatchTeam: %w", ErrETagMismatch)
		}
		return fmt.Errorf("validateIfMatchTeam: query current version: %w", err)
	}
	if current != expected {
		return fmt.Errorf("validateIfMatchTeam: %w", ErrETagMismatch)
	}
	return nil
}

// HandleIfNoneMatch returns true when If-None-Match matches current version.
func HandleIfNoneMatch(currentVersion int, header string) bool {
	if strings.TrimSpace(header) == "" {
		return false
	}
	if strings.TrimSpace(header) == "*" {
		return true
	}
	expected, err := ParseIfMatch(header)
	if err != nil {
		return false
	}
	return currentVersion == expected
}
