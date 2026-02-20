package model

import (
	"encoding/json"
	"time"
)

// DocumentStorageProvider identifies the object storage backend.
type DocumentStorageProvider string

const (
	DocumentStorageProviderS3        DocumentStorageProvider = "S3"
	DocumentStorageProviderGCS       DocumentStorageProvider = "GCS"
	DocumentStorageProviderAzureBlob DocumentStorageProvider = "AZURE_BLOB"
	DocumentStorageProviderLocal     DocumentStorageProvider = "LOCAL"
)

// DocumentRetentionPolicy controls post-retention handling.
type DocumentRetentionPolicy string

const (
	DocumentRetentionPolicyArchive DocumentRetentionPolicy = "ARCHIVE"
	DocumentRetentionPolicyDelete  DocumentRetentionPolicy = "DELETE"
)

// DocumentStatus is the lifecycle state of case_documents rows.
type DocumentStatus string

const (
	DocumentStatusPendingUpload DocumentStatus = "PENDING_UPLOAD"
	DocumentStatusUploaded      DocumentStatus = "UPLOADED"
	DocumentStatusVerified      DocumentStatus = "VERIFIED"
	DocumentStatusRejected      DocumentStatus = "REJECTED"
	DocumentStatusArchived      DocumentStatus = "ARCHIVED"
	DocumentStatusDeleted       DocumentStatus = "DELETED"
)

// DocumentRequestStatus tracks requirement fulfillment status.
type DocumentRequestStatus string

const (
	DocumentRequestStatusPending            DocumentRequestStatus = "PENDING"
	DocumentRequestStatusPartiallyFulfilled DocumentRequestStatus = "PARTIALLY_FULFILLED"
	DocumentRequestStatusFulfilled          DocumentRequestStatus = "FULFILLED"
	DocumentRequestStatusWaived             DocumentRequestStatus = "WAIVED"
)

// RedactionRule controls how sensitive fields are transformed.
type RedactionRule string

const (
	RedactionRuleMask     RedactionRule = "MASK"
	RedactionRuleTruncate RedactionRule = "TRUNCATE"
	RedactionRuleHide     RedactionRule = "HIDE"
)

// DocumentVerificationTaskStatus tracks verification task outcomes.
type DocumentVerificationTaskStatus string

const (
	DocumentVerificationTaskStatusPending         DocumentVerificationTaskStatus = "PENDING"
	DocumentVerificationTaskStatusApproved        DocumentVerificationTaskStatus = "APPROVED"
	DocumentVerificationTaskStatusRejected        DocumentVerificationTaskStatus = "REJECTED"
	DocumentVerificationTaskStatusRequestReupload DocumentVerificationTaskStatus = "REQUEST_REUPLOAD"
	DocumentVerificationTaskStatusCancelled       DocumentVerificationTaskStatus = "CANCELLED"
)

