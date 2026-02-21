package scim

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	SchemaListResponse          = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	SchemaError                = "urn:ietf:params:scim:api:messages:2.0:Error"
	SchemaBulkRequest          = "urn:ietf:params:scim:api:messages:2.0:BulkRequest"
	SchemaBulkResponse         = "urn:ietf:params:scim:api:messages:2.0:BulkResponse"
	SchemaPatchOp              = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	SchemaCoreUser             = "urn:ietf:params:scim:schemas:core:2.0:User"
	SchemaCoreGroup            = "urn:ietf:params:scim:schemas:core:2.0:Group"
	SchemaWorkflowUserExtension = "urn:ietf:params:scim:schemas:extension:workflow:2.0:User"
)

// SCIMTokenStatus is the lifecycle status of a SCIM bearer token.
type SCIMTokenStatus string

const (
	SCIMTokenStatusActive  SCIMTokenStatus = "ACTIVE"
	SCIMTokenStatusRevoked SCIMTokenStatus = "REVOKED"
)

// SCIMToken maps to scim_tokens table.
type SCIMToken struct {
	TokenID     string          `db:"token_id" json:"tokenId"`
	TenantID    string          `db:"tenant_id" json:"tenantId"`
	TokenHash   string          `db:"token_hash" json:"-"`
	Description string          `db:"description" json:"description"`
	Scopes      []string        `db:"scopes" json:"scopes"`
	Status      SCIMTokenStatus `db:"status" json:"status"`
	Metadata    json.RawMessage `db:"metadata" json:"metadata,omitempty"`
	LastUsedAt  *time.Time      `db:"last_used_at" json:"lastUsedAt,omitempty"`
	ExpiresAt   *time.Time      `db:"expires_at" json:"expiresAt,omitempty"`
	CreatedBy   string          `db:"created_by" json:"createdBy"`
	CreatedAt   time.Time       `db:"created_at" json:"createdAt"`
	UpdatedAt   time.Time       `db:"updated_at" json:"updatedAt"`
}

// SCIMTokenClaims are injected into request context by SCIM middleware.
type SCIMTokenClaims struct {
	TenantID string
	Scopes   []string
	TokenID  string
}

// SCIMResourceType identifies SCIM resources for filter translation.
type SCIMResourceType string

const (
	SCIMResourceTypeUser  SCIMResourceType = "USER"
	SCIMResourceTypeGroup SCIMResourceType = "GROUP"
)

// SCIMMeta maps to SCIM meta object.
type SCIMMeta struct {
	ResourceType string `json:"resourceType,omitempty"`
	Created      string `json:"created,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
	Location     string `json:"location,omitempty"`
	Version      string `json:"version,omitempty"`
}

// SCIMEmail maps to SCIM emails entry.
type SCIMEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
	Type    string `json:"type,omitempty"`
}

// SCIMWorkflowUserExtension is the enterprise custom extension.
type SCIMWorkflowUserExtension struct {
	TenantID     string   `json:"tenantId,omitempty"`
	Roles        []string `json:"roles,omitempty"`
	TeamID       string   `json:"teamId,omitempty"`
	Timezone     string   `json:"timezone,omitempty"`
	AuthProvider string   `json:"authProvider,omitempty"`
}

// SCIMUser is the SCIM wire format representation of a User resource.
type SCIMUser struct {
	Schemas                []string                   `json:"schemas"`
	ID                     string                     `json:"id,omitempty"`
	ExternalID             string                     `json:"externalId,omitempty"`
	UserName               string                     `json:"userName,omitempty"`
	DisplayName            string                     `json:"displayName,omitempty"`
	Emails                 []SCIMEmail                `json:"emails,omitempty"`
	Active                 *bool                      `json:"active,omitempty"`
	Locale                 string                     `json:"locale,omitempty"`
	Timezone               string                     `json:"timezone,omitempty"`
	Meta                   SCIMMeta                   `json:"meta,omitempty"`
	WorkflowUserExtension  *SCIMWorkflowUserExtension `json:"urn:ietf:params:scim:schemas:extension:workflow:2.0:User,omitempty"`
}

// SCIMGroupMember maps to SCIM Group members element.
type SCIMGroupMember struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}

// SCIMGroup is the SCIM wire format representation of a Group resource.
type SCIMGroup struct {
	Schemas     []string          `json:"schemas"`
	ID          string            `json:"id,omitempty"`
	ExternalID  string            `json:"externalId,omitempty"`
	DisplayName string            `json:"displayName,omitempty"`
	Members     []SCIMGroupMember `json:"members,omitempty"`
	Meta        SCIMMeta          `json:"meta,omitempty"`
}

// SCIMListResponse is the generic SCIM list envelope.
type SCIMListResponse[T any] struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    []T      `json:"Resources"`
}

// SCIMError is the SCIM-compliant error payload.
type SCIMError struct {
	Schemas  []string `json:"schemas"`
	Status   string   `json:"status"`
	ScimType string   `json:"scimType,omitempty"`
	Detail   string   `json:"detail"`
}

// SCIMPatchRequest is the RFC 7644 PATCH request envelope.
type SCIMPatchRequest struct {
	Schemas    []string             `json:"schemas"`
	Operations []SCIMPatchOperation `json:"Operations"`
}

// SCIMPatchOperation is one PATCH operation.
type SCIMPatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path,omitempty"`
	Value interface{} `json:"value,omitempty"`
}

