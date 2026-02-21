package scim

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	mt "workflow-engine/internal/multitenancy"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
)

func baseURLFromRequest(r *http.Request) string {
	scheme := "http"
	if r != nil && r.TLS != nil {
		scheme = "https"
	}
	host := ""
	if r != nil {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + host
}

func mapUniqueUserError(err error) (int, string, string) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return http.StatusConflict, "uniqueness", "uniqueness violation"
	}
	if errors.Is(err, mt.ErrUsernameTaken) || errors.Is(err, mt.ErrEmailTaken) || errors.Is(err, mt.ErrExternalIDConflict) {
		return http.StatusConflict, "uniqueness", "uniqueness violation"
	}
	return http.StatusInternalServerError, "", "internal server error"
}

func writeUserMutationError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrETagMismatch) {
		writeSCIMError(w, http.StatusPreconditionFailed, "", "etag mismatch")
		return
	}
	if errors.Is(err, ErrInvalidETag) {
		writeSCIMError(w, http.StatusBadRequest, "invalidVers", "invalid etag")
		return
	}
	if errors.Is(err, mt.ErrUserNotFound) {
		writeSCIMError(w, http.StatusNotFound, "", "resource not found")
		return
	}
	if errors.Is(err, mt.ErrUserDeactivated) {
		writeSCIMError(w, http.StatusUnprocessableEntity, "invalidValue", "deactivated user cannot be reactivated")
		return
	}
	status, scimType, detail := mapUniqueUserError(err)
	writeSCIMError(w, status, scimType, detail)
}

func recordAuditInOwnTx(ctx *http.Request, db *sqlx.DB, entry SCIMAuditEntry) {
	if db == nil {
		return
	}
	tx, err := db.BeginTxx(ctx.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if err := RecordSCIMAudit(ctx.Context(), tx, entry); err != nil {
		return
	}
	_ = tx.Commit()
}

// GetUser handles GET /scim/v2/Users/{id}.
func (h *SCIMHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	claims, tenantID, err := tenantFromClaims(r.Context())
	if err != nil {
		writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
		return
	}
	userID := parseIDFromPath(r, "id")
	if userID == "" {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "user id is required")
		return
	}
	start := time.Now().UTC()
	include, exclude := parseAttributes(r)
	includeExtension := len(include) == 0 || shouldIncludeTopLevel(strings.ToLower(SchemaWorkflowUserExtension), include, exclude)

	user, version, err := getSCIMUserByID(r.Context(), h.db, tenantID, userID, baseURLFromRequest(r), includeExtension)
	if err != nil {
		if errors.Is(err, mt.ErrUserNotFound) {
			writeSCIMError(w, http.StatusNotFound, "", "resource not found")
			return
		}
		writeSCIMError(w, http.StatusInternalServerError, "", "failed to load resource")
		return
	}
	etag := SCIMETag(version)
	w.Header().Set("ETag", etag)
	if HandleIfNoneMatch(version, r.Header.Get("If-None-Match")) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeSCIMJSON(w, http.StatusOK, projectSCIMUser(user, include, exclude))

	resourceID := userID
	entry := SCIMAuditEntry{
		TenantID:          tenantID,
		TokenID:           &claims.TokenID,
		Operation:         "GET",
		ResourceType:      "USER",
		ResourceID:        &resourceID,
		HTTPStatus:        http.StatusOK,
		DurationMS:        int(time.Since(start).Milliseconds()),
		RequestAttributes: keysFromSets(include, exclude),
		IPAddress:         anonymizeIP(r),
		UserAgent:         strings.TrimSpace(r.UserAgent()),
		OccurredAt:        time.Now().UTC(),
	}
	recordAuditInOwnTx(r, h.db, entry)
}

func keysFromSets(include, exclude map[string]struct{}) []string {
	out := make([]string, 0, len(include)+len(exclude))
	for k := range include {
		out = append(out, "attributes:"+k)
	}
	for k := range exclude {
		out = append(out, "excluded:"+k)
	}
	return out
}

