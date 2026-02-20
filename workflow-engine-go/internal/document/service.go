package document

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"workflow-engine/internal/sla"
	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

type caseContext struct {
	CaseTypeCode    string
	CaseTypeVersion int
	CurrentStage    string
}

type documentTypeConfig struct {
	model.DocumentType
}

// UpsertDocumentTypesFromConfig materializes case_type.config.document_types into document_types table.
func UpsertDocumentTypesFromConfig(ctx context.Context, tx *sqlx.Tx, caseType model.CaseType) error {
	if tx == nil {
		return fmt.Errorf("UpsertDocumentTypesFromConfig: tx is nil")
	}
	for _, definition := range caseType.Config.DocumentTypes {
		documentTypeCode := strings.TrimSpace(definition.DocumentTypeCode)
		if documentTypeCode == "" {
			return fmt.Errorf("UpsertDocumentTypesFromConfig: document_type_code is required")
		}

		allowedExtensions := normalizeExtensions(definition.AllowedExtensions)
		if len(allowedExtensions) == 0 {
			return fmt.Errorf("UpsertDocumentTypesFromConfig: allowed_extensions is required for %s", documentTypeCode)
		}
		maxSizeMB := definition.MaxSizeMB
		if maxSizeMB <= 0 {
			maxSizeMB = 10
		}
		requiredCountMin := definition.RequiredCountMin
		if requiredCountMin <= 0 {
			requiredCountMin = 1
		}
		requiredCountMax := definition.RequiredCountMax
		if requiredCountMax <= 0 || requiredCountMax < requiredCountMin {
			requiredCountMax = requiredCountMin
		}
		retentionDays := definition.RetentionDays
		if retentionDays <= 0 {
			retentionDays = 2555
		}

		retentionPolicy := model.DocumentRetentionPolicy(strings.ToUpper(strings.TrimSpace(definition.RetentionPolicy)))
		if retentionPolicy == "" {
			retentionPolicy = model.DocumentRetentionPolicyArchive
		}
		if retentionPolicy != model.DocumentRetentionPolicyArchive && retentionPolicy != model.DocumentRetentionPolicyDelete {
			return fmt.Errorf("UpsertDocumentTypesFromConfig: invalid retention policy %s for %s", retentionPolicy, documentTypeCode)
		}

		allowedViewers := normalizeRoles(definition.AllowedViewers)
		if len(allowedViewers) == 0 {
			allowedViewers = []string{"PUBLIC"}
		}

		var requiredAtStage interface{}
		if stage := strings.TrimSpace(definition.RequiredAtStage); stage != "" {
			requiredAtStage = stage
		} else {
			requiredAtStage = nil
		}

		var verificationRole interface{}
		if definition.RequiresVerification {
			role := strings.TrimSpace(definition.VerificationRole)
			if role == "" {
				role = "DOCUMENT_REVIEWER"
			}
			verificationRole = role
		} else {
			verificationRole = nil
		}

		_, err := tx.ExecContext(ctx, `
			INSERT INTO document_types (
				case_type_code,
				case_type_version,
				document_type_code,
				display_name,
				description,
				allowed_extensions,
				max_size_mb,
				required_at_stage,
				required_count_min,
				required_count_max,
				is_sensitive,
				requires_verification,
				verification_role,
				retention_days,
				retention_policy,
				allowed_viewers
			)
			VALUES (
				$1,
				$2,
				$3,
				$4,
				$5,
				$6,
				$7,
				$8,
				$9,
				$10,
				$11,
				$12,
				$13,
				$14,
				$15,
				$16
			)
			ON CONFLICT (case_type_code, case_type_version, document_type_code)
			DO UPDATE SET
				display_name = EXCLUDED.display_name,
				description = EXCLUDED.description,
				allowed_extensions = EXCLUDED.allowed_extensions,
				max_size_mb = EXCLUDED.max_size_mb,
				required_at_stage = EXCLUDED.required_at_stage,
				required_count_min = EXCLUDED.required_count_min,
				required_count_max = EXCLUDED.required_count_max,
				is_sensitive = EXCLUDED.is_sensitive,
				requires_verification = EXCLUDED.requires_verification,
				verification_role = EXCLUDED.verification_role,
				retention_days = EXCLUDED.retention_days,
				retention_policy = EXCLUDED.retention_policy,
				allowed_viewers = EXCLUDED.allowed_viewers,
				updated_at = now()
		`,
			caseType.Code,
			caseType.Version,
			documentTypeCode,
			strings.TrimSpace(definition.DisplayName),
			nilIfBlank(definition.Description),
			allowedExtensions,
			maxSizeMB,
			requiredAtStage,
			requiredCountMin,
			requiredCountMax,
			definition.IsSensitive,
			definition.RequiresVerification,
			verificationRole,
			retentionDays,
			string(retentionPolicy),
			allowedViewers,
		)
		if err != nil {
			return fmt.Errorf("UpsertDocumentTypesFromConfig: upsert %s: %w", documentTypeCode, err)
		}
	}
	return nil
}

