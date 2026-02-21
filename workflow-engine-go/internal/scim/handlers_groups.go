package scim

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	mt "workflow-engine/internal/multitenancy"

	"github.com/jmoiron/sqlx"
)

var nonCodePattern = regexp.MustCompile(`[^A-Z0-9]+`)

func generateTeamCode(displayName string) string {
	base := strings.ToUpper(strings.TrimSpace(displayName))
	base = nonCodePattern.ReplaceAllString(base, "_")
	base = strings.Trim(base, "_")
	if base == "" {
		base = "TEAM"
	}
	if len(base) > 90 {
		base = base[:90]
	}
	suffix := fmt.Sprintf("_%d", time.Now().UTC().UnixNano()%1000000)
	return base + suffix
}

// GetGroup handles GET /scim/v2/Groups/{id}.
func (h *SCIMHandler) GetGroup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	_, tenantID, err := tenantFromClaims(r.Context())
	if err != nil {
		writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
		return
	}
	groupID := parseIDFromPath(r, "id")
	if strings.TrimSpace(groupID) == "" {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "group id is required")
		return
	}
	group, version, err := getSCIMGroupByID(r.Context(), h.db, tenantID, groupID, baseURLFromRequest(r))
	if err != nil {
		if errors.Is(err, mt.ErrTeamNotFound) {
			writeSCIMError(w, http.StatusNotFound, "", "resource not found")
			return
		}
		writeSCIMError(w, http.StatusInternalServerError, "", "failed to load resource")
		return
	}
	w.Header().Set("ETag", SCIMETag(version))
	if HandleIfNoneMatch(version, r.Header.Get("If-None-Match")) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	include, exclude := parseAttributes(r)
	writeSCIMJSON(w, http.StatusOK, projectSCIMGroup(group, include, exclude))
}

// ListGroups handles GET /scim/v2/Groups.
func (h *SCIMHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	_, tenantID, err := tenantFromClaims(r.Context())
	if err != nil {
		writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
		return
	}
	startIndex, count, err := parsePaginationParams(r)
	if err != nil {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", err.Error())
		return
	}
	filterExpr := strings.TrimSpace(r.URL.Query().Get("filter"))
	sortBy := strings.TrimSpace(r.URL.Query().Get("sortBy"))
	sortOrder := normalizeSortOrder(r.URL.Query().Get("sortOrder"))
	include, exclude := parseAttributes(r)

	groups, total, err := listSCIMGroups(r.Context(), h.db, tenantID, filterExpr, sortBy, sortOrder, startIndex, count, baseURLFromRequest(r))
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

	projected := make([]map[string]interface{}, 0, len(groups))
	for i := range groups {
		projected = append(projected, projectSCIMGroup(groups[i], include, exclude))
	}
	resp := map[string]interface{}{
		"schemas":      []string{SchemaListResponse},
		"totalResults": total,
		"startIndex":   startIndex,
		"itemsPerPage": len(projected),
		"Resources":    projected,
	}
	writeSCIMJSON(w, http.StatusOK, resp)
}