// ListUsers handles GET /scim/v2/Users.
func (h *SCIMHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	claims, tenantID, err := tenantFromClaims(r.Context())
	if err != nil {
		writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
		return
	}
	start := time.Now().UTC()
	startIndex, count, err := parsePaginationParams(r)
	if err != nil {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", err.Error())
		return
	}
	filterExpr := strings.TrimSpace(r.URL.Query().Get("filter"))
	sortBy := strings.TrimSpace(r.URL.Query().Get("sortBy"))
	sortOrder := normalizeSortOrder(r.URL.Query().Get("sortOrder"))
	include, exclude := parseAttributes(r)
	includeExtension := len(include) == 0 || shouldIncludeTopLevel(strings.ToLower(SchemaWorkflowUserExtension), include, exclude)

	users, total, err := listSCIMUsers(
		r.Context(),
		h.db,
		tenantID,
		filterExpr,
		sortBy,
		sortOrder,
		startIndex,
		count,
		baseURLFromRequest(r),
		includeExtension,
	)
	if err != nil {
		if errors.Is(err, ErrInvalidSCIMFilter) {
			scimType := "invalidFilter"
			if strings.Contains(strings.ToLower(err.Error()), "toomany") {
				scimType = "tooMany"
			}
			writeSCIMError(w, http.StatusBadRequest, scimType, "invalid filter expression")
			return
		}
		writeSCIMError(w, http.StatusInternalServerError, "", "failed to list resources")
		return
	}

	projected := make([]map[string]interface{}, 0, len(users))
	for i := range users {
		projected = append(projected, projectSCIMUser(users[i], include, exclude))
	}
	resp := map[string]interface{}{
		"schemas":      []string{SchemaListResponse},
		"totalResults": total,
		"startIndex":   startIndex,
		"itemsPerPage": len(projected),
		"Resources":    projected,
	}
	writeSCIMJSON(w, http.StatusOK, resp)

	entry := SCIMAuditEntry{
		TenantID:          tenantID,
		TokenID:           &claims.TokenID,
		Operation:         "GET",
		ResourceType:      "USER",
		HTTPStatus:        http.StatusOK,
		FilterExpression:  nullableStringPtr(filterExpr),
		DurationMS:        int(time.Since(start).Milliseconds()),
		RequestAttributes: keysFromSets(include, exclude),
		IPAddress:         anonymizeIP(r),
		UserAgent:         strings.TrimSpace(r.UserAgent()),
		OccurredAt:        time.Now().UTC(),
	}
	recordAuditInOwnTx(r, h.db, entry)
}

func nullableStringPtr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

// CreateUser handles POST /scim/v2/Users.
func (h *SCIMHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	claims, tenantID, err := tenantFromClaims(r.Context())
	if err != nil {
		writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
		return
	}
	start := time.Now().UTC()

	var req SCIMUser
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "invalidSyntax", "invalid request body")
		return
	}
	email := extractPrimaryEmail(req.Emails)
	if strings.TrimSpace(req.UserName) == "" || strings.TrimSpace(email) == "" {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "userName and emails are required")
		return
	}
	status := mt.UserStatusActive
	if req.Active != nil && !*req.Active {
		status = mt.UserStatusSuspended
	}
	input := mt.CreateUserInput{
		TenantID:     tenantID,
		Username:     strings.TrimSpace(req.UserName),
		Email:        strings.TrimSpace(email),
		DisplayName:  strings.TrimSpace(req.DisplayName),
		Status:       status,
		AuthProvider: mt.AuthProviderOIDC,
		ExternalID:   nullableStringPtr(req.ExternalID),
		Timezone:     strings.TrimSpace(req.Timezone),
		Locale:       strings.TrimSpace(req.Locale),
		Metadata:     json.RawMessage(`{}`),
		CreatedBy:    claims.TokenID,
	}
	if input.DisplayName == "" {
		input.DisplayName = input.Username
	}
	created, err := mt.CreateUser(r.Context(), h.db, input)
	if err != nil {
		statusCode, scimType, detail := mapUniqueUserError(err)
		writeSCIMError(w, statusCode, scimType, detail)
		return
	}

	if req.WorkflowUserExtension != nil {
		if err := reconcileUserExtensionAssignments(r.Context(), h.db, tenantID, created.UserID, claims.TokenID, req.WorkflowUserExtension); err != nil {
			writeUserMutationError(w, err)
			return
		}
	}

	createdResource, version, err := getSCIMUserByID(r.Context(), h.db, tenantID, created.UserID, baseURLFromRequest(r), true)
	if err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "", "user created but retrieval failed")
		return
	}
	location := userLocation(baseURLFromRequest(r), created.UserID)
	w.Header().Set("Location", location)
	w.Header().Set("ETag", SCIMETag(version))
	writeSCIMJSON(w, http.StatusCreated, createdResource)

	resourceID := created.UserID
	entry := SCIMAuditEntry{
		TenantID:          tenantID,
		TokenID:           &claims.TokenID,
		Operation:         "POST",
		ResourceType:      "USER",
		ResourceID:        &resourceID,
		HTTPStatus:        http.StatusCreated,
		DurationMS:        int(time.Since(start).Milliseconds()),
		RequestAttributes: []string{"userName", "emails", "displayName", "active", "locale", "timezone"},
		IPAddress:         anonymizeIP(r),
		UserAgent:         strings.TrimSpace(r.UserAgent()),
		OccurredAt:        time.Now().UTC(),
	}
	recordAuditInOwnTx(r, h.db, entry)
}