// InitializeDocumentRequestsForStage inserts placeholder requests for required document types.
func InitializeDocumentRequestsForStage(
	ctx context.Context,
	tx *sqlx.Tx,
	caseID string,
	caseTypeCode string,
	caseTypeVersion int,
	stageCode string,
) error {
	if tx == nil {
		return fmt.Errorf("InitializeDocumentRequestsForStage: tx is nil")
	}
	if strings.TrimSpace(stageCode) == "" {
		return nil
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO document_requests (
			case_id,
			case_type_code,
			case_type_version,
			document_type_code,
			required_at_stage,
			required_count_min,
			required_count_max,
			current_count,
			status,
			requested_at
		)
		SELECT
			$1::uuid,
			dt.case_type_code,
			dt.case_type_version,
			dt.document_type_code,
			dt.required_at_stage,
			dt.required_count_min,
			dt.required_count_max,
			0,
			'PENDING',
			now()
		FROM document_types dt
		WHERE dt.case_type_code = $2
		  AND dt.case_type_version = $3
		  AND dt.required_at_stage = $4
		  AND dt.required_count_min > 0
		ON CONFLICT (case_id, document_type_code, required_at_stage)
		DO NOTHING
	`, caseID, caseTypeCode, caseTypeVersion, stageCode)
	if err != nil {
		return fmt.Errorf("InitializeDocumentRequestsForStage: %w", err)
	}
	return nil
}

// UploadDocument handles metadata validation, storage upload, and event emission.
func UploadDocument(
	ctx context.Context,
	tx *sqlx.Tx,
	storage DocumentStorage,
	caseID string,
	docType string,
	file io.Reader,
	metadata DocumentUploadMetadata,
) (model.Document, error) {
	if tx == nil {
		return model.Document{}, fmt.Errorf("UploadDocument: tx is nil")
	}
	if storage == nil {
		return model.Document{}, fmt.Errorf("UploadDocument: storage is nil")
	}
	if strings.TrimSpace(caseID) == "" {
		return model.Document{}, fmt.Errorf("UploadDocument: caseID is required")
	}
	if strings.TrimSpace(docType) == "" {
		return model.Document{}, fmt.Errorf("UploadDocument: docType is required")
	}
	if strings.TrimSpace(metadata.UploadedBy) == "" {
		return model.Document{}, fmt.Errorf("UploadDocument: uploaded_by is required")
	}

	ctxCase, err := loadCaseContextTx(ctx, tx, caseID)
	if err != nil {
		return model.Document{}, fmt.Errorf("UploadDocument: %w", err)
	}
	docDefinition, err := loadDocumentTypeTx(ctx, tx, ctxCase.CaseTypeCode, ctxCase.CaseTypeVersion, docType)
	if err != nil {
		return model.Document{}, fmt.Errorf("UploadDocument: %w", err)
	}

	filename := strings.TrimSpace(metadata.Filename)
	if filename == "" {
		return model.Document{}, fmt.Errorf("UploadDocument: filename is required")
	}
	extension := normalizeExtension(metadata.FileExtension)
	if extension == "" {
		extension = extensionFromFilename(filename)
	}
	if extension == "" {
		return model.Document{}, fmt.Errorf("UploadDocument: unable to determine file extension for %s", filename)
	}
	if !containsNormalized(docDefinition.AllowedExtensions, extension) {
		return model.Document{}, fmt.Errorf("UploadDocument: extension %s not allowed for %s", extension, docDefinition.DocumentTypeCode)
	}

	tmpFile, err := os.CreateTemp("", "case-document-*")
	if err != nil {
		return model.Document{}, fmt.Errorf("UploadDocument: create temp file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
	}()

	hasher := sha256.New()
	bytesWritten, err := io.Copy(io.MultiWriter(tmpFile, hasher), file)
	if err != nil {
		return model.Document{}, fmt.Errorf("UploadDocument: read source file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return model.Document{}, fmt.Errorf("UploadDocument: sync temp file: %w", err)
	}
	if metadata.FileSizeBytes > 0 && metadata.FileSizeBytes != bytesWritten {
		return model.Document{}, fmt.Errorf("UploadDocument: file_size_bytes mismatch expected=%d actual=%d", metadata.FileSizeBytes, bytesWritten)
	}

	maxSizeBytes := int64(docDefinition.MaxSizeMB) * 1024 * 1024
	if bytesWritten > maxSizeBytes {
		return model.Document{}, fmt.Errorf("UploadDocument: file exceeds max_size_mb (%dMB)", docDefinition.MaxSizeMB)
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))

	var documentID string
	if err := tx.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&documentID); err != nil {
		return model.Document{}, fmt.Errorf("UploadDocument: generate document id: %w", err)
	}

	stageCode := strings.TrimSpace(metadata.StageCode)
	if stageCode == "" {
		stageCode = ctxCase.CurrentStage
	}
	if stageCode == "" {
		stageCode = "UNKNOWN_STAGE"
	}

	storageProvider := metadata.StorageProvider
	if storageProvider == "" {
		storageProvider = inferStorageProvider(storage)
	}
	if storageProvider == "" {
		storageProvider = model.DocumentStorageProviderLocal
	}

	bucket := strings.TrimSpace(metadata.StorageBucket)
	if bucket == "" {
		bucket = "documents"
	}
	key := fmt.Sprintf("%s/%s.%s", caseID, documentID, extension)

	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return model.Document{}, fmt.Errorf("UploadDocument: rewind temp file: %w", err)
	}

	storagePath, storageURL, err := storage.Upload(ctx, bucket, key, tmpFile, map[string]string{
		"case_id":            caseID,
		"document_id":        documentID,
		"document_type_code": strings.TrimSpace(docType),
		"checksum_sha256":    checksum,
	})
	if err != nil {
		return model.Document{}, fmt.Errorf("UploadDocument: storage upload: %w", err)
	}

	version := 1
	var supersedesID interface{}
	if metadata.SupersedesDocumentID != nil && strings.TrimSpace(*metadata.SupersedesDocumentID) != "" {
		oldID := strings.TrimSpace(*metadata.SupersedesDocumentID)
		var oldVersion int
		var oldStatus string
		err := tx.QueryRowContext(ctx, `
			SELECT version, status
			FROM case_documents
			WHERE id = $1::uuid
			  AND case_id = $2::uuid
			  AND document_type_code = $3
			FOR UPDATE
		`, oldID, caseID, docType).Scan(&oldVersion, &oldStatus)
		if err != nil {
			return model.Document{}, fmt.Errorf("UploadDocument: lock superseded document %s: %w", oldID, err)
		}
		version = oldVersion + 1
		supersedesID = oldID
	}

	metadataJSON := jsonRawOrDefault(metadata.Metadata)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO case_documents (
			id,
			case_id,
			task_id,
			stage_code,
			case_type_code,
			case_type_version,
			document_type_code,
			filename,
			file_extension,
			file_size_bytes,
			storage_provider,
			storage_path,
			storage_url,
			checksum_sha256,
			uploaded_by,
			uploaded_at,
			status,
			metadata,
			version,
			created_at,
			updated_at
		)
		VALUES (
			$1::uuid,
			$2::uuid,
			$3::uuid,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			$11,
			$12,
			$13,
			$14,
			$15,
			now(),
			$16,
			$17::jsonb,
			$18,
			now(),
			now()
		)
	`,
		documentID,
		caseID,
		nilIfBlankPtr(metadata.TaskID),
		stageCode,
		ctxCase.CaseTypeCode,
		ctxCase.CaseTypeVersion,
		docDefinition.DocumentTypeCode,
		filename,
		extension,
		bytesWritten,
		string(storageProvider),
		storagePath,
		nilIfBlank(storageURL),
		checksum,
		metadata.UploadedBy,
		string(model.DocumentStatusUploaded),
		metadataJSON,
		version,
	); err != nil {
		return model.Document{}, fmt.Errorf("UploadDocument: insert case_documents: %w", err)
	}

	if supersedesID != nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE case_documents
			SET status = $1,
			    superseded_by_document_id = $2::uuid,
			    updated_at = now()
			WHERE id = $3::uuid
		`, string(model.DocumentStatusArchived), documentID, supersedesID); err != nil {
			return model.Document{}, fmt.Errorf("UploadDocument: mark superseded document archived: %w", err)
		}
		payload, _ := json.Marshal(map[string]interface{}{
			"case_id":                 caseID,
			"document_id":             documentID,
			"supersedes_document_id":  supersedesID,
			"document_type_code":      docDefinition.DocumentTypeCode,
			"version":                 version,
			"event_reason":            "document_version_created",
		})
		if err := sla.PublishEvent(ctx, tx, model.Event{
			CaseID:        &caseID,
			EventType:     model.EventDocumentVersionCreated,
			Payload:       payload,
			Status:        model.EventStatusPending,
			TargetService: targetServiceOrDefault(metadata.TargetService),
		}); err != nil {
			return model.Document{}, fmt.Errorf("UploadDocument: publish DOCUMENT_VERSION_CREATED: %w", err)
		}
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"case_id":             caseID,
		"document_id":         documentID,
		"document_type_code":  docDefinition.DocumentTypeCode,
		"stage_code":          stageCode,
		"uploaded_by":         metadata.UploadedBy,
		"checksum_sha256":     checksum,
		"storage_path":        storagePath,
		"file_size_bytes":     bytesWritten,
		"requires_verification": docDefinition.RequiresVerification,
	})
	if err := sla.PublishEvent(ctx, tx, model.Event{
		CaseID:        &caseID,
		EventType:     model.EventDocumentUploaded,
		Payload:       payload,
		Status:        model.EventStatusPending,
		TargetService: targetServiceOrDefault(metadata.TargetService),
	}); err != nil {
		return model.Document{}, fmt.Errorf("UploadDocument: publish DOCUMENT_UPLOADED: %w", err)
	}

	if docDefinition.RequiresVerification {
		verificationMetadata := metadata
		verificationMetadata.StageCode = stageCode
		if err := createVerificationTask(ctx, tx, caseID, documentID, docDefinition, verificationMetadata); err != nil {
			return model.Document{}, fmt.Errorf("UploadDocument: create verification task: %w", err)
		}
	} else {
		fulfilledNow, err := refreshDocumentRequestStatus(ctx, tx, caseID, ctxCase.CaseTypeCode, ctxCase.CaseTypeVersion, docDefinition.DocumentTypeCode)
		if err != nil {
			return model.Document{}, fmt.Errorf("UploadDocument: refresh document request: %w", err)
		}
		if fulfilledNow {
			fulfillmentPayload, _ := json.Marshal(map[string]interface{}{
				"case_id":            caseID,
				"document_type_code": docDefinition.DocumentTypeCode,
				"event_reason":       "requirement_fulfilled_after_upload",
			})
			if err := sla.PublishEvent(ctx, tx, model.Event{
				CaseID:        &caseID,
				EventType:     model.EventDocumentRequirementFulfilled,
				Payload:       fulfillmentPayload,
				Status:        model.EventStatusPending,
				TargetService: targetServiceOrDefault(metadata.TargetService),
			}); err != nil {
				return model.Document{}, fmt.Errorf("UploadDocument: publish DOCUMENT_REQUIREMENT_FULFILLED: %w", err)
			}
		}
	}

	document, err := loadDocumentByIDTx(ctx, tx, documentID)
	if err != nil {
		return model.Document{}, fmt.Errorf("UploadDocument: load inserted document: %w", err)
	}
	return document, nil
}

// GetDocument loads document metadata after enforcing viewer-role access.
func GetDocument(
	ctx context.Context,
	db *sqlx.DB,
	documentID string,
	requestor Actor,
) (model.Document, error) {
	if db == nil {
		return model.Document{}, fmt.Errorf("GetDocument: db is nil")
	}
	var row struct {
		model.Document
		AllowedViewersJSON string `db:"allowed_viewers_json"`
	}

	err := db.QueryRowxContext(ctx, `
		SELECT
			cd.id::text AS id,
			cd.case_id::text AS case_id,
			cd.task_id::text AS task_id,
			cd.stage_code,
			cd.case_type_code,
			cd.case_type_version,
			cd.document_type_code,
			cd.filename,
			cd.file_extension,
			cd.file_size_bytes,
			cd.storage_provider,
			cd.storage_path,
			cd.storage_url,
			cd.checksum_sha256,
			cd.uploaded_by,
			cd.uploaded_at,
			cd.status,
			cd.rejection_reason,
			cd.verified_by,
			cd.verified_at,
			cd.metadata,
			cd.version,
			cd.superseded_by_document_id::text AS superseded_by_document_id,
			cd.legal_hold,
			cd.created_at,
			cd.updated_at,
			COALESCE(array_to_json(dt.allowed_viewers)::text, '[]') AS allowed_viewers_json
		FROM case_documents cd
		JOIN document_types dt
		  ON dt.case_type_code = cd.case_type_code
		 AND dt.case_type_version = cd.case_type_version
		 AND dt.document_type_code = cd.document_type_code
		WHERE cd.id = $1::uuid
	`, documentID).StructScan(&row)
	if err != nil {
		return model.Document{}, fmt.Errorf("GetDocument: query document: %w", err)
	}

	var allowedViewers []string
	if err := json.Unmarshal([]byte(row.AllowedViewersJSON), &allowedViewers); err != nil {
		return model.Document{}, fmt.Errorf("GetDocument: decode allowed_viewers: %w", err)
	}
	roleUpper := strings.ToUpper(strings.TrimSpace(requestor.Role))
	if !requestor.IsSystem && !containsRole(allowedViewers, "PUBLIC") && !containsRole(allowedViewers, roleUpper) {
		return model.Document{}, &AuthorizationError{
			Resource: "document",
			Reason:   fmt.Sprintf("role %s not allowed to view %s", roleUpper, row.Document.DocumentTypeCode),
		}
	}
	return row.Document, nil
}

// DeleteDocument marks a document metadata row as DELETED (soft delete).
func DeleteDocument(
	ctx context.Context,
	tx *sqlx.Tx,
	documentID string,
	reason string,
) error {
	if tx == nil {
		return fmt.Errorf("DeleteDocument: tx is nil")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("DeleteDocument: reason is required")
	}
	var (
		caseID          string
		caseTypeCode    string
		caseTypeVersion int
		documentType    string
		currentStatus   string
	)
	err := tx.QueryRowContext(ctx, `
		SELECT case_id::text, case_type_code, case_type_version, document_type_code, status
		FROM case_documents
		WHERE id = $1::uuid
		FOR UPDATE
	`, documentID).Scan(&caseID, &caseTypeCode, &caseTypeVersion, &documentType, &currentStatus)
	if err != nil {
		return fmt.Errorf("DeleteDocument: lock document: %w", err)
	}
	if model.DocumentStatus(currentStatus) == model.DocumentStatusDeleted {
		return nil
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE case_documents
		SET status = $1,
		    metadata = jsonb_set(COALESCE(metadata, '{}'::jsonb), '{deletion_reason}', to_jsonb($2::text), true),
		    updated_at = now()
		WHERE id = $3::uuid
	`, string(model.DocumentStatusDeleted), reason, documentID)
	if err != nil {
		return fmt.Errorf("DeleteDocument: update status: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE document_verification_tasks
		SET status = 'CANCELLED',
		    reason = $1,
		    updated_at = now()
		WHERE document_id = $2::uuid
		  AND status = 'PENDING'
	`, reason, documentID)
	if err != nil {
		return fmt.Errorf("DeleteDocument: cancel verification task mapping: %w", err)
	}

	if _, err := refreshDocumentRequestStatus(ctx, tx, caseID, caseTypeCode, caseTypeVersion, documentType); err != nil {
		return fmt.Errorf("DeleteDocument: refresh request status: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"case_id":            caseID,
		"document_id":        documentID,
		"document_type_code": documentType,
		"reason":             reason,
	})
	if err := sla.PublishEvent(ctx, tx, model.Event{
		CaseID:        &caseID,
		EventType:     model.EventDocumentDeleted,
		Payload:       payload,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
	}); err != nil {
		return fmt.Errorf("DeleteDocument: publish DOCUMENT_DELETED: %w", err)
	}
	return nil
}

// ApproveDocument marks a document as VERIFIED and updates requirement counts.
func ApproveDocument(
	ctx context.Context,
	tx *sqlx.Tx,
	documentID string,
	verifierID string,
) error {
	if tx == nil {
		return fmt.Errorf("ApproveDocument: tx is nil")
	}
	if strings.TrimSpace(verifierID) == "" {
		return fmt.Errorf("ApproveDocument: verifierID is required")
	}

	var (
		caseID          string
		caseTypeCode    string
		caseTypeVersion int
		documentType    string
		currentStatus   string
	)
	err := tx.QueryRowContext(ctx, `
		SELECT case_id::text, case_type_code, case_type_version, document_type_code, status
		FROM case_documents
		WHERE id = $1::uuid
		FOR UPDATE
	`, documentID).Scan(&caseID, &caseTypeCode, &caseTypeVersion, &documentType, &currentStatus)
	if err != nil {
		return fmt.Errorf("ApproveDocument: load document: %w", err)
	}

	status := model.DocumentStatus(currentStatus)
	if status == model.DocumentStatusVerified {
		return fmt.Errorf("ApproveDocument: %w: document already verified", ErrInvalidDocumentOperation)
	}
	if status == model.DocumentStatusDeleted || status == model.DocumentStatusArchived {
		return fmt.Errorf("ApproveDocument: %w: document status %s cannot be approved", ErrInvalidDocumentOperation, status)
	}

	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `
		UPDATE case_documents
		SET status = 'VERIFIED',
		    verified_by = $1,
		    verified_at = $2,
		    rejection_reason = NULL,
		    updated_at = now()
		WHERE id = $3::uuid
	`, verifierID, now, documentID)
	if err != nil {
		return fmt.Errorf("ApproveDocument: update document: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE document_verification_tasks
		SET status = 'APPROVED',
		    verifier_id = $1,
		    verified_at = $2,
		    updated_at = now()
		WHERE document_id = $3::uuid
		  AND status = 'PENDING'
	`, verifierID, now, documentID)
	if err != nil {
		return fmt.Errorf("ApproveDocument: update verification mapping: %w", err)
	}

	fulfilledNow, err := refreshDocumentRequestStatus(ctx, tx, caseID, caseTypeCode, caseTypeVersion, documentType)
	if err != nil {
		return fmt.Errorf("ApproveDocument: refresh request status: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"case_id":            caseID,
		"document_id":        documentID,
		"document_type_code": documentType,
		"verified_by":        verifierID,
		"verified_at":        now,
	})
	if err := sla.PublishEvent(ctx, tx, model.Event{
		CaseID:        &caseID,
		EventType:     model.EventDocumentVerified,
		Payload:       payload,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
	}); err != nil {
		return fmt.Errorf("ApproveDocument: publish DOCUMENT_VERIFIED: %w", err)
	}

	if fulfilledNow {
		fulfilledPayload, _ := json.Marshal(map[string]interface{}{
			"case_id":            caseID,
			"document_type_code": documentType,
		})
		if err := sla.PublishEvent(ctx, tx, model.Event{
			CaseID:        &caseID,
			EventType:     model.EventDocumentRequirementFulfilled,
			Payload:       fulfilledPayload,
			Status:        model.EventStatusPending,
			TargetService: "case-orchestrator",
		}); err != nil {
			return fmt.Errorf("ApproveDocument: publish DOCUMENT_REQUIREMENT_FULFILLED: %w", err)
		}
	}

	return nil
}