// SCIMBulkRequest is the RFC 7644 bulk request envelope.
type SCIMBulkRequest struct {
	Schemas      []string            `json:"schemas"`
	FailOnErrors int                 `json:"failOnErrors,omitempty"`
	Operations   []SCIMBulkOperation `json:"Operations"`
}

// SCIMBulkOperation defines one operation in a bulk request.
type SCIMBulkOperation struct {
	Method  string          `json:"method"`
	Path    string          `json:"path"`
	BulkID  string          `json:"bulkId,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Version string          `json:"version,omitempty"`
}

// SCIMBulkResponse is the RFC 7644 bulk response envelope.
type SCIMBulkResponse struct {
	Schemas    []string                    `json:"schemas"`
	Operations []SCIMBulkOperationResponse `json:"Operations"`
}

// SCIMBulkOperationResponse is one operation result in bulk response.
type SCIMBulkOperationResponse struct {
	Method   string      `json:"method,omitempty"`
	Location string      `json:"location,omitempty"`
	BulkID   string      `json:"bulkId,omitempty"`
	Version  string      `json:"version,omitempty"`
	Status   string      `json:"status"`
	Response interface{} `json:"response,omitempty"`
}

// SCIMSchemaDocument models schema-discovery documents.
type SCIMSchemaDocument struct {
	Schemas     []string              `json:"schemas"`
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Attributes  []SCIMSchemaAttribute `json:"attributes"`
	Meta        SCIMMeta              `json:"meta,omitempty"`
}

// SCIMSchemaAttribute models one SCIM schema attribute declaration.
type SCIMSchemaAttribute struct {
	Name           string                `json:"name"`
	Type           string                `json:"type"`
	SubAttributes  []SCIMSchemaAttribute `json:"subAttributes,omitempty"`
	MultiValued    bool                  `json:"multiValued"`
	Description    string                `json:"description,omitempty"`
	Required       bool                  `json:"required"`
	CaseExact      bool                  `json:"caseExact,omitempty"`
	Mutability     string                `json:"mutability"`
	Returned       string                `json:"returned"`
	Uniqueness     string                `json:"uniqueness"`
	CanonicalValues []string             `json:"canonicalValues,omitempty"`
}

// SCIMResourceTypeSchemaExtension models resource-type schema extension declarations.
type SCIMResourceTypeSchemaExtension struct {
	Schema   string `json:"schema"`
	Required bool   `json:"required"`
}

// SCIMResourceTypeDocument models /ResourceTypes documents.
type SCIMResourceTypeDocument struct {
	Schemas          []string                          `json:"schemas"`
	ID               string                            `json:"id"`
	Name             string                            `json:"name"`
	Endpoint         string                            `json:"endpoint"`
	Description      string                            `json:"description,omitempty"`
	Schema           string                            `json:"schema"`
	SchemaExtensions []SCIMResourceTypeSchemaExtension `json:"schemaExtensions,omitempty"`
	Meta             SCIMMeta                          `json:"meta,omitempty"`
}

// SCIMServiceProviderConfig models /ServiceProviderConfig response.
type SCIMServiceProviderConfig struct {
	Schemas               []string                                `json:"schemas"`
	Patch                 SCIMServiceProviderConfigSupport        `json:"patch"`
	Bulk                  SCIMServiceProviderConfigBulkSupport    `json:"bulk"`
	Filter                SCIMServiceProviderConfigFilterSupport  `json:"filter"`
	ChangePassword        SCIMServiceProviderConfigSupport        `json:"changePassword"`
	Sort                  SCIMServiceProviderConfigSupport        `json:"sort"`
	ETag                  SCIMServiceProviderConfigSupport        `json:"etag"`
	AuthenticationSchemes []SCIMAuthenticationScheme             `json:"authenticationSchemes"`
	Meta                  SCIMMeta                                `json:"meta,omitempty"`
}

// SCIMServiceProviderConfigSupport models supported flags.
type SCIMServiceProviderConfigSupport struct {
	Supported bool `json:"supported"`
}

// SCIMServiceProviderConfigBulkSupport models bulk support.
type SCIMServiceProviderConfigBulkSupport struct {
	Supported      bool `json:"supported"`
	MaxOperations  int  `json:"maxOperations"`
	MaxPayloadSize int  `json:"maxPayloadSize"`
}

// SCIMServiceProviderConfigFilterSupport models filter support.
type SCIMServiceProviderConfigFilterSupport struct {
	Supported  bool `json:"supported"`
	MaxResults int  `json:"maxResults"`
}

// SCIMAuthenticationScheme models authentication declaration.
type SCIMAuthenticationScheme struct {
	Type             string `json:"type"`
	Name             string `json:"name,omitempty"`
	Description      string `json:"description,omitempty"`
	SpecURI          string `json:"specUri,omitempty"`
	DocumentationURI string `json:"documentationUri,omitempty"`
	Primary          bool   `json:"primary,omitempty"`
}

// SCIMAuditEntry maps to scim_audit_log table.
type SCIMAuditEntry struct {
	AuditID            string     `db:"audit_id" json:"auditId"`
	TenantID           string     `db:"tenant_id" json:"tenantId"`
	TokenID            *string    `db:"token_id" json:"tokenId,omitempty"`
	Operation          string     `db:"operation" json:"operation"`
	ResourceType       string     `db:"resource_type" json:"resourceType"`
	ResourceID         *string    `db:"resource_id" json:"resourceId,omitempty"`
	HTTPStatus         int        `db:"http_status" json:"httpStatus"`
	FilterExpression   *string    `db:"filter_expression" json:"filterExpression,omitempty"`
	RequestAttributes  []string   `db:"request_attributes" json:"requestAttributes,omitempty"`
	OperationsCount    int        `db:"operations_count" json:"operationsCount,omitempty"`
	DurationMS         int        `db:"duration_ms" json:"durationMs"`
	IPAddress          string     `db:"ip_address" json:"ipAddress,omitempty"`
	UserAgent          string     `db:"user_agent" json:"userAgent,omitempty"`
	OccurredAt         time.Time  `db:"occurred_at" json:"occurredAt"`
}

// SCIMAuditFilters defines query filters for SCIM audit retrieval.
type SCIMAuditFilters struct {
	TokenID      string
	Operation    string
	ResourceType string
	ResourceID   string
	From         time.Time
	To           time.Time
}

// SCIMFilter translates parsed filter AST to SQL.
type SCIMFilter interface {
	ToSQL(resource SCIMResourceType) (clause string, args []interface{}, err error)
}

// SCIMFilterNode is one AST node in the filter expression tree.
type SCIMFilterNode struct {
	Kind      string
	Attribute string
	Operator  string
	Value     interface{}
	Left      *SCIMFilterNode
	Right     *SCIMFilterNode
}

var (
	ErrSCIMTokenInvalid      = errors.New("invalid scim token")
	ErrInvalidSCIMFilter     = errors.New("invalid scim filter")
	ErrInvalidETag           = errors.New("invalid etag")
	ErrETagMismatch          = errors.New("etag mismatch")
	ErrSCIMRateLimitExceeded = errors.New("scim rate limit exceeded")
)

func scimRateLimitError(retryAfter int) error {
	return fmt.Errorf("%w: retry_after=%d", ErrSCIMRateLimitExceeded, retryAfter)
}

// SCIMHandler hosts SCIM endpoints.
type SCIMHandler struct {
	db     *sqlx.DB
	logger *slog.Logger
}

func newSCIMHandler(db *sqlx.DB, logger *slog.Logger) *SCIMHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SCIMHandler{db: db, logger: logger}
}