func reconcileUserExtensionAssignments(ctx context.Context, db *sqlx.DB, tenantID, userID, actorID string, ext *SCIMWorkflowUserExtension) error {
	if ext == nil {
		return nil
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		actorID = userID
	}
	if len(ext.Roles) > 0 {
		current, err := mt.GetUserRoles(ctx, db, userID, tenantID)
		if err != nil {
			return fmt.Errorf("reconcileUserExtensionAssignments: load current roles: %w", err)
		}
		curr := make(map[string]string)
		for i := range current {
			curr[strings.ToUpper(strings.TrimSpace(current[i].RoleCode))] = current[i].RoleID
		}
		target := make(map[string]struct{})
		for i := range ext.Roles {
			code := strings.ToUpper(strings.TrimSpace(ext.Roles[i]))
			if code == "" {
				continue
			}
			target[code] = struct{}{}
		}
		for code := range target {
			if _, ok := curr[code]; ok {
				continue
			}
			var roleID string
			if err := db.GetContext(ctx, &roleID, `
				SELECT role_id::text
				FROM roles
				WHERE tenant_id = $1::uuid
				  AND role_code = $2
			`, tenantID, code); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("reconcileUserExtensionAssignments: %w", mt.ErrRoleNotFound)
				}
				return fmt.Errorf("reconcileUserExtensionAssignments: lookup role: %w", err)
			}
			if err := mt.AssignRoleToUser(ctx, db, userID, roleID, tenantID, userID); err != nil {
				return fmt.Errorf("reconcileUserExtensionAssignments: assign role: %w", err)
			}
		}
		for code, roleID := range curr {
			if _, ok := target[code]; ok {
				continue
			}
			if err := mt.RevokeRoleFromUser(ctx, db, userID, roleID, tenantID, actorID); err != nil {
				return fmt.Errorf("reconcileUserExtensionAssignments: revoke role: %w", err)
			}
		}
	}
	if strings.TrimSpace(ext.TeamID) != "" {
		if err := mt.AddUserToTeam(ctx, db, strings.TrimSpace(ext.TeamID), userID, tenantID, mt.TeamMemberRoleMember, userID); err != nil {
			return fmt.Errorf("reconcileUserExtensionAssignments: assign team: %w", err)
		}
	}
	return nil
}

