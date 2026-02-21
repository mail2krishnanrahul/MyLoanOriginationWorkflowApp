package scim

import (
	"net/http"
	"strings"
)

func schemaDocLocation(r *http.Request, id string) string {
	base := baseURLFromRequest(r)
	return strings.TrimRight(base, "/") + "/scim/v2/Schemas/" + id
}

func resourceTypeDocLocation(r *http.Request, name string) string {
	base := baseURLFromRequest(r)
	return strings.TrimRight(base, "/") + "/scim/v2/ResourceTypes/" + name
}

func serviceProviderConfigLocation(r *http.Request) string {
	base := baseURLFromRequest(r)
	return strings.TrimRight(base, "/") + "/scim/v2/ServiceProviderConfig"
}

func schemaDocuments(r *http.Request) []SCIMSchemaDocument {
	now := nowRFC3339UTC()
	return []SCIMSchemaDocument{
		{
			Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"},
			ID:          SchemaCoreUser,
			Name:        "User",
			Description: "SCIM User",
			Attributes: []SCIMSchemaAttribute{
				{Name: "id", Type: "string", MultiValued: false, Required: true, Mutability: "readOnly", Returned: "always", Uniqueness: "server"},
				{Name: "externalId", Type: "string", MultiValued: false, Required: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
				{Name: "userName", Type: "string", MultiValued: false, Required: true, Mutability: "readWrite", Returned: "default", Uniqueness: "server"},
				{Name: "displayName", Type: "string", MultiValued: false, Required: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
				{Name: "emails", Type: "complex", MultiValued: true, Required: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none", SubAttributes: []SCIMSchemaAttribute{
					{Name: "value", Type: "string", MultiValued: false, Required: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
					{Name: "primary", Type: "boolean", MultiValued: false, Required: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
					{Name: "type", Type: "string", MultiValued: false, Required: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
				}},
				{Name: "active", Type: "boolean", MultiValued: false, Required: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
				{Name: "locale", Type: "string", MultiValued: false, Required: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
				{Name: "timezone", Type: "string", MultiValued: false, Required: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
				{Name: "meta", Type: "complex", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "always", Uniqueness: "none", SubAttributes: []SCIMSchemaAttribute{
					{Name: "resourceType", Type: "string", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "always", Uniqueness: "none"},
					{Name: "created", Type: "dateTime", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "always", Uniqueness: "none"},
					{Name: "lastModified", Type: "dateTime", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "always", Uniqueness: "none"},
					{Name: "location", Type: "reference", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "always", Uniqueness: "none"},
					{Name: "version", Type: "string", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "always", Uniqueness: "none"},
				}},
			},
			Meta: SCIMMeta{ResourceType: "Schema", Created: now, LastModified: now, Location: schemaDocLocation(r, SchemaCoreUser), Version: SCIMETag(1)},
		},
		{
			Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"},
			ID:          SchemaCoreGroup,
			Name:        "Group",
			Description: "SCIM Group",
			Attributes: []SCIMSchemaAttribute{
				{Name: "id", Type: "string", MultiValued: false, Required: true, Mutability: "readOnly", Returned: "always", Uniqueness: "server"},
				{Name: "externalId", Type: "string", MultiValued: false, Required: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
				{Name: "displayName", Type: "string", MultiValued: false, Required: true, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
				{Name: "members", Type: "complex", MultiValued: true, Required: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none", SubAttributes: []SCIMSchemaAttribute{
					{Name: "value", Type: "string", MultiValued: false, Required: true, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
					{Name: "display", Type: "string", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "default", Uniqueness: "none"},
				}},
				{Name: "meta", Type: "complex", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "always", Uniqueness: "none", SubAttributes: []SCIMSchemaAttribute{
					{Name: "resourceType", Type: "string", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "always", Uniqueness: "none"},
					{Name: "created", Type: "dateTime", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "always", Uniqueness: "none"},
					{Name: "lastModified", Type: "dateTime", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "always", Uniqueness: "none"},
					{Name: "location", Type: "reference", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "always", Uniqueness: "none"},
					{Name: "version", Type: "string", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "always", Uniqueness: "none"},
				}},
			},
			Meta: SCIMMeta{ResourceType: "Schema", Created: now, LastModified: now, Location: schemaDocLocation(r, SchemaCoreGroup), Version: SCIMETag(1)},
		},
		{
			Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"},
			ID:          SchemaWorkflowUserExtension,
			Name:        "WorkflowUserExtension",
			Description: "Workflow enterprise user extension",
			Attributes: []SCIMSchemaAttribute{
				{Name: "tenantId", Type: "string", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "default", Uniqueness: "none"},
				{Name: "roles", Type: "string", MultiValued: true, Required: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
				{Name: "teamId", Type: "string", MultiValued: false, Required: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
				{Name: "timezone", Type: "string", MultiValued: false, Required: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
				{Name: "authProvider", Type: "string", MultiValued: false, Required: false, Mutability: "readOnly", Returned: "default", Uniqueness: "none"},
			},
			Meta: SCIMMeta{ResourceType: "Schema", Created: now, LastModified: now, Location: schemaDocLocation(r, SchemaWorkflowUserExtension), Version: SCIMETag(1)},
		},
	}
}

func resourceTypeDocuments(r *http.Request) []SCIMResourceTypeDocument {
	now := nowRFC3339UTC()
	return []SCIMResourceTypeDocument{
		{
			Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"},
			ID:          "User",
			Name:        "User",
			Endpoint:    "/Users",
			Description: "SCIM User",
			Schema:      SchemaCoreUser,
			SchemaExtensions: []SCIMResourceTypeSchemaExtension{
				{Schema: SchemaWorkflowUserExtension, Required: false},
			},
			Meta: SCIMMeta{ResourceType: "ResourceType", Created: now, LastModified: now, Location: resourceTypeDocLocation(r, "User"), Version: SCIMETag(1)},
		},
		{
			Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"},
			ID:          "Group",
			Name:        "Group",
			Endpoint:    "/Groups",
			Description: "SCIM Group",
			Schema:      SchemaCoreGroup,
			Meta:        SCIMMeta{ResourceType: "ResourceType", Created: now, LastModified: now, Location: resourceTypeDocLocation(r, "Group"), Version: SCIMETag(1)},
		},
	}
}

func serviceProviderConfigDocument(r *http.Request) SCIMServiceProviderConfig {
	now := nowRFC3339UTC()
	return SCIMServiceProviderConfig{
		Schemas: []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		Patch:   SCIMServiceProviderConfigSupport{Supported: true},
		Bulk: SCIMServiceProviderConfigBulkSupport{
			Supported:      true,
			MaxOperations:  1000,
			MaxPayloadSize: 1048576,
		},
		Filter:         SCIMServiceProviderConfigFilterSupport{Supported: true, MaxResults: 200},
		ChangePassword: SCIMServiceProviderConfigSupport{Supported: false},
		Sort:           SCIMServiceProviderConfigSupport{Supported: true},
		ETag:           SCIMServiceProviderConfigSupport{Supported: true},
		AuthenticationSchemes: []SCIMAuthenticationScheme{
			{
				Type:        "oauthbearertoken",
				Name:        "OAuth Bearer Token",
				Description: "OAuth 2.0 bearer token",
				Primary:     true,
			},
		},
		Meta: SCIMMeta{ResourceType: "ServiceProviderConfig", Created: now, LastModified: now, Location: serviceProviderConfigLocation(r), Version: SCIMETag(1)},
	}
}

// GetSchemas handles GET /scim/v2/Schemas.
func (h *SCIMHandler) GetSchemas(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	docs := schemaDocuments(r)
	resp := SCIMListResponse[SCIMSchemaDocument]{
		Schemas:      []string{SchemaListResponse},
		TotalResults: len(docs),
		StartIndex:   1,
		ItemsPerPage: len(docs),
		Resources:    docs,
	}
	writeSCIMJSON(w, http.StatusOK, resp)
}

// GetSchema handles GET /scim/v2/Schemas/{id}.
func (h *SCIMHandler) GetSchema(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	schemaID := strings.TrimSpace(parseIDFromPath(r, "id"))
	if schemaID == "" {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "schema id is required")
		return
	}
	for _, doc := range schemaDocuments(r) {
		if strings.EqualFold(doc.ID, schemaID) {
			writeSCIMJSON(w, http.StatusOK, doc)
			return
		}
	}
	writeSCIMError(w, http.StatusNotFound, "", "schema not found")
}

// GetResourceTypes handles GET /scim/v2/ResourceTypes.
func (h *SCIMHandler) GetResourceTypes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	docs := resourceTypeDocuments(r)
	resp := SCIMListResponse[SCIMResourceTypeDocument]{
		Schemas:      []string{SchemaListResponse},
		TotalResults: len(docs),
		StartIndex:   1,
		ItemsPerPage: len(docs),
		Resources:    docs,
	}
	writeSCIMJSON(w, http.StatusOK, resp)
}

// GetResourceType handles GET /scim/v2/ResourceTypes/{name}.
func (h *SCIMHandler) GetResourceType(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	name := strings.TrimSpace(parseIDFromPath(r, "name"))
	if name == "" {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "resource type name is required")
		return
	}
	for _, doc := range resourceTypeDocuments(r) {
		if strings.EqualFold(doc.Name, name) || strings.EqualFold(doc.ID, name) {
			writeSCIMJSON(w, http.StatusOK, doc)
			return
		}
	}
	writeSCIMError(w, http.StatusNotFound, "", "resource type not found")
}

// GetServiceProviderConfig handles GET /scim/v2/ServiceProviderConfig.
func (h *SCIMHandler) GetServiceProviderConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	writeSCIMJSON(w, http.StatusOK, serviceProviderConfigDocument(r))
}
