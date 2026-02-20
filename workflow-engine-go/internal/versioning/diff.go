package versioning

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type caseTypeDiffSource struct {
	Version CaseTypeVersion
}

// DiffCaseTypeVersions computes and stores a structured diff between two versions.
// If the diff already exists, the stored immutable value is returned.
func DiffCaseTypeVersions(
	ctx context.Context,
	db *sqlx.DB,
	fromVerID string,
	toVerID string,
) (CaseTypeVersionDiff, error) {
	if db == nil {
		return CaseTypeVersionDiff{}, fmt.Errorf("DiffCaseTypeVersions: db is nil")
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return CaseTypeVersionDiff{}, fmt.Errorf("DiffCaseTypeVersions: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	diff, err := diffCaseTypeVersionsTx(ctx, tx, fromVerID, toVerID, actorFromContext(ctx))
	if err != nil {
		return CaseTypeVersionDiff{}, fmt.Errorf("DiffCaseTypeVersions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return CaseTypeVersionDiff{}, fmt.Errorf("DiffCaseTypeVersions: commit: %w", err)
	}
	return diff, nil
}

func diffCaseTypeVersionsTx(
	ctx context.Context,
	tx *sqlx.Tx,
	fromVerID string,
	toVerID string,
	computedBy string,
) (CaseTypeVersionDiff, error) {
	if tx == nil {
		return CaseTypeVersionDiff{}, fmt.Errorf("diffCaseTypeVersionsTx: tx is nil")
	}
	fromVerID = strings.TrimSpace(fromVerID)
	toVerID = strings.TrimSpace(toVerID)
	if fromVerID == "" || toVerID == "" {
		return CaseTypeVersionDiff{}, fmt.Errorf("diffCaseTypeVersionsTx: fromVerID and toVerID are required")
	}

	stored, err := getStoredDiffTx(ctx, tx, fromVerID, toVerID)
	if err == nil {
		return stored, nil
	}
	if err != nil && err != ErrDiffNotFound {
		return CaseTypeVersionDiff{}, fmt.Errorf("diffCaseTypeVersionsTx: lookup existing diff: %w", err)
	}

	fromSource, err := loadCaseTypeDiffSourceTx(ctx, tx, fromVerID)
	if err != nil {
		return CaseTypeVersionDiff{}, fmt.Errorf("diffCaseTypeVersionsTx: load from version: %w", err)
	}
	toSource, err := loadCaseTypeDiffSourceTx(ctx, tx, toVerID)
	if err != nil {
		return CaseTypeVersionDiff{}, fmt.Errorf("diffCaseTypeVersionsTx: load to version: %w", err)
	}

	if !strings.EqualFold(fromSource.Version.Code, toSource.Version.Code) {
		return CaseTypeVersionDiff{}, fmt.Errorf("diffCaseTypeVersionsTx: versions belong to different case_type_code values (%s vs %s)", fromSource.Version.Code, toSource.Version.Code)
	}

	diff := computeCaseTypeVersionDiff(fromSource, toSource)
	diff.CaseTypeCode = toSource.Version.Code
	diff.FromVersionID = fromSource.Version.ID
	diff.ToVersionID = toSource.Version.ID
	diff.FromVersion = fromSource.Version.Version
	diff.ToVersion = toSource.Version.Version
	if strings.TrimSpace(computedBy) == "" {
		computedBy = "system"
	}
	diff.ComputedBy = computedBy
	diff.ComputedAt = time.Now().UTC()

	rawPayload, err := diff.MarshalPayload()
	if err != nil {
		return CaseTypeVersionDiff{}, fmt.Errorf("diffCaseTypeVersionsTx: marshal payload: %w", err)
	}

	var diffID string
	var computedAt time.Time
	insertErr := tx.QueryRowxContext(ctx, `
		INSERT INTO case_type_version_diffs (
			case_type_code,
			from_case_type_id,
			to_case_type_id,
			from_version,
			to_version,
			diff_json,
			computed_by,
			computed_at
		)
		VALUES (
			$1,
			$2::uuid,
			$3::uuid,
			$4,
			$5,
			$6::jsonb,
			$7,
			$8
		)
		ON CONFLICT (from_case_type_id, to_case_type_id)
		DO NOTHING
		RETURNING diff_id::text, computed_at
	`, diff.CaseTypeCode, diff.FromVersionID, diff.ToVersionID, diff.FromVersion, diff.ToVersion, rawPayload, diff.ComputedBy, diff.ComputedAt).Scan(&diffID, &computedAt)
	if insertErr != nil {
		if insertErr != sql.ErrNoRows {
			return CaseTypeVersionDiff{}, fmt.Errorf("diffCaseTypeVersionsTx: insert diff: %w", insertErr)
		}
		storedAfterConflict, err := getStoredDiffTx(ctx, tx, fromVerID, toVerID)
		if err != nil {
			return CaseTypeVersionDiff{}, fmt.Errorf("diffCaseTypeVersionsTx: load diff after conflict: %w", err)
		}
		return storedAfterConflict, nil
	}

	diff.DiffID = diffID
	diff.ComputedAt = computedAt
	slog.Info("stored case_type diff",
		"from_case_type_id", diff.FromVersionID,
		"to_case_type_id", diff.ToVersionID,
		"diff_id", diff.DiffID,
		"is_empty", diff.Empty())
	return diff, nil
}

func getStoredDiffTx(ctx context.Context, tx *sqlx.Tx, fromVerID string, toVerID string) (CaseTypeVersionDiff, error) {
	if tx == nil {
		return CaseTypeVersionDiff{}, fmt.Errorf("getStoredDiffTx: tx is nil")
	}

	var row struct {
		DiffID        string    `db:"diff_id"`
		CaseTypeCode  string    `db:"case_type_code"`
		FromVersionID string    `db:"from_case_type_id"`
		ToVersionID   string    `db:"to_case_type_id"`
		FromVersion   int       `db:"from_version"`
		ToVersion     int       `db:"to_version"`
		DiffJSON      []byte    `db:"diff_json"`
		ComputedBy    string    `db:"computed_by"`
		ComputedAt    time.Time `db:"computed_at"`
	}

	err := tx.GetContext(ctx, &row, `
		SELECT
			diff_id::text AS diff_id,
			case_type_code,
			from_case_type_id::text AS from_case_type_id,
			to_case_type_id::text AS to_case_type_id,
			from_version,
			to_version,
			diff_json,
			computed_by,
			computed_at
		FROM case_type_version_diffs
		WHERE from_case_type_id = $1::uuid
		  AND to_case_type_id = $2::uuid
	`, fromVerID, toVerID)
	if err != nil {
		if err == sql.ErrNoRows {
			return CaseTypeVersionDiff{}, ErrDiffNotFound
		}
		return CaseTypeVersionDiff{}, fmt.Errorf("getStoredDiffTx: query diff: %w", err)
	}

	diff := CaseTypeVersionDiff{}
	if len(row.DiffJSON) > 0 {
		if err := json.Unmarshal(row.DiffJSON, &diff); err != nil {
			return CaseTypeVersionDiff{}, fmt.Errorf("getStoredDiffTx: unmarshal diff json: %w", err)
		}
	}
	diff.DiffID = row.DiffID
	diff.CaseTypeCode = row.CaseTypeCode
	diff.FromVersionID = row.FromVersionID
	diff.ToVersionID = row.ToVersionID
	diff.FromVersion = row.FromVersion
	diff.ToVersion = row.ToVersion
	diff.ComputedBy = row.ComputedBy
	diff.ComputedAt = row.ComputedAt
	return diff, nil
}

func loadCaseTypeDiffSourceTx(ctx context.Context, tx *sqlx.Tx, caseTypeID string) (caseTypeDiffSource, error) {
	if tx == nil {
		return caseTypeDiffSource{}, fmt.Errorf("loadCaseTypeDiffSourceTx: tx is nil")
	}
	var row struct {
		ID           string     `db:"id"`
		Code         string     `db:"code"`
		Version      int        `db:"version"`
		Name         string     `db:"name"`
		Description  *string    `db:"description"`
		Status       string     `db:"status"`
		Config       []byte     `db:"config"`
		CreatedAt    time.Time  `db:"created_at"`
		UpdatedAt    time.Time  `db:"updated_at"`
		ActivatedAt  *time.Time `db:"activated_at"`
		ActivatedBy  *string    `db:"activated_by"`
		DeprecatedAt *time.Time `db:"deprecated_at"`
		DeprecatedBy *string    `db:"deprecated_by"`
	}

	if err := tx.GetContext(ctx, &row, `
		SELECT
			id::text AS id,
			code,
			version,
			name,
			description,
			status,
			config,
			created_at,
			updated_at,
			activated_at,
			activated_by,
			deprecated_at,
			deprecated_by
		FROM case_types
		WHERE id = $1::uuid
	`, caseTypeID); err != nil {
		return caseTypeDiffSource{}, fmt.Errorf("loadCaseTypeDiffSourceTx: load case_type %s: %w", caseTypeID, err)
	}

	var cfg CaseTypeConfig
	if err := json.Unmarshal(row.Config, &cfg); err != nil {
		return caseTypeDiffSource{}, fmt.Errorf("loadCaseTypeDiffSourceTx: unmarshal config: %w", err)
	}

	version := CaseTypeVersion{
		ID:           row.ID,
		Code:         row.Code,
		Version:      row.Version,
		Name:         row.Name,
		Description:  row.Description,
		Config:       cfg,
		Status:       CaseTypeStatus(row.Status),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		ActivatedAt:  row.ActivatedAt,
		ActivatedBy:  row.ActivatedBy,
		DeprecatedAt: row.DeprecatedAt,
		DeprecatedBy: row.DeprecatedBy,
	}

	return caseTypeDiffSource{Version: version}, nil
}

func computeCaseTypeVersionDiff(from caseTypeDiffSource, to caseTypeDiffSource) CaseTypeVersionDiff {
	diff := CaseTypeVersionDiff{
		StagesAdded:             []StageDefinition{},
		StagesRemoved:           []StageDefinition{},
		StagesReordered:         []StageReorder{},
		ActivitiesAdded:         []ActivityDelta{},
		ActivitiesRemoved:       []ActivityDelta{},
		TaskDefinitionsAdded:    []TaskDefinitionSnapshot{},
		TaskDefinitionsRemoved:  []TaskDefinitionSnapshot{},
		TaskDefinitionsModified: []TaskDefinitionChange{},
		RetryPolicyChanges:      []RetryPolicyChange{},
		MetadataChanges:         []MetadataChange{},
	}

	fromDesc := ""
	if from.Version.Description != nil {
		fromDesc = strings.TrimSpace(*from.Version.Description)
	}
	toDesc := ""
	if to.Version.Description != nil {
		toDesc = strings.TrimSpace(*to.Version.Description)
	}
	if strings.TrimSpace(from.Version.Name) != strings.TrimSpace(to.Version.Name) {
		diff.MetadataChanges = append(diff.MetadataChanges, MetadataChange{Field: "name", Old: from.Version.Name, New: to.Version.Name})
	}
	if fromDesc != toDesc {
		diff.MetadataChanges = append(diff.MetadataChanges, MetadataChange{Field: "description", Old: fromDesc, New: toDesc})
	}

	fromStages := make(map[string]StageDefinition, len(from.Version.Config.Stages))
	toStages := make(map[string]StageDefinition, len(to.Version.Config.Stages))

	for _, stage := range from.Version.Config.Stages {
		fromStages[stage.Code] = stage
	}
	for _, stage := range to.Version.Config.Stages {
		toStages[stage.Code] = stage
	}

	for code, stage := range toStages {
		if _, ok := fromStages[code]; !ok {
			diff.StagesAdded = append(diff.StagesAdded, stage)
		}
	}
	for code, stage := range fromStages {
		if _, ok := toStages[code]; !ok {
			diff.StagesRemoved = append(diff.StagesRemoved, stage)
		}
	}

	for code, fromStage := range fromStages {
		toStage, ok := toStages[code]
		if !ok {
			continue
		}
		if fromStage.SequenceOrder != toStage.SequenceOrder {
			diff.StagesReordered = append(diff.StagesReordered, StageReorder{
				StageCode:   code,
				FromOrdinal: fromStage.SequenceOrder,
				ToOrdinal:   toStage.SequenceOrder,
			})
		}

		fromActivityCodes := map[string]struct{}{}
		toActivityCodes := map[string]struct{}{}
		for _, activity := range fromStage.Activities {
			fromActivityCodes[activity.Code] = struct{}{}
		}
		for _, activity := range toStage.Activities {
			toActivityCodes[activity.Code] = struct{}{}
		}
		for activityCode := range toActivityCodes {
			if _, ok := fromActivityCodes[activityCode]; !ok {
				diff.ActivitiesAdded = append(diff.ActivitiesAdded, ActivityDelta{StageCode: code, ActivityCode: activityCode})
			}
		}
		for activityCode := range fromActivityCodes {
			if _, ok := toActivityCodes[activityCode]; !ok {
				diff.ActivitiesRemoved = append(diff.ActivitiesRemoved, ActivityDelta{StageCode: code, ActivityCode: activityCode})
			}
		}
	}

	fromTasks := flattenTaskDefinitions(from.Version.Config)
	toTasks := flattenTaskDefinitions(to.Version.Config)

	for taskCode, snapshot := range toTasks {
		if _, ok := fromTasks[taskCode]; !ok {
			diff.TaskDefinitionsAdded = append(diff.TaskDefinitionsAdded, snapshot)
		}
	}
	for taskCode, snapshot := range fromTasks {
		if _, ok := toTasks[taskCode]; !ok {
			diff.TaskDefinitionsRemoved = append(diff.TaskDefinitionsRemoved, snapshot)
		}
	}

	for taskCode, fromSnapshot := range fromTasks {
		toSnapshot, ok := toTasks[taskCode]
		if !ok {
			continue
		}
		if taskDefinitionDifferent(fromSnapshot.Definition, toSnapshot.Definition) ||
			fromSnapshot.StageCode != toSnapshot.StageCode ||
			fromSnapshot.ActivityCode != toSnapshot.ActivityCode {
			diff.TaskDefinitionsModified = append(diff.TaskDefinitionsModified, TaskDefinitionChange{
				TaskDefinitionCode: taskCode,
				Old:                fromSnapshot,
				New:                toSnapshot,
			})
		}

		if !retryPolicyEqual(fromSnapshot.Definition.RetryPolicy, toSnapshot.Definition.RetryPolicy) {
			diff.RetryPolicyChanges = append(diff.RetryPolicyChanges, RetryPolicyChange{
				TaskDefinitionCode: taskCode,
				Old:                cloneRetryPolicy(fromSnapshot.Definition.RetryPolicy),
				New:                cloneRetryPolicy(toSnapshot.Definition.RetryPolicy),
			})
		}
	}

	sort.Slice(diff.StagesAdded, func(i, j int) bool { return diff.StagesAdded[i].Code < diff.StagesAdded[j].Code })
	sort.Slice(diff.StagesRemoved, func(i, j int) bool { return diff.StagesRemoved[i].Code < diff.StagesRemoved[j].Code })
	sort.Slice(diff.StagesReordered, func(i, j int) bool { return diff.StagesReordered[i].StageCode < diff.StagesReordered[j].StageCode })
	sort.Slice(diff.ActivitiesAdded, func(i, j int) bool {
		if diff.ActivitiesAdded[i].StageCode == diff.ActivitiesAdded[j].StageCode {
			return diff.ActivitiesAdded[i].ActivityCode < diff.ActivitiesAdded[j].ActivityCode
		}
		return diff.ActivitiesAdded[i].StageCode < diff.ActivitiesAdded[j].StageCode
	})
	sort.Slice(diff.ActivitiesRemoved, func(i, j int) bool {
		if diff.ActivitiesRemoved[i].StageCode == diff.ActivitiesRemoved[j].StageCode {
			return diff.ActivitiesRemoved[i].ActivityCode < diff.ActivitiesRemoved[j].ActivityCode
		}
		return diff.ActivitiesRemoved[i].StageCode < diff.ActivitiesRemoved[j].StageCode
	})
	sort.Slice(diff.TaskDefinitionsAdded, func(i, j int) bool {
		return diff.TaskDefinitionsAdded[i].TaskDefinitionCode < diff.TaskDefinitionsAdded[j].TaskDefinitionCode
	})
	sort.Slice(diff.TaskDefinitionsRemoved, func(i, j int) bool {
		return diff.TaskDefinitionsRemoved[i].TaskDefinitionCode < diff.TaskDefinitionsRemoved[j].TaskDefinitionCode
	})
	sort.Slice(diff.TaskDefinitionsModified, func(i, j int) bool {
		return diff.TaskDefinitionsModified[i].TaskDefinitionCode < diff.TaskDefinitionsModified[j].TaskDefinitionCode
	})
	sort.Slice(diff.RetryPolicyChanges, func(i, j int) bool {
		return diff.RetryPolicyChanges[i].TaskDefinitionCode < diff.RetryPolicyChanges[j].TaskDefinitionCode
	})
	sort.Slice(diff.MetadataChanges, func(i, j int) bool {
		return diff.MetadataChanges[i].Field < diff.MetadataChanges[j].Field
	})

	return diff
}

func flattenTaskDefinitions(config CaseTypeConfig) map[string]TaskDefinitionSnapshot {
	result := make(map[string]TaskDefinitionSnapshot)
	for _, stage := range config.Stages {
		stageCode := strings.TrimSpace(stage.Code)
		for _, activity := range stage.Activities {
			activityCode := strings.TrimSpace(activity.Code)
			for _, taskDef := range activity.TaskDefs {
				taskCode := strings.TrimSpace(taskDef.Code)
				if taskCode == "" {
					continue
				}
				result[taskCode] = TaskDefinitionSnapshot{
					TaskDefinitionCode: taskCode,
					StageCode:          stageCode,
					ActivityCode:       activityCode,
					Definition:         taskDef,
				}
			}
		}
	}
	return result
}

func taskDefinitionDifferent(a TaskDefinition, b TaskDefinition) bool {
	aRaw, err := json.Marshal(a)
	if err != nil {
		slog.Warn("taskDefinitionDifferent: marshal old failed", "error", err)
		return true
	}
	bRaw, err := json.Marshal(b)
	if err != nil {
		slog.Warn("taskDefinitionDifferent: marshal new failed", "error", err)
		return true
	}
	return !bytes.Equal(aRaw, bRaw)
}

func cloneRetryPolicy(policy *RetryPolicy) *RetryPolicy {
	if policy == nil {
		return nil
	}
	cloned := *policy
	if len(policy.RetryableErrorCodes) > 0 {
		cloned.RetryableErrorCodes = append([]string(nil), policy.RetryableErrorCodes...)
	}
	return &cloned
}

func retryPolicyEqual(a *RetryPolicy, b *RetryPolicy) bool {
	if a == nil && b == nil {
		return true
	}
	if (a == nil) != (b == nil) {
		return false
	}
	if a.MaxRetries != b.MaxRetries ||
		a.BackoffStrategy != b.BackoffStrategy ||
		a.BaseIntervalSeconds != b.BaseIntervalSeconds ||
		a.MaxIntervalSeconds != b.MaxIntervalSeconds {
		return false
	}
	if len(a.RetryableErrorCodes) != len(b.RetryableErrorCodes) {
		return false
	}
	for i := range a.RetryableErrorCodes {
		if strings.TrimSpace(a.RetryableErrorCodes[i]) != strings.TrimSpace(b.RetryableErrorCodes[i]) {
			return false
		}
	}
	return true
}
