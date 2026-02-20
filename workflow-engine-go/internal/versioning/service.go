package versioning

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"workflow-engine/internal/sla"
	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

type sqlxGetter interface {
	GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
}

// AssertCaseTypeIsMutable rejects config mutation attempts for non-DRAFT versions.
func AssertCaseTypeIsMutable(
	ctx context.Context,
	db *sqlx.DB,
	caseTypeID string,
) error {
	if db == nil {
		return fmt.Errorf("AssertCaseTypeIsMutable: db is nil")
	}
	if err := assertCaseTypeIsMutableWithGetter(ctx, db, caseTypeID); err != nil {
		return fmt.Errorf("AssertCaseTypeIsMutable: %w", err)
	}
	return nil
}

// AssertCaseTypeIsMutableTx is the transactional variant used by write paths.
func AssertCaseTypeIsMutableTx(
	ctx context.Context,
	tx *sqlx.Tx,
	caseTypeID string,
) error {
	if tx == nil {
		return fmt.Errorf("AssertCaseTypeIsMutableTx: tx is nil")
	}
	if err := assertCaseTypeIsMutableWithGetter(ctx, tx, caseTypeID); err != nil {
		return fmt.Errorf("AssertCaseTypeIsMutableTx: %w", err)
	}
	return nil
}

func assertCaseTypeIsMutableWithGetter(ctx context.Context, getter sqlxGetter, caseTypeID string) error {
	caseTypeID = strings.TrimSpace(caseTypeID)
	if caseTypeID == "" {
		return fmt.Errorf("assertCaseTypeIsMutableWithGetter: caseTypeID is required")
	}

	var status string
	err := getter.GetContext(ctx, &status, `
		SELECT status
		FROM case_types
		WHERE id = $1::uuid
	`, caseTypeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("assertCaseTypeIsMutableWithGetter: case_type %s not found", caseTypeID)
		}
		return fmt.Errorf("assertCaseTypeIsMutableWithGetter: query case_type status: %w", err)
	}

	if CaseTypeStatus(strings.ToUpper(strings.TrimSpace(status))) != CaseTypeStatusDraft {
		actor := actorFromContext(ctx)
		slog.Warn("blocked immutable case_type mutation attempt",
			"case_type_id", caseTypeID,
			"status", status,
			"actor", actor)
		return &ImmutableCaseTypeError{
			CaseTypeID: caseTypeID,
			Status:     CaseTypeStatus(status),
		}
	}

	return nil
}

// ResolveCaseTypeConfig resolves configuration from a case's pinned version.
func ResolveCaseTypeConfig(
	ctx context.Context,
	db *sqlx.DB,
	caseID string,
) (CaseTypeConfig, error) {
	if db == nil {
		return CaseTypeConfig{}, fmt.Errorf("ResolveCaseTypeConfig: db is nil")
	}
	caseID = strings.TrimSpace(caseID)
	if caseID == "" {
		return CaseTypeConfig{}, fmt.Errorf("ResolveCaseTypeConfig: caseID is required")
	}

	var rawConfig []byte
	if err := db.GetContext(ctx, &rawConfig, `
		SELECT ct.config
		FROM cases c
		JOIN case_types ct
		  ON ct.id = COALESCE(c.case_type_version_id, c.case_type_id)
		WHERE c.id = $1::uuid
	`, caseID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CaseTypeConfig{}, fmt.Errorf("ResolveCaseTypeConfig: case %s not found", caseID)
		}
		return CaseTypeConfig{}, fmt.Errorf("ResolveCaseTypeConfig: query pinned config: %w", err)
	}

	cfg := CaseTypeConfig{}
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return CaseTypeConfig{}, fmt.Errorf("ResolveCaseTypeConfig: unmarshal config: %w", err)
	}
	return cfg, nil
}