// RejectDocument marks a document as REJECTED and emits DOCUMENT_REJECTED.
func RejectDocument(
	ctx context.Context,
	tx *sqlx.Tx,
	documentID string,
	verifierID string,
	reason string,
) error {
	if tx == nil {
		return fmt.Errorf("RejectDocument: tx is nil")
	}
	if strings.TrimSpace(verifierID) == "" {
		return fmt.Errorf("RejectDocument: verifierID is required")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("RejectDocument: reason is required")
	}

	var (
		caseID          string
		caseTypeCode    string
		caseTypeVersion int
		documentType    string
		status          string
	)
	err := tx.QueryRowContext(ctx, `
		SELECT case_id::text, case_type_code, case_type_version, document_type_code, status
		FROM case_documents
		WHERE id = $1::uuid
		FOR UPDATE
	`, documentID).Scan(&caseID, &caseTypeCode, &caseTypeVersion, &documentType, &status)
	if err != nil {
		return fmt.Errorf("RejectDocument: load document: %w", err)
	}
	current := model.DocumentStatus(status)
	if current == model.DocumentStatusDeleted || current == model.DocumentStatusArchived {
		return fmt.Errorf("RejectDocument: %w: document status %s cannot be rejected", ErrInvalidDocumentOperation, current)
	}

	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `
		UPDATE case_documents
		SET status = 'REJECTED',
		    rejection_reason = $1,
		    verified_by = $2,
		    verified_at = $3,
		    updated_at = now()
		WHERE id = $4::uuid
	`, reason, verifierID, now, documentID)
	if err != nil {
		return fmt.Errorf("RejectDocument: update document: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE document_verification_tasks
		SET status = 'REJECTED',
		    verifier_id = $1,
		    verified_at = $2,
		    reason = $3,
		    updated_at = now()
		WHERE document_id = $4::uuid
		  AND status = 'PENDING'
	`, verifierID, now, reason, documentID)
	if err != nil {
		return fmt.Errorf("RejectDocument: update verification mapping: %w", err)
	}

	if _, err := refreshDocumentRequestStatus(ctx, tx, caseID, caseTypeCode, caseTypeVersion, documentType); err != nil {
		return fmt.Errorf("RejectDocument: refresh request status: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"case_id":            caseID,
		"document_id":        documentID,
		"document_type_code": documentType,
		"reason":             reason,
		"rejected_by":        verifierID,
	})
	if err := sla.PublishEvent(ctx, tx, model.Event{
		CaseID:        &caseID,
		EventType:     model.EventDocumentRejected,
		Payload:       payload,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
	}); err != nil {
		return fmt.Errorf("RejectDocument: publish DOCUMENT_REJECTED: %w", err)
	}
	return nil
}

// CheckDocumentRequirements verifies stage-level requirements and returns missing types.
func CheckDocumentRequirements(
	ctx context.Context,
	db *sqlx.DB,
	caseID string,
	stageCode string,
) (fulfilled bool, missing []model.DocumentType, err error) {
	if db == nil {
		return false, nil, fmt.Errorf("CheckDocumentRequirements: db is nil")
	}
	stageCode = strings.TrimSpace(stageCode)
	if stageCode == "" {
		return true, nil, nil
	}

	type requirementRow struct {
		ID                   string `db:"id"`
		CaseTypeCode         string `db:"case_type_code"`
		CaseTypeVersion      int    `db:"case_type_version"`
		DocumentTypeCode     string `db:"document_type_code"`
		DisplayName          string `db:"display_name"`
		Description          *string `db:"description"`
		AllowedExtensionsJSON string `db:"allowed_extensions_json"`
		MaxSizeMB            int    `db:"max_size_mb"`
		RequiredAtStage      *string `db:"required_at_stage"`
		RequiredCountMin     int    `db:"required_count_min"`
		RequiredCountMax     int    `db:"required_count_max"`
		IsSensitive          bool   `db:"is_sensitive"`
		RequiresVerification bool   `db:"requires_verification"`
		VerificationRole     *string `db:"verification_role"`
		RetentionDays        int    `db:"retention_days"`
		RetentionPolicy      string `db:"retention_policy"`
		AllowedViewersJSON   string `db:"allowed_viewers_json"`
		CurrentCount         int    `db:"current_count"`
		RequestStatus        string `db:"request_status"`
	}

	var rows []requirementRow
	if err := db.SelectContext(ctx, &rows, `
		SELECT
			dt.id::text AS id,
			dt.case_type_code,
			dt.case_type_version,
			dt.document_type_code,
			dt.display_name,
			dt.description,
			COALESCE(array_to_json(dt.allowed_extensions)::text, '[]') AS allowed_extensions_json,
			dt.max_size_mb,
			dt.required_at_stage,
			dt.required_count_min,
			dt.required_count_max,
			dt.is_sensitive,
			dt.requires_verification,
			dt.verification_role,
			dt.retention_days,
			dt.retention_policy,
			COALESCE(array_to_json(dt.allowed_viewers)::text, '[]') AS allowed_viewers_json,
			dr.current_count,
			dr.status AS request_status
		FROM document_requests dr
		JOIN document_types dt
		  ON dt.case_type_code = dr.case_type_code
		 AND dt.case_type_version = dr.case_type_version
		 AND dt.document_type_code = dr.document_type_code
		WHERE dr.case_id = $1::uuid
		  AND dr.required_at_stage = $2
		  AND dr.status NOT IN ('FULFILLED', 'WAIVED')
		ORDER BY dr.document_type_code ASC
	`, caseID, stageCode); err != nil {
		return false, nil, fmt.Errorf("CheckDocumentRequirements: query requirements: %w", err)
	}

	missing = make([]model.DocumentType, 0)
	for _, row := range rows {
		if row.CurrentCount >= row.RequiredCountMin {
			continue
		}

		var allowedExtensions []string
		if err := json.Unmarshal([]byte(row.AllowedExtensionsJSON), &allowedExtensions); err != nil {
			return false, nil, fmt.Errorf("CheckDocumentRequirements: decode allowed_extensions for %s: %w", row.DocumentTypeCode, err)
		}
		var allowedViewers []string
		if err := json.Unmarshal([]byte(row.AllowedViewersJSON), &allowedViewers); err != nil {
			return false, nil, fmt.Errorf("CheckDocumentRequirements: decode allowed_viewers for %s: %w", row.DocumentTypeCode, err)
		}

		missing = append(missing, model.DocumentType{
			ID:                   row.ID,
			CaseTypeCode:         row.CaseTypeCode,
			CaseTypeVersion:      row.CaseTypeVersion,
			DocumentTypeCode:     row.DocumentTypeCode,
			DisplayName:          row.DisplayName,
			Description:          row.Description,
			AllowedExtensions:    allowedExtensions,
			MaxSizeMB:            row.MaxSizeMB,
			RequiredAtStage:      row.RequiredAtStage,
			RequiredCountMin:     row.RequiredCountMin,
			RequiredCountMax:     row.RequiredCountMax,
			IsSensitive:          row.IsSensitive,
			RequiresVerification: row.RequiresVerification,
			VerificationRole:     row.VerificationRole,
			RetentionDays:        row.RetentionDays,
			RetentionPolicy:      model.DocumentRetentionPolicy(row.RetentionPolicy),
			AllowedViewers:       allowedViewers,
		})
	}

	return len(missing) == 0, missing, nil
}

// GetDocumentHistory returns full version chain ordered by version DESC.
func GetDocumentHistory(
	ctx context.Context,
	db *sqlx.DB,
	documentID string,
) ([]model.Document, error) {
	if db == nil {
		return nil, fmt.Errorf("GetDocumentHistory: db is nil")
	}
	var docs []model.Document
	if err := db.SelectContext(ctx, &docs, `
		WITH RECURSIVE chain AS (
			SELECT
				cd.id::text AS id,
				cd.case_id::text AS case_id,
				cd.task_id::text AS task_id,
				cd.stage_code,
				cd.case_type_code,
				cd.case_type_version,
				cd.document_type_code,
				cd.filename,
				cd.file_extension,
				cd.file_size_bytes,
				cd.storage_provider,
				cd.storage_path,
				cd.storage_url,
				cd.checksum_sha256,
				cd.uploaded_by,
				cd.uploaded_at,
				cd.status,
				cd.rejection_reason,
				cd.verified_by,
				cd.verified_at,
				cd.metadata,
				cd.version,
				cd.superseded_by_document_id::text AS superseded_by_document_id,
				cd.legal_hold,
				cd.created_at,
				cd.updated_at
			FROM case_documents cd
			WHERE cd.id = $1::uuid

			UNION

			SELECT
				prev.id::text AS id,
				prev.case_id::text AS case_id,
				prev.task_id::text AS task_id,
				prev.stage_code,
				prev.case_type_code,
				prev.case_type_version,
				prev.document_type_code,
				prev.filename,
				prev.file_extension,
				prev.file_size_bytes,
				prev.storage_provider,
				prev.storage_path,
				prev.storage_url,
				prev.checksum_sha256,
				prev.uploaded_by,
				prev.uploaded_at,
				prev.status,
				prev.rejection_reason,
				prev.verified_by,
				prev.verified_at,
				prev.metadata,
				prev.version,
				prev.superseded_by_document_id::text AS superseded_by_document_id,
				prev.legal_hold,
				prev.created_at,
				prev.updated_at
			FROM case_documents prev
			JOIN chain c
			  ON prev.superseded_by_document_id = c.id::uuid

			UNION

			SELECT
				next.id::text AS id,
				next.case_id::text AS case_id,
				next.task_id::text AS task_id,
				next.stage_code,
				next.case_type_code,
				next.case_type_version,
				next.document_type_code,
				next.filename,
				next.file_extension,
				next.file_size_bytes,
				next.storage_provider,
				next.storage_path,
				next.storage_url,
				next.checksum_sha256,
				next.uploaded_by,
				next.uploaded_at,
				next.status,
				next.rejection_reason,
				next.verified_by,
				next.verified_at,
				next.metadata,
				next.version,
				next.superseded_by_document_id::text AS superseded_by_document_id,
				next.legal_hold,
				next.created_at,
				next.updated_at
			FROM case_documents next
			JOIN chain c
			  ON c.superseded_by_document_id = next.id::uuid
		)
		SELECT *
		FROM chain
		ORDER BY version DESC, uploaded_at DESC
	`, documentID); err != nil {
		return nil, fmt.Errorf("GetDocumentHistory: %w", err)
	}
	return docs, nil
}

// ListCaseDocuments returns case documents with optional version-chain expansion.
func ListCaseDocuments(
	ctx context.Context,
	db *sqlx.DB,
	caseID string,
	includeAllVersions bool,
) ([]model.Document, error) {
	if db == nil {
		return nil, fmt.Errorf("ListCaseDocuments: db is nil")
	}
	baseQuery := `
		SELECT
			id::text AS id,
			case_id::text AS case_id,
			task_id::text AS task_id,
			stage_code,
			case_type_code,
			case_type_version,
			document_type_code,
			filename,
			file_extension,
			file_size_bytes,
			storage_provider,
			storage_path,
			storage_url,
			checksum_sha256,
			uploaded_by,
			uploaded_at,
			status,
			rejection_reason,
			verified_by,
			verified_at,
			metadata,
			version,
			superseded_by_document_id::text AS superseded_by_document_id,
			legal_hold,
			created_at,
			updated_at
		FROM case_documents
		WHERE case_id = $1::uuid
	`
	if !includeAllVersions {
		baseQuery += `
			AND superseded_by_document_id IS NULL
		`
	}
	baseQuery += `
		ORDER BY document_type_code ASC, version DESC, uploaded_at DESC
	`
	var docs []model.Document
	if err := db.SelectContext(ctx, &docs, baseQuery, caseID); err != nil {
		return nil, fmt.Errorf("ListCaseDocuments: query documents: %w", err)
	}
	return docs, nil
}

// EnforceDocumentRetention applies DB-level retention transitions.
func EnforceDocumentRetention(
	ctx context.Context,
	db *sqlx.DB,
) (archived int, deleted int, err error) {
	if db == nil {
		return 0, 0, fmt.Errorf("EnforceDocumentRetention: db is nil")
	}
	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, 0, fmt.Errorf("EnforceDocumentRetention: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var result struct {
		Archived int `db:"archived_count"`
		Deleted  int `db:"deleted_count"`
	}
	err = tx.QueryRowxContext(ctx, `
		WITH candidates AS (
			SELECT cd.id, dt.retention_policy
			FROM case_documents cd
			JOIN cases c ON c.id = cd.case_id
			JOIN document_types dt
			  ON dt.case_type_code = cd.case_type_code
			 AND dt.case_type_version = cd.case_type_version
			 AND dt.document_type_code = cd.document_type_code
			WHERE cd.status IN ('UPLOADED', 'VERIFIED')
			  AND cd.legal_hold = FALSE
			  AND c.status IN ('COMPLETED', 'CANCELLED')
			  AND cd.uploaded_at < (now() - (dt.retention_days || ' days')::interval)
			FOR UPDATE OF cd SKIP LOCKED
		),
		updated AS (
			UPDATE case_documents cd
			SET status = CASE WHEN c.retention_policy = 'ARCHIVE' THEN 'ARCHIVED' ELSE 'DELETED' END,
			    updated_at = now()
			FROM candidates c
			WHERE cd.id = c.id
			RETURNING c.retention_policy
		)
		SELECT
			COUNT(*) FILTER (WHERE retention_policy = 'ARCHIVE') AS archived_count,
			COUNT(*) FILTER (WHERE retention_policy = 'DELETE') AS deleted_count
		FROM updated
	`).StructScan(&result)
	if err != nil {
		return 0, 0, fmt.Errorf("EnforceDocumentRetention: update statuses: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("EnforceDocumentRetention: commit: %w", err)
	}
	return result.Archived, result.Deleted, nil
}

func loadCaseContextTx(ctx context.Context, tx *sqlx.Tx, caseID string) (caseContext, error) {
	var c caseContext
	err := tx.QueryRowContext(ctx, `
		SELECT
			ct.code AS case_type_code,
			ct.version AS case_type_version,
			COALESCE(c.current_stage_code, '') AS current_stage_code
		FROM cases c
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE c.id = $1::uuid
		FOR UPDATE
	`, caseID).Scan(&c.CaseTypeCode, &c.CaseTypeVersion, &c.CurrentStage)
	if err != nil {
		return caseContext{}, fmt.Errorf("loadCaseContextTx: %w", err)
	}
	return c, nil
}

func loadDocumentTypeTx(ctx context.Context, tx *sqlx.Tx, caseTypeCode string, caseTypeVersion int, docType string) (documentTypeConfig, error) {
	var (
		documentType        model.DocumentType
		allowedExtJSON      string
		allowedViewersJSON  string
		retentionPolicyRaw  string
	)
	err := tx.QueryRowContext(ctx, `
		SELECT
			id::text,
			case_type_code,
			case_type_version,
			document_type_code,
			display_name,
			description,
			COALESCE(array_to_json(allowed_extensions)::text, '[]') AS allowed_extensions_json,
			max_size_mb,
			required_at_stage,
			required_count_min,
			required_count_max,
			is_sensitive,
			requires_verification,
			verification_role,
			retention_days,
			retention_policy,
			COALESCE(array_to_json(allowed_viewers)::text, '[]') AS allowed_viewers_json,
			created_at,
			updated_at
		FROM document_types
		WHERE case_type_code = $1
		  AND case_type_version = $2
		  AND document_type_code = $3
		FOR UPDATE
	`, caseTypeCode, caseTypeVersion, strings.TrimSpace(docType)).Scan(
		&documentType.ID,
		&documentType.CaseTypeCode,
		&documentType.CaseTypeVersion,
		&documentType.DocumentTypeCode,
		&documentType.DisplayName,
		&documentType.Description,
		&allowedExtJSON,
		&documentType.MaxSizeMB,
		&documentType.RequiredAtStage,
		&documentType.RequiredCountMin,
		&documentType.RequiredCountMax,
		&documentType.IsSensitive,
		&documentType.RequiresVerification,
		&documentType.VerificationRole,
		&documentType.RetentionDays,
		&retentionPolicyRaw,
		&allowedViewersJSON,
		&documentType.CreatedAt,
		&documentType.UpdatedAt,
	)
	if err != nil {
		return documentTypeConfig{}, fmt.Errorf("loadDocumentTypeTx: %w", err)
	}
	if err := json.Unmarshal([]byte(allowedExtJSON), &documentType.AllowedExtensions); err != nil {
		return documentTypeConfig{}, fmt.Errorf("loadDocumentTypeTx: parse allowed_extensions: %w", err)
	}
	if err := json.Unmarshal([]byte(allowedViewersJSON), &documentType.AllowedViewers); err != nil {
		return documentTypeConfig{}, fmt.Errorf("loadDocumentTypeTx: parse allowed_viewers: %w", err)
	}
	documentType.RetentionPolicy = model.DocumentRetentionPolicy(retentionPolicyRaw)
	return documentTypeConfig{DocumentType: documentType}, nil
}

func refreshDocumentRequestStatus(
	ctx context.Context,
	tx *sqlx.Tx,
	caseID string,
	caseTypeCode string,
	caseTypeVersion int,
	documentTypeCode string,
) (bool, error) {
	var request struct {
		ID               string `db:"id"`
		RequiredCountMin int    `db:"required_count_min"`
		RequiredCountMax int    `db:"required_count_max"`
		Status           string `db:"status"`
	}
	err := tx.QueryRowxContext(ctx, `
		SELECT id::text, required_count_min, required_count_max, status
		FROM document_requests
		WHERE case_id = $1::uuid
		  AND case_type_code = $2
		  AND case_type_version = $3
		  AND document_type_code = $4
		FOR UPDATE
	`, caseID, caseTypeCode, caseTypeVersion, documentTypeCode).StructScan(&request)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("refreshDocumentRequestStatus: load request: %w", err)
	}

	if model.DocumentRequestStatus(request.Status) == model.DocumentRequestStatusWaived {
		return false, nil
	}

	var requiresVerification bool
	if err := tx.QueryRowContext(ctx, `
		SELECT requires_verification
		FROM document_types
		WHERE case_type_code = $1
		  AND case_type_version = $2
		  AND document_type_code = $3
	`, caseTypeCode, caseTypeVersion, documentTypeCode).Scan(&requiresVerification); err != nil {
		return false, fmt.Errorf("refreshDocumentRequestStatus: load requires_verification: %w", err)
	}

	countSQL := `
		SELECT COUNT(*)
		FROM case_documents
		WHERE case_id = $1::uuid
		  AND document_type_code = $2
		  AND superseded_by_document_id IS NULL
		  AND status = 'VERIFIED'
	`
	if !requiresVerification {
		countSQL = `
			SELECT COUNT(*)
			FROM case_documents
			WHERE case_id = $1::uuid
			  AND document_type_code = $2
			  AND superseded_by_document_id IS NULL
			  AND status IN ('UPLOADED', 'VERIFIED')
		`
	}
	var currentCount int
	if err := tx.QueryRowContext(ctx, countSQL, caseID, documentTypeCode).Scan(&currentCount); err != nil {
		return false, fmt.Errorf("refreshDocumentRequestStatus: count documents: %w", err)
	}

	if currentCount > request.RequiredCountMax {
		currentCount = request.RequiredCountMax
	}

	newStatus := model.DocumentRequestStatusPending
	var fulfilledAt interface{}
	switch {
	case currentCount >= request.RequiredCountMin:
		newStatus = model.DocumentRequestStatusFulfilled
		fulfilledAt = time.Now().UTC()
	case currentCount > 0:
		newStatus = model.DocumentRequestStatusPartiallyFulfilled
		fulfilledAt = nil
	default:
		newStatus = model.DocumentRequestStatusPending
		fulfilledAt = nil
	}

	fulfilledNow := model.DocumentRequestStatus(request.Status) != model.DocumentRequestStatusFulfilled &&
		newStatus == model.DocumentRequestStatusFulfilled

	_, err = tx.ExecContext(ctx, `
		UPDATE document_requests
		SET current_count = $1,
		    status = $2,
		    fulfilled_at = CASE WHEN $2 = 'FULFILLED' THEN COALESCE(fulfilled_at, $3::timestamptz) ELSE NULL END,
		    updated_at = now()
		WHERE id = $4::uuid
	`, currentCount, string(newStatus), fulfilledAt, request.ID)
	if err != nil {
		return false, fmt.Errorf("refreshDocumentRequestStatus: update request: %w", err)
	}
	return fulfilledNow, nil
}

func createVerificationTask(
	ctx context.Context,
	tx *sqlx.Tx,
	caseID string,
	documentID string,
	documentType documentTypeConfig,
	metadata DocumentUploadMetadata,
) error {
	taskDefinitionCode := fmt.Sprintf("VERIFY_%s", documentType.DocumentTypeCode)
	assignedService := "DOCUMENT_VERIFICATION_SERVICE"
	if strings.TrimSpace(metadata.AssignedVerificationTo) != "" {
		assignedService = strings.TrimSpace(metadata.AssignedVerificationTo)
	}
	taskInput := jsonRawOrDefault(map[string]interface{}{
		"document_id":        documentID,
		"document_type_code": documentType.DocumentTypeCode,
	})
	var taskID string
	err := tx.QueryRowContext(ctx, `
		INSERT INTO tasks (
			case_id,
			task_definition_code,
			activity_code,
			stage_code,
			status,
			priority,
			assigned_service,
			input_payload,
			idempotency_key,
			max_retries,
			is_document_verification
		)
		VALUES (
			$1::uuid,
			$2,
			$3,
			$4,
			'PENDING',
			2,
			$5,
			$6::jsonb,
			$7,
			3,
			TRUE
		)
		RETURNING id::text
	`,
		caseID,
		taskDefinitionCode,
		"DOCUMENT_VERIFICATION",
		metadata.StageCode,
		assignedService,
		taskInput,
		fmt.Sprintf("%s:doc-verify:%s", caseID, documentID),
	).Scan(&taskID)
	if err != nil {
		return fmt.Errorf("createVerificationTask: insert task: %w", err)
	}

	role := "DOCUMENT_REVIEWER"
	if documentType.VerificationRole != nil && strings.TrimSpace(*documentType.VerificationRole) != "" {
		role = strings.TrimSpace(*documentType.VerificationRole)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO document_verification_tasks (
			task_id,
			document_id,
			requested_role,
			status,
			created_at,
			updated_at
		)
		VALUES ($1::uuid, $2::uuid, $3, 'PENDING', now(), now())
	`, taskID, documentID, role)
	if err != nil {
		return fmt.Errorf("createVerificationTask: insert mapping: %w", err)
	}
	return nil
}