func updateUserCoreFields(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	userID string,
	username string,
	email string,
	displayName string,
	locale string,
	timezone string,
) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE users
		SET username = $1,
		    email = $2,
		    display_name = $3,
		    full_name = $3,
		    locale = $4,
		    timezone = $5,
		    updated_at = now()
		WHERE tenant_id = $6::uuid
		  AND user_id = $7::uuid
	`, username, email, displayName, locale, timezone, tenantID, userID)
	if err != nil {
		return fmt.Errorf("updateUserCoreFields: %w", err)
	}
	return nil
}

func changeUserActiveState(ctx context.Context, db *sqlx.DB, tenantID string, userID string, active bool, actor string) error {
	if active {
		if err := mt.ReactivateUser(ctx, db, userID, tenantID, actor); err != nil {
			return fmt.Errorf("changeUserActiveState: %w", err)
		}
		return nil
	}
	if err := mt.SuspendUser(ctx, db, userID, tenantID, actor, "SCIM active=false"); err != nil {
		return fmt.Errorf("changeUserActiveState: %w", err)
	}
	return nil
}

// ReplaceUser handles PUT /scim/v2/Users/{id}.
func (h *SCIMHandler) ReplaceUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	claims, tenantID, err := tenantFromClaims(r.Context())
	if err != nil {
		writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
		return
	}
	userID := parseIDFromPath(r, "id")
	if userID == "" {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "user id is required")
		return
	}
	ifMatch := strings.TrimSpace(r.Header.Get("If-Match"))
	if ifMatch != "" {
		if err := ValidateIfMatch(r.Context(), h.db, userID, tenantID, ifMatch); err != nil {
			writeUserMutationError(w, err)
			return
		}
	}

	var req SCIMUser
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "invalidSyntax", "invalid request body")
		return
	}
	email := extractPrimaryEmail(req.Emails)
	if strings.TrimSpace(req.UserName) == "" || strings.TrimSpace(email) == "" {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "userName and emails are required")
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(req.UserName)
	}
	locale := strings.TrimSpace(req.Locale)
	if locale == "" {
		locale = "en-US"
	}
	timezone := strings.TrimSpace(req.Timezone)
	if timezone == "" {
		timezone = "UTC"
	}

	tx, err := h.db.BeginTxx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "", "failed to start transaction")
		return
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := updateUserCoreFields(r.Context(), tx, tenantID, userID, strings.TrimSpace(req.UserName), strings.TrimSpace(email), displayName, locale, timezone); err != nil {
		status, scimType, detail := mapUniqueUserError(err)
		writeSCIMError(w, status, scimType, detail)
		return
	}

	if req.WorkflowUserExtension != nil {
		if err := tx.Commit(); err != nil {
			writeSCIMError(w, http.StatusInternalServerError, "", "commit failed")
			return
		}
		if err := reconcileUserExtensionAssignments(r.Context(), h.db, tenantID, userID, claims.TokenID, req.WorkflowUserExtension); err != nil {
			writeUserMutationError(w, err)
			return
		}
	} else {
		if err := tx.Commit(); err != nil {
			writeSCIMError(w, http.StatusInternalServerError, "", "commit failed")
			return
		}
	}

	if req.Active != nil {
		if err := changeUserActiveState(r.Context(), h.db, tenantID, userID, *req.Active, claims.TokenID); err != nil {
			writeUserMutationError(w, err)
			return
		}
	}

	updated, version, err := getSCIMUserByID(r.Context(), h.db, tenantID, userID, baseURLFromRequest(r), true)
	if err != nil {
		writeUserMutationError(w, err)
		return
	}
	w.Header().Set("ETag", SCIMETag(version))
	writeSCIMJSON(w, http.StatusOK, updated)
}

func normalizePatchPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$")
	path = strings.Trim(path, "/")
	return strings.ToLower(path)
}

func applyUserPatchOperation(state *SCIMUser, op SCIMPatchOperation) (changedActive *bool, err error) {
	opName := strings.ToLower(strings.TrimSpace(op.Op))
	if opName != "add" && opName != "remove" && opName != "replace" {
		return nil, fmt.Errorf("unsupported patch op")
	}
	path := normalizePatchPath(op.Path)
	if path == "" {
		return nil, fmt.Errorf("patch path required")
	}
	setString := func(target *string, value interface{}) {
		*target = strings.TrimSpace(anyToString(value))
	}
	switch path {
	case "username":
		if opName == "remove" {
			return nil, fmt.Errorf("cannot remove userName")
		}
		setString(&state.UserName, op.Value)
	case "displayname":
		if opName == "remove" {
			state.DisplayName = ""
			return nil, nil
		}
		setString(&state.DisplayName, op.Value)
	case "emails[primary].value", "emails.value":
		if opName == "remove" {
			state.Emails = nil
			return nil, nil
		}
		v := strings.TrimSpace(anyToString(op.Value))
		state.Emails = []SCIMEmail{{Value: v, Primary: true, Type: "work"}}
	case "active":
		if opName == "remove" {
			return nil, fmt.Errorf("cannot remove active")
		}
		b, ok := op.Value.(bool)
		if !ok {
			return nil, fmt.Errorf("active must be boolean")
		}
		state.Active = boolPtr(b)
		return &b, nil
	case "locale":
		if opName == "remove" {
			state.Locale = ""
			return nil, nil
		}
		setString(&state.Locale, op.Value)
	case "timezone":
		if opName == "remove" {
			state.Timezone = ""
			return nil, nil
		}
		setString(&state.Timezone, op.Value)
	case strings.ToLower(SchemaWorkflowUserExtension + ":roles"), "urn:workflow:user:roles":
		if state.WorkflowUserExtension == nil {
			state.WorkflowUserExtension = &SCIMWorkflowUserExtension{}
		}
		if opName == "remove" {
			state.WorkflowUserExtension.Roles = []string{}
			return nil, nil
		}
		roles := make([]string, 0)
		switch v := op.Value.(type) {
		case []interface{}:
			for i := range v {
				role := strings.TrimSpace(anyToString(v[i]))
				if role != "" {
					roles = append(roles, role)
				}
			}
		case []string:
			for i := range v {
				role := strings.TrimSpace(v[i])
				if role != "" {
					roles = append(roles, role)
				}
			}
		default:
			return nil, fmt.Errorf("roles must be an array")
		}
		state.WorkflowUserExtension.Roles = roles
	case strings.ToLower(SchemaWorkflowUserExtension + ":teamid"), "urn:workflow:user:teamid":
		if state.WorkflowUserExtension == nil {
			state.WorkflowUserExtension = &SCIMWorkflowUserExtension{}
		}
		if opName == "remove" {
			state.WorkflowUserExtension.TeamID = ""
			return nil, nil
		}
		state.WorkflowUserExtension.TeamID = strings.TrimSpace(anyToString(op.Value))
	default:
		return nil, fmt.Errorf("path not found")
	}
	return nil, nil
}

// PatchUser handles PATCH /scim/v2/Users/{id}.
func (h *SCIMHandler) PatchUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	claims, tenantID, err := tenantFromClaims(r.Context())
	if err != nil {
		writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
		return
	}
	userID := parseIDFromPath(r, "id")
	if userID == "" {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "user id is required")
		return
	}
	ifMatch := strings.TrimSpace(r.Header.Get("If-Match"))
	if ifMatch != "" {
		if err := ValidateIfMatch(r.Context(), h.db, userID, tenantID, ifMatch); err != nil {
			writeUserMutationError(w, err)
			return
		}
	}

	var req SCIMPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "invalidSyntax", "invalid request body")
		return
	}
	if len(req.Operations) == 0 {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "at least one patch operation is required")
		return
	}

	current, _, err := getSCIMUserByID(r.Context(), h.db, tenantID, userID, baseURLFromRequest(r), true)
	if err != nil {
		writeUserMutationError(w, err)
		return
	}
	state := current
	var changedActive *bool
	for i := range req.Operations {
		activeChange, patchErr := applyUserPatchOperation(&state, req.Operations[i])
		if patchErr != nil {
			if strings.Contains(strings.ToLower(patchErr.Error()), "path not found") {
				writeSCIMError(w, http.StatusBadRequest, "noTarget", "patch path does not exist")
				return
			}
			writeSCIMError(w, http.StatusUnprocessableEntity, "", "invalid patch operation")
			return
		}
		if activeChange != nil {
			changedActive = activeChange
		}
	}

	if strings.TrimSpace(state.DisplayName) == "" {
		state.DisplayName = state.UserName
	}
	email := extractPrimaryEmail(state.Emails)
	if strings.TrimSpace(email) == "" {
		email = current.Emails[0].Value
	}
	if strings.TrimSpace(state.Locale) == "" {
		state.Locale = current.Locale
	}
	if strings.TrimSpace(state.Timezone) == "" {
		state.Timezone = current.Timezone
	}

	tx, err := h.db.BeginTxx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "", "failed to start transaction")
		return
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if err := updateUserCoreFields(r.Context(), tx, tenantID, userID, strings.TrimSpace(state.UserName), strings.TrimSpace(email), strings.TrimSpace(state.DisplayName), strings.TrimSpace(state.Locale), strings.TrimSpace(state.Timezone)); err != nil {
		statusCode, scimType, detail := mapUniqueUserError(err)
		writeSCIMError(w, statusCode, scimType, detail)
		return
	}
	if err := tx.Commit(); err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "", "commit failed")
		return
	}

	if state.WorkflowUserExtension != nil {
		if err := reconcileUserExtensionAssignments(r.Context(), h.db, tenantID, userID, claims.TokenID, state.WorkflowUserExtension); err != nil {
			writeUserMutationError(w, err)
			return
		}
	}
	if changedActive != nil {
		if err := changeUserActiveState(r.Context(), h.db, tenantID, userID, *changedActive, claims.TokenID); err != nil {
			writeUserMutationError(w, err)
			return
		}
	}

	updated, version, err := getSCIMUserByID(r.Context(), h.db, tenantID, userID, baseURLFromRequest(r), true)
	if err != nil {
		writeUserMutationError(w, err)
		return
	}
	w.Header().Set("ETag", SCIMETag(version))
	writeSCIMJSON(w, http.StatusOK, updated)
}

// DeleteUser handles DELETE /scim/v2/Users/{id}.
func (h *SCIMHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	claims, tenantID, err := tenantFromClaims(r.Context())
	if err != nil {
		writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
		return
	}
	userID := parseIDFromPath(r, "id")
	if userID == "" {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "user id is required")
		return
	}
	if err := mt.DeactivateUser(r.Context(), h.db, userID, tenantID, claims.TokenID, "SCIM DELETE"); err != nil {
		if errors.Is(err, mt.ErrUserNotFound) {
			writeSCIMError(w, http.StatusNotFound, "", "resource not found")
			return
		}
		writeSCIMError(w, http.StatusInternalServerError, "", "failed to delete user")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