// CreateCaseTypeDraftVersion creates the next monotonic DRAFT version for a case_type_code.
func CreateCaseTypeDraftVersion(
	ctx context.Context,
	db *sqlx.DB,
	caseTypeCode string,
	name string,
	description string,
	config CaseTypeConfig,
	createdBy string,
) (CaseTypeVersion, error) {
	if db == nil {
		return CaseTypeVersion{}, fmt.Errorf("CreateCaseTypeDraftVersion: db is nil")
	}
	caseTypeCode = strings.ToUpper(strings.TrimSpace(caseTypeCode))
	if caseTypeCode == "" {
		return CaseTypeVersion{}, fmt.Errorf("CreateCaseTypeDraftVersion: caseTypeCode is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return CaseTypeVersion{}, fmt.Errorf("CreateCaseTypeDraftVersion: name is required")
	}
	createdBy = strings.TrimSpace(createdBy)
	if createdBy == "" {
		createdBy = actorFromContext(ctx)
	}

	rawConfig, err := json.Marshal(config)
	if err != nil {
		return CaseTypeVersion{}, fmt.Errorf("CreateCaseTypeDraftVersion: marshal config: %w", err)
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return CaseTypeVersion{}, fmt.Errorf("CreateCaseTypeDraftVersion: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := lockCaseTypeCodeRowsTx(ctx, tx, caseTypeCode); err != nil {
		return CaseTypeVersion{}, fmt.Errorf("CreateCaseTypeDraftVersion: lock case_type code rows: %w", err)
	}

	var nextVersion int
	if err := tx.GetContext(ctx, &nextVersion, `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM case_types
		WHERE code = $1
	`, caseTypeCode); err != nil {
		return CaseTypeVersion{}, fmt.Errorf("CreateCaseTypeDraftVersion: compute next version: %w", err)
	}

	var descriptionValue interface{}
	if strings.TrimSpace(description) == "" {
		descriptionValue = nil
	} else {
		descriptionValue = strings.TrimSpace(description)
	}

	var created CaseTypeVersion
	var createdConfigRaw []byte
	if err := tx.QueryRowxContext(ctx, `
		INSERT INTO case_types (
			code,
			version,
			name,
			description,
			config,
			status
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5::jsonb,
			'DRAFT'
		)
		RETURNING
			id::text,
			code,
			version,
			name,
			description,
			config,
			status,
			created_at,
			updated_at,
			activated_at,
			activated_by,
			deprecated_at,
			deprecated_by
	`, caseTypeCode, nextVersion, name, descriptionValue, rawConfig).Scan(
		&created.ID,
		&created.Code,
		&created.Version,
		&created.Name,
		&created.Description,
		&createdConfigRaw,
		&created.Status,
		&created.CreatedAt,
		&created.UpdatedAt,
		&created.ActivatedAt,
		&created.ActivatedBy,
		&created.DeprecatedAt,
		&created.DeprecatedBy,
	); err != nil {
		return CaseTypeVersion{}, fmt.Errorf("CreateCaseTypeDraftVersion: insert draft case_type: %w", err)
	}
	if err := json.Unmarshal(createdConfigRaw, &created.Config); err != nil {
		return CaseTypeVersion{}, fmt.Errorf("CreateCaseTypeDraftVersion: unmarshal created config: %w", err)
	}

	if err := RecordCaseTypeAudit(ctx, tx, CaseTypeAuditEntry{
		CaseTypeID:    created.ID,
		Action:        CaseTypeAuditActionCreated,
		Actor:         createdBy,
		ChangedFields: nil,
		PreviousValue: nil,
		NewValue:      rawConfig,
	}); err != nil {
		return CaseTypeVersion{}, fmt.Errorf("CreateCaseTypeDraftVersion: record audit CREATED: %w", err)
	}

	slog.Info("created case_type draft version",
		"case_type_id", created.ID,
		"case_type_code", created.Code,
		"version", created.Version,
		"actor", createdBy)

	if err := tx.Commit(); err != nil {
		return CaseTypeVersion{}, fmt.Errorf("CreateCaseTypeDraftVersion: commit: %w", err)
	}
	return created, nil
}

// UpdateCaseTypeDraftConfig mutates config only when version is still DRAFT.
func UpdateCaseTypeDraftConfig(
	ctx context.Context,
	db *sqlx.DB,
	caseTypeID string,
	newConfig CaseTypeConfig,
	updatedBy string,
) error {
	if db == nil {
		return fmt.Errorf("UpdateCaseTypeDraftConfig: db is nil")
	}
	caseTypeID = strings.TrimSpace(caseTypeID)
	if caseTypeID == "" {
		return fmt.Errorf("UpdateCaseTypeDraftConfig: caseTypeID is required")
	}
	if strings.TrimSpace(updatedBy) == "" {
		updatedBy = actorFromContext(ctx)
	}

	rawNew, err := json.Marshal(newConfig)
	if err != nil {
		return fmt.Errorf("UpdateCaseTypeDraftConfig: marshal new config: %w", err)
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("UpdateCaseTypeDraftConfig: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := AssertCaseTypeIsMutableTx(ctx, tx, caseTypeID); err != nil {
		return fmt.Errorf("UpdateCaseTypeDraftConfig: %w", err)
	}

	var previousRaw []byte
	if err := tx.GetContext(ctx, &previousRaw, `
		SELECT config
		FROM case_types
		WHERE id = $1::uuid
		FOR UPDATE
	`, caseTypeID); err != nil {
		return fmt.Errorf("UpdateCaseTypeDraftConfig: lock current config: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE case_types
		SET config = $1::jsonb,
		    updated_at = now()
		WHERE id = $2::uuid
	`, rawNew, caseTypeID); err != nil {
		return fmt.Errorf("UpdateCaseTypeDraftConfig: update config: %w", err)
	}

	changedFields, marshalErr := json.Marshal([]string{"config"})
	if marshalErr != nil {
		return fmt.Errorf("UpdateCaseTypeDraftConfig: marshal changed fields: %w", marshalErr)
	}

	if err := RecordCaseTypeAudit(ctx, tx, CaseTypeAuditEntry{
		CaseTypeID:    caseTypeID,
		Action:        CaseTypeAuditActionConfigUpdated,
		Actor:         updatedBy,
		ChangedFields: changedFields,
		PreviousValue: previousRaw,
		NewValue:      rawNew,
	}); err != nil {
		return fmt.Errorf("UpdateCaseTypeDraftConfig: record audit CONFIG_UPDATED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("UpdateCaseTypeDraftConfig: commit: %w", err)
	}

	slog.Info("updated DRAFT case_type config", "case_type_id", caseTypeID, "actor", updatedBy)
	return nil
}

// ActivateCaseTypeVersion atomically validates and activates a DRAFT case_type version.
func ActivateCaseTypeVersion(
	ctx context.Context,
	db *sqlx.DB,
	caseTypeID string,
	activatedBy string,
) error {
	if db == nil {
		return fmt.Errorf("ActivateCaseTypeVersion: db is nil")
	}
	caseTypeID = strings.TrimSpace(caseTypeID)
	if caseTypeID == "" {
		return fmt.Errorf("ActivateCaseTypeVersion: caseTypeID is required")
	}
	activatedBy = strings.TrimSpace(activatedBy)
	if activatedBy == "" {
		activatedBy = actorFromContext(ctx)
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("ActivateCaseTypeVersion: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	target, err := loadCaseTypeVersionByIDTx(ctx, tx, caseTypeID, true)
	if err != nil {
		return fmt.Errorf("ActivateCaseTypeVersion: load target case_type: %w", err)
	}

	if err := lockCaseTypeCodeRowsTx(ctx, tx, target.Code); err != nil {
		return fmt.Errorf("ActivateCaseTypeVersion: lock case_type code rows: %w", err)
	}

	if target.Status != CaseTypeStatusDraft {
		return fmt.Errorf("ActivateCaseTypeVersion: %w", &ImmutableCaseTypeError{CaseTypeID: caseTypeID, Status: target.Status})
	}

	if validationErr := ValidateCaseTypeConfig(ctx, target.Config); validationErr != nil {
		changedFields, _ := json.Marshal(map[string]interface{}{"validation_error": validationErr.Error()})
		newValue, _ := json.Marshal(target.Config)
		auditErr := RecordCaseTypeAudit(ctx, tx, CaseTypeAuditEntry{
			CaseTypeID:    target.ID,
			Action:        CaseTypeAuditActionValidationFailed,
			Actor:         activatedBy,
			ChangedFields: changedFields,
			PreviousValue: newValue,
			NewValue:      newValue,
		})
		if auditErr != nil {
			return fmt.Errorf("ActivateCaseTypeVersion: record validation failure audit: %w", auditErr)
		}
		return fmt.Errorf("ActivateCaseTypeVersion: %w", validationErr)
	}

	if err := validateActivitiesExistInDefinitionsTx(ctx, tx, target); err != nil {
		changedFields, _ := json.Marshal(map[string]interface{}{"validation_error": err.Error()})
		newValue, _ := json.Marshal(target.Config)
		auditErr := RecordCaseTypeAudit(ctx, tx, CaseTypeAuditEntry{
			CaseTypeID:    target.ID,
			Action:        CaseTypeAuditActionValidationFailed,
			Actor:         activatedBy,
			ChangedFields: changedFields,
			PreviousValue: newValue,
			NewValue:      newValue,
		})
		if auditErr != nil {
			return fmt.Errorf("ActivateCaseTypeVersion: record activity validation audit: %w", auditErr)
		}
		return fmt.Errorf("ActivateCaseTypeVersion: %w", err)
	}

	previousActive, err := loadActiveCaseTypeByCodeTx(ctx, tx, target.Code, target.ID, true)
	if err != nil {
		if err != ErrNoActiveVersion {
			return fmt.Errorf("ActivateCaseTypeVersion: load previous active version: %w", err)
		}
	}

	now := time.Now().UTC()
	if previousActive.ID != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE case_types
			SET status = 'DEPRECATED',
			    deprecated_at = $1,
			    deprecated_by = $2,
			    updated_at = now()
			WHERE id = $3::uuid
		`, now, activatedBy, previousActive.ID); err != nil {
			return fmt.Errorf("ActivateCaseTypeVersion: deprecate previous active version: %w", err)
		}

		if err := publishCaseTypeEvent(ctx, tx, model.EventCaseTypeDeprecated, target.Code, previousActive.ID, previousActive.Version, activatedBy, nil); err != nil {
			return fmt.Errorf("ActivateCaseTypeVersion: publish CASE_TYPE_DEPRECATED: %w", err)
		}

		previousConfigRaw, _ := json.Marshal(previousActive.Config)
		changedFields, _ := json.Marshal([]string{"status", "deprecated_at", "deprecated_by"})
		if err := RecordCaseTypeAudit(ctx, tx, CaseTypeAuditEntry{
			CaseTypeID:    previousActive.ID,
			Action:        CaseTypeAuditActionDeprecated,
			Actor:         activatedBy,
			ChangedFields: changedFields,
			PreviousValue: previousConfigRaw,
			NewValue:      previousConfigRaw,
		}); err != nil {
			return fmt.Errorf("ActivateCaseTypeVersion: record DEPRECATED audit for previous active: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE case_types
		SET status = 'ACTIVE',
		    activated_at = $1,
		    activated_by = $2,
		    updated_at = now()
		WHERE id = $3::uuid
	`, now, activatedBy, target.ID); err != nil {
		return fmt.Errorf("ActivateCaseTypeVersion: activate target version: %w", err)
	}

	if previousActive.ID != "" {
		if _, err := diffCaseTypeVersionsTx(ctx, tx, previousActive.ID, target.ID, activatedBy); err != nil {
			return fmt.Errorf("ActivateCaseTypeVersion: compute/store version diff: %w", err)
		}
	}

	if err := publishCaseTypeEvent(ctx, tx, model.EventCaseTypeActivated, target.Code, target.ID, target.Version, activatedBy, nil); err != nil {
		return fmt.Errorf("ActivateCaseTypeVersion: publish CASE_TYPE_ACTIVATED: %w", err)
	}

	targetConfigRaw, _ := json.Marshal(target.Config)
	changedFields, _ := json.Marshal([]string{"status", "activated_at", "activated_by"})
	if err := RecordCaseTypeAudit(ctx, tx, CaseTypeAuditEntry{
		CaseTypeID:    target.ID,
		Action:        CaseTypeAuditActionActivated,
		Actor:         activatedBy,
		ChangedFields: changedFields,
		PreviousValue: targetConfigRaw,
		NewValue:      targetConfigRaw,
	}); err != nil {
		return fmt.Errorf("ActivateCaseTypeVersion: record ACTIVATED audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ActivateCaseTypeVersion: commit: %w", err)
	}

	slog.Info("activated case_type version",
		"case_type_id", target.ID,
		"case_type_code", target.Code,
		"version", target.Version,
		"actor", activatedBy,
		"previous_active_id", previousActive.ID)
	return nil
}

// DeprecateCaseTypeVersion deprecates an ACTIVE version only when a replacement ACTIVE exists.
func DeprecateCaseTypeVersion(
	ctx context.Context,
	db *sqlx.DB,
	caseTypeID string,
	deprecatedBy string,
) error {
	if db == nil {
		return fmt.Errorf("DeprecateCaseTypeVersion: db is nil")
	}
	caseTypeID = strings.TrimSpace(caseTypeID)
	if caseTypeID == "" {
		return fmt.Errorf("DeprecateCaseTypeVersion: caseTypeID is required")
	}
	deprecatedBy = strings.TrimSpace(deprecatedBy)
	if deprecatedBy == "" {
		deprecatedBy = actorFromContext(ctx)
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("DeprecateCaseTypeVersion: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	target, err := loadCaseTypeVersionByIDTx(ctx, tx, caseTypeID, true)
	if err != nil {
		return fmt.Errorf("DeprecateCaseTypeVersion: load target case_type: %w", err)
	}
	if err := lockCaseTypeCodeRowsTx(ctx, tx, target.Code); err != nil {
		return fmt.Errorf("DeprecateCaseTypeVersion: lock case_type code rows: %w", err)
	}

	if target.Status != CaseTypeStatusActive {
		return fmt.Errorf("DeprecateCaseTypeVersion: only ACTIVE versions may be deprecated (current status: %s)", target.Status)
	}

	replacement, err := loadActiveCaseTypeByCodeTx(ctx, tx, target.Code, target.ID, false)
	if err != nil {
		if err == ErrNoActiveVersion {
			return fmt.Errorf("DeprecateCaseTypeVersion: %w", ErrCannotDeprecateSoleActive)
		}
		return fmt.Errorf("DeprecateCaseTypeVersion: lookup replacement active version: %w", err)
	}
	if replacement.ID == "" {
		return fmt.Errorf("DeprecateCaseTypeVersion: %w", ErrCannotDeprecateSoleActive)
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE case_types
		SET status = 'DEPRECATED',
		    deprecated_at = $1,
		    deprecated_by = $2,
		    updated_at = now()
		WHERE id = $3::uuid
	`, now, deprecatedBy, target.ID); err != nil {
		return fmt.Errorf("DeprecateCaseTypeVersion: update status to DEPRECATED: %w", err)
	}

	if err := publishCaseTypeEvent(ctx, tx, model.EventCaseTypeDeprecated, target.Code, target.ID, target.Version, deprecatedBy, map[string]interface{}{
		"replacement_case_type_id": replacement.ID,
		"replacement_version":      replacement.Version,
	}); err != nil {
		return fmt.Errorf("DeprecateCaseTypeVersion: publish CASE_TYPE_DEPRECATED: %w", err)
	}

	targetConfigRaw, _ := json.Marshal(target.Config)
	changedFields, _ := json.Marshal([]string{"status", "deprecated_at", "deprecated_by"})
	if err := RecordCaseTypeAudit(ctx, tx, CaseTypeAuditEntry{
		CaseTypeID:    target.ID,
		Action:        CaseTypeAuditActionDeprecated,
		Actor:         deprecatedBy,
		ChangedFields: changedFields,
		PreviousValue: targetConfigRaw,
		NewValue:      targetConfigRaw,
	}); err != nil {
		return fmt.Errorf("DeprecateCaseTypeVersion: record DEPRECATED audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("DeprecateCaseTypeVersion: commit: %w", err)
	}

	slog.Info("deprecated case_type version",
		"case_type_id", target.ID,
		"case_type_code", target.Code,
		"version", target.Version,
		"actor", deprecatedBy,
		"replacement_case_type_id", replacement.ID)

	return nil
}

// RecordCaseTypeAudit appends an immutable audit entry.
func RecordCaseTypeAudit(
	ctx context.Context,
	tx *sqlx.Tx,
	entry CaseTypeAuditEntry,
) error {
	if tx == nil {
		return fmt.Errorf("RecordCaseTypeAudit: tx is nil")
	}
	entry.CaseTypeID = strings.TrimSpace(entry.CaseTypeID)
	if entry.CaseTypeID == "" {
		return fmt.Errorf("RecordCaseTypeAudit: caseTypeID is required")
	}
	if strings.TrimSpace(string(entry.Action)) == "" {
		return fmt.Errorf("RecordCaseTypeAudit: action is required")
	}
	entry.Actor = strings.TrimSpace(entry.Actor)
	if entry.Actor == "" {
		entry.Actor = actorFromContext(ctx)
	}
	if entry.OccurredAt.IsZero() {
		entry.OccurredAt = time.Now().UTC()
	}

	if err := tx.QueryRowxContext(ctx, `
		INSERT INTO case_type_audit_log (
			case_type_id,
			action,
			actor,
			changed_fields,
			previous_value,
			new_value,
			occurred_at
		)
		VALUES (
			$1::uuid,
			$2,
			$3,
			$4::jsonb,
			$5::jsonb,
			$6::jsonb,
			$7
		)
		RETURNING audit_id::text
	`, entry.CaseTypeID, string(entry.Action), entry.Actor, nullableRawJSON(entry.ChangedFields), nullableRawJSON(entry.PreviousValue), nullableRawJSON(entry.NewValue), entry.OccurredAt).Scan(&entry.AuditID); err != nil {
		return fmt.Errorf("RecordCaseTypeAudit: insert audit entry: %w", err)
	}
	return nil
}

// MigrateCaseToLatestVersion migrates a non-terminal in-flight case from DEPRECATED to ACTIVE version.
func MigrateCaseToLatestVersion(
	ctx context.Context,
	db *sqlx.DB,
	caseID string,
	migratedBy string,
) error {
	if db == nil {
		return fmt.Errorf("MigrateCaseToLatestVersion: db is nil")
	}
	caseID = strings.TrimSpace(caseID)
	if caseID == "" {
		return fmt.Errorf("MigrateCaseToLatestVersion: caseID is required")
	}
	migratedBy = strings.TrimSpace(migratedBy)
	if migratedBy == "" {
		migratedBy = actorFromContext(ctx)
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("MigrateCaseToLatestVersion: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var caseRow struct {
		CaseID               string `db:"case_id"`
		Status               string `db:"status"`
		CurrentStageCode     string `db:"current_stage_code"`
		CurrentStageOrdinal  int    `db:"current_stage_ordinal"`
		PinnedCaseTypeID     string `db:"pinned_case_type_id"`
		PinnedCaseTypeCode   string `db:"pinned_case_type_code"`
		PinnedCaseTypeStatus string `db:"pinned_case_type_status"`
		PinnedVersion        int    `db:"pinned_version"`
	}

	if err := tx.GetContext(ctx, &caseRow, `
		SELECT
			c.id::text AS case_id,
			c.status,
			COALESCE(c.current_stage_code, '') AS current_stage_code,
			c.current_stage_ordinal,
			COALESCE(c.case_type_version_id, c.case_type_id)::text AS pinned_case_type_id,
			ct.code AS pinned_case_type_code,
			ct.status AS pinned_case_type_status,
			ct.version AS pinned_version
		FROM cases c
		JOIN case_types ct
		  ON ct.id = COALESCE(c.case_type_version_id, c.case_type_id)
		WHERE c.id = $1::uuid
		FOR UPDATE
	`, caseID); err != nil {
		return fmt.Errorf("MigrateCaseToLatestVersion: load and lock case: %w", err)
	}

	switch strings.ToUpper(strings.TrimSpace(caseRow.Status)) {
	case model.CaseStatusCompleted, model.CaseStatusCancelled, model.CaseStatusException:
		return fmt.Errorf("MigrateCaseToLatestVersion: %w", ErrCaseTerminalForMigration)
	}

	if CaseTypeStatus(strings.ToUpper(strings.TrimSpace(caseRow.PinnedCaseTypeStatus))) != CaseTypeStatusDeprecated {
		return fmt.Errorf("MigrateCaseToLatestVersion: pinned version status must be DEPRECATED (current %s)", caseRow.PinnedCaseTypeStatus)
	}

	if err := lockCaseTypeCodeRowsTx(ctx, tx, caseRow.PinnedCaseTypeCode); err != nil {
		return fmt.Errorf("MigrateCaseToLatestVersion: lock case_type code rows: %w", err)
	}

	activeVersion, err := loadActiveCaseTypeByCodeTx(ctx, tx, caseRow.PinnedCaseTypeCode, caseRow.PinnedCaseTypeID, true)
	if err != nil {
		return fmt.Errorf("MigrateCaseToLatestVersion: load active replacement version: %w", err)
	}
	if activeVersion.ID == caseRow.PinnedCaseTypeID {
		return fmt.Errorf("MigrateCaseToLatestVersion: %w", ErrCaseAlreadyOnActiveVersion)
	}

	targetStageOrdinal, stageFound := findStageOrdinal(activeVersion.Config, caseRow.CurrentStageCode)
	if !stageFound {
		return fmt.Errorf("MigrateCaseToLatestVersion: %w", &StageCompatibilityError{
			CaseID:              caseRow.CaseID,
			CurrentStageCode:    caseRow.CurrentStageCode,
			CurrentStageOrdinal: caseRow.CurrentStageOrdinal,
			Reason:              fmt.Sprintf("current stage %s does not exist in target version %d", caseRow.CurrentStageCode, activeVersion.Version),
		})
	}
	if targetStageOrdinal < caseRow.CurrentStageOrdinal {
		return fmt.Errorf("MigrateCaseToLatestVersion: %w", &StageCompatibilityError{
			CaseID:              caseRow.CaseID,
			CurrentStageCode:    caseRow.CurrentStageCode,
			CurrentStageOrdinal: caseRow.CurrentStageOrdinal,
			TargetStageOrdinal:  targetStageOrdinal,
			Reason:              fmt.Sprintf("target stage ordinal regression is not permitted (%d -> %d)", caseRow.CurrentStageOrdinal, targetStageOrdinal),
		})
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE cases
		SET case_type_version_id = $1::uuid,
		    case_type_id = $1::uuid,
		    case_type_version = $2,
		    updated_at = now(),
		    row_version = row_version + 1
		WHERE id = $3::uuid
	`, activeVersion.ID, activeVersion.Version, caseRow.CaseID); err != nil {
		return fmt.Errorf("MigrateCaseToLatestVersion: update case pinning: %w", err)
	}

	changedFields, _ := json.Marshal(map[string]interface{}{
		"case_id":         caseRow.CaseID,
		"from_version_id": caseRow.PinnedCaseTypeID,
		"to_version_id":   activeVersion.ID,
		"from_version":    caseRow.PinnedVersion,
		"to_version":      activeVersion.Version,
	})
	previousValue, _ := json.Marshal(map[string]interface{}{"case_type_version_id": caseRow.PinnedCaseTypeID})
	newValue, _ := json.Marshal(map[string]interface{}{"case_type_version_id": activeVersion.ID})
	if err := RecordCaseTypeAudit(ctx, tx, CaseTypeAuditEntry{
		CaseTypeID:    activeVersion.ID,
		Action:        CaseTypeAuditActionCaseMigrated,
		Actor:         migratedBy,
		ChangedFields: changedFields,
		PreviousValue: previousValue,
		NewValue:      newValue,
	}); err != nil {
		return fmt.Errorf("MigrateCaseToLatestVersion: record CASE_MIGRATED audit: %w", err)
	}

	if err := publishCaseTypeEvent(ctx, tx, model.EventCaseVersionMigrated, activeVersion.Code, activeVersion.ID, activeVersion.Version, migratedBy, map[string]interface{}{
		"case_id":         caseRow.CaseID,
		"from_version_id": caseRow.PinnedCaseTypeID,
		"to_version_id":   activeVersion.ID,
		"from_version":    caseRow.PinnedVersion,
		"to_version":      activeVersion.Version,
	}); err != nil {
		return fmt.Errorf("MigrateCaseToLatestVersion: publish CASE_VERSION_MIGRATED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("MigrateCaseToLatestVersion: commit: %w", err)
	}

	slog.Info("migrated case to latest case_type version",
		"case_id", caseRow.CaseID,
		"from_case_type_id", caseRow.PinnedCaseTypeID,
		"to_case_type_id", activeVersion.ID,
		"actor", migratedBy)

	return nil
}

func publishCaseTypeEvent(
	ctx context.Context,
	tx *sqlx.Tx,
	eventType model.EventType,
	caseTypeCode string,
	caseTypeID string,
	version int,
	actor string,
	extra map[string]interface{},
) error {
	payload := map[string]interface{}{
		"case_type_id":   caseTypeID,
		"case_type_code": caseTypeCode,
		"version":        version,
		"actor":          actor,
		"occurred_at":    time.Now().UTC(),
	}
	for k, v := range extra {
		payload[k] = v
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("publishCaseTypeEvent: marshal payload: %w", err)
	}
	if err := sla.PublishEvent(ctx, tx, model.Event{
		EventType:     eventType,
		Payload:       raw,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
	}); err != nil {
		return fmt.Errorf("publishCaseTypeEvent: %w", err)
	}
	return nil
}

func nullableRawJSON(raw json.RawMessage) interface{} {
	if len(raw) == 0 {
		return nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	return raw
}

func lockCaseTypeCodeRowsTx(ctx context.Context, tx *sqlx.Tx, caseTypeCode string) error {
	if tx == nil {
		return fmt.Errorf("lockCaseTypeCodeRowsTx: tx is nil")
	}
	caseTypeCode = strings.ToUpper(strings.TrimSpace(caseTypeCode))
	if caseTypeCode == "" {
		return fmt.Errorf("lockCaseTypeCodeRowsTx: caseTypeCode is required")
	}
	ids := make([]string, 0)
	if err := tx.SelectContext(ctx, &ids, `
		SELECT id::text
		FROM case_types
		WHERE code = $1
		FOR UPDATE
	`, caseTypeCode); err != nil {
		return fmt.Errorf("lockCaseTypeCodeRowsTx: lock rows by case_type_code %s: %w", caseTypeCode, err)
	}
	return nil
}

func loadCaseTypeVersionByIDTx(ctx context.Context, tx *sqlx.Tx, caseTypeID string, forUpdate bool) (CaseTypeVersion, error) {
	if tx == nil {
		return CaseTypeVersion{}, fmt.Errorf("loadCaseTypeVersionByIDTx: tx is nil")
	}
	caseTypeID = strings.TrimSpace(caseTypeID)
	if caseTypeID == "" {
		return CaseTypeVersion{}, fmt.Errorf("loadCaseTypeVersionByIDTx: caseTypeID is required")
	}

	query := `
		SELECT
			id::text AS id,
			code,
			version,
			name,
			description,
			config,
			status,
			created_at,
			updated_at,
			activated_at,
			activated_by,
			deprecated_at,
			deprecated_by
		FROM case_types
		WHERE id = $1::uuid
	`
	if forUpdate {
		query += " FOR UPDATE"
	}

	var row struct {
		ID           string     `db:"id"`
		Code         string     `db:"code"`
		Version      int        `db:"version"`
		Name         string     `db:"name"`
		Description  *string    `db:"description"`
		ConfigRaw    []byte     `db:"config"`
		Status       string     `db:"status"`
		CreatedAt    time.Time  `db:"created_at"`
		UpdatedAt    time.Time  `db:"updated_at"`
		ActivatedAt  *time.Time `db:"activated_at"`
		ActivatedBy  *string    `db:"activated_by"`
		DeprecatedAt *time.Time `db:"deprecated_at"`
		DeprecatedBy *string    `db:"deprecated_by"`
	}

	if err := tx.GetContext(ctx, &row, query, caseTypeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CaseTypeVersion{}, fmt.Errorf("loadCaseTypeVersionByIDTx: case_type %s not found", caseTypeID)
		}
		return CaseTypeVersion{}, fmt.Errorf("loadCaseTypeVersionByIDTx: query case_type %s: %w", caseTypeID, err)
	}

	cfg := CaseTypeConfig{}
	if err := json.Unmarshal(row.ConfigRaw, &cfg); err != nil {
		return CaseTypeVersion{}, fmt.Errorf("loadCaseTypeVersionByIDTx: unmarshal config: %w", err)
	}

	return CaseTypeVersion{
		ID:           row.ID,
		Code:         row.Code,
		Version:      row.Version,
		Name:         row.Name,
		Description:  row.Description,
		Config:       cfg,
		Status:       CaseTypeStatus(strings.ToUpper(strings.TrimSpace(row.Status))),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		ActivatedAt:  row.ActivatedAt,
		ActivatedBy:  row.ActivatedBy,
		DeprecatedAt: row.DeprecatedAt,
		DeprecatedBy: row.DeprecatedBy,
	}, nil
}

func loadActiveCaseTypeByCodeTx(
	ctx context.Context,
	tx *sqlx.Tx,
	caseTypeCode string,
	excludeCaseTypeID string,
	forUpdate bool,
) (CaseTypeVersion, error) {
	if tx == nil {
		return CaseTypeVersion{}, fmt.Errorf("loadActiveCaseTypeByCodeTx: tx is nil")
	}
	caseTypeCode = strings.ToUpper(strings.TrimSpace(caseTypeCode))
	if caseTypeCode == "" {
		return CaseTypeVersion{}, fmt.Errorf("loadActiveCaseTypeByCodeTx: caseTypeCode is required")
	}

	query := `
		SELECT
			id::text AS id,
			code,
			version,
			name,
			description,
			config,
			status,
			created_at,
			updated_at,
			activated_at,
			activated_by,
			deprecated_at,
			deprecated_by
		FROM case_types
		WHERE code = $1
		  AND status = 'ACTIVE'
	`
	args := []interface{}{caseTypeCode}
	if strings.TrimSpace(excludeCaseTypeID) != "" {
		query += " AND id <> $2::uuid"
		args = append(args, excludeCaseTypeID)
	}
	query += " ORDER BY version DESC LIMIT 1"
	if forUpdate {
		query += " FOR UPDATE"
	}

	var row struct {
		ID           string     `db:"id"`
		Code         string     `db:"code"`
		Version      int        `db:"version"`
		Name         string     `db:"name"`
		Description  *string    `db:"description"`
		ConfigRaw    []byte     `db:"config"`
		Status       string     `db:"status"`
		CreatedAt    time.Time  `db:"created_at"`
		UpdatedAt    time.Time  `db:"updated_at"`
		ActivatedAt  *time.Time `db:"activated_at"`
		ActivatedBy  *string    `db:"activated_by"`
		DeprecatedAt *time.Time `db:"deprecated_at"`
		DeprecatedBy *string    `db:"deprecated_by"`
	}
	if err := tx.GetContext(ctx, &row, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CaseTypeVersion{}, ErrNoActiveVersion
		}
		return CaseTypeVersion{}, fmt.Errorf("loadActiveCaseTypeByCodeTx: query active case_type for code %s: %w", caseTypeCode, err)
	}

	cfg := CaseTypeConfig{}
	if err := json.Unmarshal(row.ConfigRaw, &cfg); err != nil {
		return CaseTypeVersion{}, fmt.Errorf("loadActiveCaseTypeByCodeTx: unmarshal config: %w", err)
	}

	return CaseTypeVersion{
		ID:           row.ID,
		Code:         row.Code,
		Version:      row.Version,
		Name:         row.Name,
		Description:  row.Description,
		Config:       cfg,
		Status:       CaseTypeStatus(strings.ToUpper(strings.TrimSpace(row.Status))),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		ActivatedAt:  row.ActivatedAt,
		ActivatedBy:  row.ActivatedBy,
		DeprecatedAt: row.DeprecatedAt,
		DeprecatedBy: row.DeprecatedBy,
	}, nil
}

func validateActivitiesExistInDefinitionsTx(ctx context.Context, tx *sqlx.Tx, version CaseTypeVersion) error {
	if tx == nil {
		return fmt.Errorf("validateActivitiesExistInDefinitionsTx: tx is nil")
	}

	type activityDefRow struct {
		StageCode    string `db:"stage_code"`
		ActivityCode string `db:"activity_code"`
	}
	rows := make([]activityDefRow, 0)
	if err := tx.SelectContext(ctx, &rows, `
		SELECT stage_code, activity_code
		FROM activity_definitions
		WHERE case_type_id = $1::uuid
		  AND case_type_version = $2
	`, version.ID, version.Version); err != nil {
		return fmt.Errorf("validateActivitiesExistInDefinitionsTx: query activity_definitions: %w", err)
	}

	existing := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		key := strings.ToUpper(strings.TrimSpace(row.StageCode)) + "::" + strings.ToUpper(strings.TrimSpace(row.ActivityCode))
		existing[key] = struct{}{}
	}

	result := &ValidationResult{Violations: []ValidationViolation{}}
	for stageIdx, stage := range version.Config.Stages {
		for activityIdx, activity := range stage.Activities {
			key := strings.ToUpper(strings.TrimSpace(stage.Code)) + "::" + strings.ToUpper(strings.TrimSpace(activity.Code))
			if _, ok := existing[key]; !ok {
				result.Add(
					fmt.Sprintf("stages[%d].activities[%d].code", stageIdx, activityIdx),
					fmt.Sprintf("activity %s in stage %s is not materialized in activity_definitions", strings.TrimSpace(activity.Code), strings.TrimSpace(stage.Code)),
				)
			}
		}
	}

	if result.HasViolations() {
		return result
	}
	return nil
}

func findStageOrdinal(config CaseTypeConfig, stageCode string) (int, bool) {
	stageCode = strings.TrimSpace(stageCode)
	if stageCode == "" {
		return 0, false
	}
	for _, stage := range config.Stages {
		if strings.EqualFold(strings.TrimSpace(stage.Code), stageCode) {
			return stage.SequenceOrder, true
		}
	}
	return 0, false
}