// DocumentType is the persisted definition of a document category.
type DocumentType struct {
	ID                   string                  `json:"id" db:"id"`
	CaseTypeCode         string                  `json:"case_type_code" db:"case_type_code"`
	CaseTypeVersion      int                     `json:"case_type_version" db:"case_type_version"`
	DocumentTypeCode     string                  `json:"document_type_code" db:"document_type_code"`
	DisplayName          string                  `json:"display_name" db:"display_name"`
	Description          *string                 `json:"description,omitempty" db:"description"`
	AllowedExtensions    []string                `json:"allowed_extensions" db:"allowed_extensions"`
	MaxSizeMB            int                     `json:"max_size_mb" db:"max_size_mb"`
	RequiredAtStage      *string                 `json:"required_at_stage,omitempty" db:"required_at_stage"`
	RequiredCountMin     int                     `json:"required_count_min" db:"required_count_min"`
	RequiredCountMax     int                     `json:"required_count_max" db:"required_count_max"`
	IsSensitive          bool                    `json:"is_sensitive" db:"is_sensitive"`
	RequiresVerification bool                    `json:"requires_verification" db:"requires_verification"`
	VerificationRole     *string                 `json:"verification_role,omitempty" db:"verification_role"`
	RetentionDays        int                     `json:"retention_days" db:"retention_days"`
	RetentionPolicy      DocumentRetentionPolicy `json:"retention_policy" db:"retention_policy"`
	AllowedViewers       []string                `json:"allowed_viewers" db:"allowed_viewers"`
	CreatedAt            time.Time               `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time               `json:"updated_at" db:"updated_at"`
}

// Document is the persisted metadata row for a file stored externally.
type Document struct {
	ID                     string                 `json:"id" db:"id"`
	CaseID                 string                 `json:"case_id" db:"case_id"`
	TaskID                 *string                `json:"task_id,omitempty" db:"task_id"`
	StageCode              string                 `json:"stage_code" db:"stage_code"`
	CaseTypeCode           string                 `json:"case_type_code" db:"case_type_code"`
	CaseTypeVersion        int                    `json:"case_type_version" db:"case_type_version"`
	DocumentTypeCode       string                 `json:"document_type_code" db:"document_type_code"`
	Filename               string                 `json:"filename" db:"filename"`
	FileExtension          string                 `json:"file_extension" db:"file_extension"`
	FileSizeBytes          int64                  `json:"file_size_bytes" db:"file_size_bytes"`
	StorageProvider        DocumentStorageProvider `json:"storage_provider" db:"storage_provider"`
	StoragePath            string                 `json:"storage_path" db:"storage_path"`
	StorageURL             *string                `json:"storage_url,omitempty" db:"storage_url"`
	ChecksumSHA256         string                 `json:"checksum_sha256" db:"checksum_sha256"`
	UploadedBy             string                 `json:"uploaded_by" db:"uploaded_by"`
	UploadedAt             time.Time              `json:"uploaded_at" db:"uploaded_at"`
	Status                 DocumentStatus         `json:"status" db:"status"`
	RejectionReason        *string                `json:"rejection_reason,omitempty" db:"rejection_reason"`
	VerifiedBy             *string                `json:"verified_by,omitempty" db:"verified_by"`
	VerifiedAt             *time.Time             `json:"verified_at,omitempty" db:"verified_at"`
	Metadata               json.RawMessage        `json:"metadata" db:"metadata"`
	Version                int                    `json:"version" db:"version"`
	SupersededByDocumentID *string                `json:"superseded_by_document_id,omitempty" db:"superseded_by_document_id"`
	LegalHold              bool                   `json:"legal_hold" db:"legal_hold"`
	CreatedAt              time.Time              `json:"created_at" db:"created_at"`
	UpdatedAt              time.Time              `json:"updated_at" db:"updated_at"`
}

// DocumentRequest tracks if required documents are satisfied for a case/stage.
type DocumentRequest struct {
	ID                string                `json:"id" db:"id"`
	CaseID            string                `json:"case_id" db:"case_id"`
	CaseTypeCode      string                `json:"case_type_code" db:"case_type_code"`
	CaseTypeVersion   int                   `json:"case_type_version" db:"case_type_version"`
	DocumentTypeCode  string                `json:"document_type_code" db:"document_type_code"`
	RequiredAtStage   *string               `json:"required_at_stage,omitempty" db:"required_at_stage"`
	RequiredCountMin  int                   `json:"required_count_min" db:"required_count_min"`
	RequiredCountMax  int                   `json:"required_count_max" db:"required_count_max"`
	CurrentCount      int                   `json:"current_count" db:"current_count"`
	Status            DocumentRequestStatus `json:"status" db:"status"`
	RequestedAt       time.Time             `json:"requested_at" db:"requested_at"`
	FulfilledAt       *time.Time            `json:"fulfilled_at,omitempty" db:"fulfilled_at"`
	WaivedBy          *string               `json:"waived_by,omitempty" db:"waived_by"`
	WaivedAt          *time.Time            `json:"waived_at,omitempty" db:"waived_at"`
	WaiverReason      *string               `json:"waiver_reason,omitempty" db:"waiver_reason"`
	CreatedAt         time.Time             `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at" db:"updated_at"`
}

// SensitiveField defines redaction policy for a payload path.
type SensitiveField struct {
	ID            string       `json:"id" db:"id"`
	FieldPath     string       `json:"field_path" db:"field_path"`
	RedactionRule RedactionRule `json:"redaction_rule" db:"redaction_rule"`
	MaskPattern   *string      `json:"mask_pattern,omitempty" db:"mask_pattern"`
	AllowedRoles  []string     `json:"allowed_roles" db:"allowed_roles"`
	CreatedAt     time.Time    `json:"created_at" db:"created_at"`
}

// DocumentVerificationTask maps verification tasks to documents for auditing.
type DocumentVerificationTask struct {
	ID            string      `json:"id" db:"id"`
	TaskID        string      `json:"task_id" db:"task_id"`
	DocumentID    string      `json:"document_id" db:"document_id"`
	RequestedRole string      `json:"requested_role" db:"requested_role"`
	Status        DocumentVerificationTaskStatus `json:"status" db:"status"`
	VerifierID    *string     `json:"verifier_id,omitempty" db:"verifier_id"`
	VerifiedAt    *time.Time  `json:"verified_at,omitempty" db:"verified_at"`
	Reason        *string     `json:"reason,omitempty" db:"reason"`
	CreatedAt     time.Time   `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at" db:"updated_at"`
}