func parseGroupMemberIDs(members []SCIMGroupMember) []string {
	uniq := make(map[string]struct{})
	out := make([]string, 0, len(members))
	for i := range members {
		id := strings.TrimSpace(members[i].Value)
		if id == "" {
			continue
		}
		if _, ok := uniq[id]; ok {
			continue
		}
		uniq[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func validateUsersExistForMembership(ctx context.Context, tx *sqlx.Tx, tenantID string, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	rows := make([]string, 0)
	if err := tx.SelectContext(ctx, &rows, `
		SELECT user_id::text
		FROM users
		WHERE tenant_id = $1::uuid
		  AND user_id = ANY($2::uuid[])
	`, tenantID, userIDs); err != nil {
		return fmt.Errorf("validateUsersExistForMembership: query users: %w", err)
	}
	if len(rows) != len(userIDs) {
		return fmt.Errorf("validateUsersExistForMembership: one or more members do not exist in tenant")
	}
	return nil
}

// CreateGroup handles POST /scim/v2/Groups.
func (h *SCIMHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	_, tenantID, err := tenantFromClaims(r.Context())
	if err != nil {
		writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
		return
	}
	var req SCIMGroup
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "invalidSyntax", "invalid request body")
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "displayName is required")
		return
	}

	memberIDs := parseGroupMemberIDs(req.Members)
	tx, err := h.db.BeginTxx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "", "failed to start transaction")
		return
	}
	defer func() {
		_ = tx.Rollback()
	}()

	teamCode := generateTeamCode(displayName)
	var groupID string
	if err := tx.GetContext(r.Context(), &groupID, `
		INSERT INTO teams (
			tenant_id,
			team_code,
			display_name,
			team_type,
			status,
			metadata,
			external_id
		)
		VALUES (
			$1::uuid,
			$2,
			$3,
			'MIXED',
			'ACTIVE',
			'{}'::jsonb,
			$4
		)
		RETURNING team_id::text
	`, tenantID, teamCode, displayName, nullableStringPtr(req.ExternalID)); err != nil {
		writeSCIMError(w, http.StatusConflict, "uniqueness", "group uniqueness violation")
		return
	}

	if err := validateUsersExistForMembership(r.Context(), tx, tenantID, memberIDs); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "one or more members are invalid")
		return
	}
	if len(memberIDs) > 0 {
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO team_members (
				team_id,
				user_id,
				tenant_id,
				role_in_team,
				joined_at,
				added_by
			)
			SELECT
				$1::uuid,
				u,
				$2::uuid,
				'MEMBER',
				now(),
				u
			FROM unnest($3::uuid[]) AS u
			ON CONFLICT (team_id, user_id)
			DO UPDATE SET role_in_team = EXCLUDED.role_in_team
		`, groupID, tenantID, memberIDs); err != nil {
			writeSCIMError(w, http.StatusInternalServerError, "", "failed to add members")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "", "commit failed")
		return
	}

	created, version, err := getSCIMGroupByID(r.Context(), h.db, tenantID, groupID, baseURLFromRequest(r))
	if err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "", "group created but retrieval failed")
		return
	}
	location := groupLocation(baseURLFromRequest(r), groupID)
	w.Header().Set("Location", location)
	w.Header().Set("ETag", SCIMETag(version))
	writeSCIMJSON(w, http.StatusCreated, created)
}

func diffMembers(current []SCIMGroupMember, target []SCIMGroupMember) (toAdd []string, toRemove []string) {
	curr := make(map[string]struct{})
	tgt := make(map[string]struct{})
	for i := range current {
		id := strings.TrimSpace(current[i].Value)
		if id != "" {
			curr[id] = struct{}{}
		}
	}
	for i := range target {
		id := strings.TrimSpace(target[i].Value)
		if id != "" {
			tgt[id] = struct{}{}
		}
	}
	for id := range tgt {
		if _, ok := curr[id]; !ok {
			toAdd = append(toAdd, id)
		}
	}
	for id := range curr {
		if _, ok := tgt[id]; !ok {
			toRemove = append(toRemove, id)
		}
	}
	return toAdd, toRemove
}

func replaceGroupMembersTx(ctx context.Context, tx *sqlx.Tx, tenantID, groupID string, members []SCIMGroupMember) error {
	currentRows := make([]string, 0)
	if err := tx.SelectContext(ctx, &currentRows, `
		SELECT user_id::text
		FROM team_members
		WHERE tenant_id = $1::uuid
		  AND team_id = $2::uuid
	`, tenantID, groupID); err != nil {
		return fmt.Errorf("replaceGroupMembersTx: load current members: %w", err)
	}
	currentMembers := make([]SCIMGroupMember, 0, len(currentRows))
	for i := range currentRows {
		currentMembers = append(currentMembers, SCIMGroupMember{Value: currentRows[i]})
	}
	toAdd, toRemove := diffMembers(currentMembers, members)
	if err := validateUsersExistForMembership(ctx, tx, tenantID, toAdd); err != nil {
		return fmt.Errorf("replaceGroupMembersTx: validate additions: %w", err)
	}
	if len(toAdd) > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO team_members (
				team_id,
				user_id,
				tenant_id,
				role_in_team,
				joined_at,
				added_by
			)
			SELECT
				$1::uuid,
				u,
				$2::uuid,
				'MEMBER',
				now(),
				u
			FROM unnest($3::uuid[]) AS u
			ON CONFLICT (team_id, user_id)
			DO UPDATE SET role_in_team = EXCLUDED.role_in_team
		`, groupID, tenantID, toAdd); err != nil {
			return fmt.Errorf("replaceGroupMembersTx: insert additions: %w", err)
		}
	}
	if len(toRemove) > 0 {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM team_members
			WHERE tenant_id = $1::uuid
			  AND team_id = $2::uuid
			  AND user_id = ANY($3::uuid[])
		`, tenantID, groupID, toRemove); err != nil {
			return fmt.Errorf("replaceGroupMembersTx: delete removals: %w", err)
		}
	}
	return nil
}

// ReplaceGroup handles PUT /scim/v2/Groups/{id}.
func (h *SCIMHandler) ReplaceGroup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	_, tenantID, err := tenantFromClaims(r.Context())
	if err != nil {
		writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
		return
	}
	groupID := parseIDFromPath(r, "id")
	if groupID == "" {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "group id is required")
		return
	}
	ifMatch := strings.TrimSpace(r.Header.Get("If-Match"))
	if ifMatch != "" {
		if err := validateIfMatchTeam(r.Context(), h.db, groupID, tenantID, ifMatch); err != nil {
			if errors.Is(err, ErrETagMismatch) {
				writeSCIMError(w, http.StatusPreconditionFailed, "", "etag mismatch")
				return
			}
			writeSCIMError(w, http.StatusBadRequest, "invalidVers", "invalid etag")
			return
		}
	}

	var req SCIMGroup
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "invalidSyntax", "invalid request body")
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "displayName is required")
		return
	}

	tx, err := h.db.BeginTxx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "", "failed to start transaction")
		return
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(r.Context(), `
		UPDATE teams
		SET display_name = $1,
		    external_id = $2,
		    updated_at = now()
		WHERE tenant_id = $3::uuid
		  AND team_id = $4::uuid
	`, displayName, nullableStringPtr(req.ExternalID), tenantID, groupID); err != nil {
		writeSCIMError(w, http.StatusConflict, "uniqueness", "group uniqueness violation")
		return
	}

	if err := replaceGroupMembersTx(r.Context(), tx, tenantID, groupID, req.Members); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "invalid group members")
		return
	}

	if err := tx.Commit(); err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "", "commit failed")
		return
	}

	updated, version, err := getSCIMGroupByID(r.Context(), h.db, tenantID, groupID, baseURLFromRequest(r))
	if err != nil {
		if errors.Is(err, mt.ErrTeamNotFound) {
			writeSCIMError(w, http.StatusNotFound, "", "resource not found")
			return
		}
		writeSCIMError(w, http.StatusInternalServerError, "", "failed to load updated group")
		return
	}
	w.Header().Set("ETag", SCIMETag(version))
	writeSCIMJSON(w, http.StatusOK, updated)
}

func parsePatchMembersValue(value interface{}) ([]SCIMGroupMember, error) {
	members := make([]SCIMGroupMember, 0)
	switch v := value.(type) {
	case []interface{}:
		for i := range v {
			item, ok := v[i].(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("invalid members value")
			}
			memberID := strings.TrimSpace(anyToString(item["value"]))
			if memberID == "" {
				continue
			}
			members = append(members, SCIMGroupMember{Value: memberID, Display: strings.TrimSpace(anyToString(item["display"]))})
		}
	case map[string]interface{}:
		memberID := strings.TrimSpace(anyToString(v["value"]))
		if memberID != "" {
			members = append(members, SCIMGroupMember{Value: memberID, Display: strings.TrimSpace(anyToString(v["display"]))})
		}
	default:
		return nil, fmt.Errorf("invalid members value")
	}
	return members, nil
}

// PatchGroup handles PATCH /scim/v2/Groups/{id}.
func (h *SCIMHandler) PatchGroup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	_, tenantID, err := tenantFromClaims(r.Context())
	if err != nil {
		writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
		return
	}
	groupID := parseIDFromPath(r, "id")
	if groupID == "" {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "group id is required")
		return
	}
	ifMatch := strings.TrimSpace(r.Header.Get("If-Match"))
	if ifMatch != "" {
		if err := validateIfMatchTeam(r.Context(), h.db, groupID, tenantID, ifMatch); err != nil {
			if errors.Is(err, ErrETagMismatch) {
				writeSCIMError(w, http.StatusPreconditionFailed, "", "etag mismatch")
				return
			}
			writeSCIMError(w, http.StatusBadRequest, "invalidVers", "invalid etag")
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

	current, _, err := getSCIMGroupByID(r.Context(), h.db, tenantID, groupID, baseURLFromRequest(r))
	if err != nil {
		if errors.Is(err, mt.ErrTeamNotFound) {
			writeSCIMError(w, http.StatusNotFound, "", "resource not found")
			return
		}
		writeSCIMError(w, http.StatusInternalServerError, "", "failed to load group")
		return
	}

	target := current
	for i := range req.Operations {
		op := strings.ToLower(strings.TrimSpace(req.Operations[i].Op))
		if op != "add" && op != "remove" && op != "replace" {
			writeSCIMError(w, http.StatusUnprocessableEntity, "", "invalid patch operation")
			return
		}
		path := normalizePatchPath(req.Operations[i].Path)
		switch path {
		case "displayname":
			if op == "remove" {
				writeSCIMError(w, http.StatusUnprocessableEntity, "", "displayName cannot be removed")
				return
			}
			target.DisplayName = strings.TrimSpace(anyToString(req.Operations[i].Value))
		case "members":
			patchMembers, err := parsePatchMembersValue(req.Operations[i].Value)
			if err != nil {
				writeSCIMError(w, http.StatusUnprocessableEntity, "", "invalid members patch value")
				return
			}
			switch op {
			case "add":
				currSet := make(map[string]struct{})
				for _, m := range target.Members {
					currSet[strings.TrimSpace(m.Value)] = struct{}{}
				}
				for _, m := range patchMembers {
					if _, ok := currSet[strings.TrimSpace(m.Value)]; !ok {
						target.Members = append(target.Members, m)
					}
				}
			case "remove":
				removeSet := make(map[string]struct{})
				for _, m := range patchMembers {
					removeSet[strings.TrimSpace(m.Value)] = struct{}{}
				}
				kept := make([]SCIMGroupMember, 0, len(target.Members))
				for _, m := range target.Members {
					if _, remove := removeSet[strings.TrimSpace(m.Value)]; !remove {
						kept = append(kept, m)
					}
				}
				target.Members = kept
			case "replace":
				target.Members = patchMembers
			}
		default:
			writeSCIMError(w, http.StatusBadRequest, "noTarget", "patch path does not exist")
			return
		}
	}
	if strings.TrimSpace(target.DisplayName) == "" {
		target.DisplayName = current.DisplayName
	}

	tx, err := h.db.BeginTxx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "", "failed to start transaction")
		return
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE teams
		SET display_name = $1,
		    updated_at = now()
		WHERE tenant_id = $2::uuid
		  AND team_id = $3::uuid
	`, target.DisplayName, tenantID, groupID); err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "", "failed to update group")
		return
	}
	if err := replaceGroupMembersTx(r.Context(), tx, tenantID, groupID, target.Members); err != nil {
		writeSCIMError(w, http.StatusUnprocessableEntity, "", "invalid patch operation")
		return
	}
	if err := tx.Commit(); err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "", "commit failed")
		return
	}

	updated, version, err := getSCIMGroupByID(r.Context(), h.db, tenantID, groupID, baseURLFromRequest(r))
	if err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "", "failed to load updated group")
		return
	}
	w.Header().Set("ETag", SCIMETag(version))
	writeSCIMJSON(w, http.StatusOK, updated)
}

// DeleteGroup handles DELETE /scim/v2/Groups/{id}.
func (h *SCIMHandler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	claims, tenantID, err := tenantFromClaims(r.Context())
	if err != nil {
		writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
		return
	}
	groupID := parseIDFromPath(r, "id")
	if groupID == "" {
		writeSCIMError(w, http.StatusBadRequest, "invalidValue", "group id is required")
		return
	}
	if err := mt.DisbandTeam(r.Context(), h.db, groupID, tenantID, claims.TokenID); err != nil {
		if errors.Is(err, mt.ErrTeamNotFound) {
			writeSCIMError(w, http.StatusNotFound, "", "resource not found")
			return
		}
		if errors.Is(err, mt.ErrTeamHasOpenTasks) {
			writeSCIMError(w, http.StatusConflict, "", err.Error())
			return
		}
		writeSCIMError(w, http.StatusInternalServerError, "", "failed to delete group")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
