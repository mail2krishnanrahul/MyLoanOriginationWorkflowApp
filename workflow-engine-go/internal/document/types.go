package document

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"workflow-engine/pkg/model"
)

// Actor represents a requestor identity for read-time authorization/redaction.
type Actor struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	IsSystem bool   `json:"is_system,omitempty"`
}

// DocumentUploadMetadata captures upload context and metadata.
type DocumentUploadMetadata struct {
	Filename               string                 `json:"filename"`
	FileExtension          string                 `json:"file_extension,omitempty"`
	FileSizeBytes          int64                  `json:"file_size_bytes,omitempty"`
	StorageProvider        model.DocumentStorageProvider `json:"storage_provider"`
	StorageBucket          string                 `json:"storage_bucket"`
	UploadedBy             string                 `json:"uploaded_by"`
	TaskID                 *string                `json:"task_id,omitempty"`
	StageCode              string                 `json:"stage_code,omitempty"`
	Metadata               map[string]interface{} `json:"metadata,omitempty"`
	SupersedesDocumentID   *string                `json:"supersedes_document_id,omitempty"`
	TargetService          string                 `json:"target_service,omitempty"`
	AssignedVerificationTo string                 `json:"assigned_verification_to,omitempty"`
}

// DocumentMetadata is kept as an alias to match existing API signatures.
type DocumentMetadata = DocumentUploadMetadata

// SchemaViolation is one JSON Schema validation failure.
type SchemaViolation struct {
	Field       string      `json:"field"`
	Description string      `json:"description"`
	Value       interface{} `json:"value,omitempty"`
}

// ValidationError contains field-level schema violations.
type ValidationError struct {
	Operation  string            `json:"operation"`
	Violations []SchemaViolation `json:"violations"`
}

func (e *ValidationError) Error() string {
	if e == nil {
		return "validation failed"
	}
	if len(e.Violations) == 0 {
		if strings.TrimSpace(e.Operation) == "" {
			return "validation failed"
		}
		return fmt.Sprintf("%s: validation failed", e.Operation)
	}
	first := e.Violations[0]
	if strings.TrimSpace(e.Operation) == "" {
		return fmt.Sprintf("validation failed: %s (%s)", first.Field, first.Description)
	}
	return fmt.Sprintf("%s: validation failed: %s (%s)", e.Operation, first.Field, first.Description)
}

func (e *ValidationError) Is(target error) bool {
	_, ok := target.(*ValidationError)
	return ok
}

// DependencyError indicates task input dependency is not yet satisfied.
type DependencyError struct {
	SourceTask  string `json:"source_task"`
	SourceField string `json:"source_field"`
}

func (e *DependencyError) Error() string {
	return fmt.Sprintf("Dependency not satisfied: waiting for %s.%s", strings.TrimSpace(e.SourceTask), strings.TrimSpace(e.SourceField))
}

func (e *DependencyError) Is(target error) bool {
	_, ok := target.(*DependencyError)
	return ok
}

// AuthorizationError represents requestor access denial.
type AuthorizationError struct {
	Resource string `json:"resource"`
	Reason   string `json:"reason"`
}

func (e *AuthorizationError) Error() string {
	resource := strings.TrimSpace(e.Resource)
	if resource == "" {
		resource = "resource"
	}
	reason := strings.TrimSpace(e.Reason)
	if reason == "" {
		reason = "forbidden"
	}
	return fmt.Sprintf("%s: %s", resource, reason)
}

func (e *AuthorizationError) Is(target error) bool {
	_, ok := target.(*AuthorizationError)
	return ok
}

var (
	// ErrInvalidDocumentOperation is returned for invalid state transitions on documents.
	ErrInvalidDocumentOperation = errors.New("invalid document operation")
)

func jsonRawOrDefault(value map[string]interface{}) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("{}")
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("{}")
	}
	return json.RawMessage(payload)
}