func loadDocumentByIDTx(ctx context.Context, tx *sqlx.Tx, documentID string) (model.Document, error) {
	var document model.Document
	err := tx.QueryRowxContext(ctx, `
		SELECT
			id::text AS id,
			case_id::text AS case_id,
			task_id::text AS task_id,
			stage_code,
			case_type_code,
			case_type_version,
			document_type_code,
			filename,
			file_extension,
			file_size_bytes,
			storage_provider,
			storage_path,
			storage_url,
			checksum_sha256,
			uploaded_by,
			uploaded_at,
			status,
			rejection_reason,
			verified_by,
			verified_at,
			metadata,
			version,
			superseded_by_document_id::text AS superseded_by_document_id,
			legal_hold,
			created_at,
			updated_at
		FROM case_documents
		WHERE id = $1::uuid
	`, documentID).StructScan(&document)
	if err != nil {
		return model.Document{}, fmt.Errorf("loadDocumentByIDTx: %w", err)
	}
	return document, nil
}

func normalizeExtensions(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized := normalizeExtension(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func containsNormalized(values []string, target string) bool {
	target = normalizeExtension(target)
	for _, candidate := range values {
		if normalizeExtension(candidate) == target {
			return true
		}
	}
	return false
}

func normalizeRoles(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		role := strings.ToUpper(strings.TrimSpace(value))
		if role == "" {
			continue
		}
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		out = append(out, role)
	}
	return out
}

func inferStorageProvider(storage DocumentStorage) model.DocumentStorageProvider {
	switch storage.(type) {
	case *S3Storage:
		return model.DocumentStorageProviderS3
	case *LocalStorage:
		return model.DocumentStorageProviderLocal
	default:
		return model.DocumentStorageProviderLocal
	}
}

func nilIfBlank(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nilIfBlankPtr(value *string) interface{} {
	if value == nil {
		return nil
	}
	if strings.TrimSpace(*value) == "" {
		return nil
	}
	return strings.TrimSpace(*value)
}

func targetServiceOrDefault(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "case-orchestrator"
	}
	return value
}
