package scim

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const scimContentType = "application/scim+json"

func writeSCIMJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", scimContentType)
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func writeSCIMError(w http.ResponseWriter, status int, scimType string, detail string) {
	errPayload := SCIMError{
		Schemas: []string{SchemaError},
		Status:  strconv.Itoa(status),
		Detail:  strings.TrimSpace(detail),
	}
	if errPayload.Detail == "" {
		errPayload.Detail = http.StatusText(status)
	}
	if strings.TrimSpace(scimType) != "" {
		errPayload.ScimType = strings.TrimSpace(scimType)
	}
	writeSCIMJSON(w, status, errPayload)
}

func nowRFC3339UTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func formatRFC3339UTC(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func sanitizePagination(startIndex, count int) (int, int) {
	if startIndex <= 0 {
		startIndex = 1
	}
	if count <= 0 {
		count = 100
	}
	if count > 200 {
		count = 200
	}
	return startIndex, count
}

func parsePaginationParams(r *http.Request) (startIndex int, count int, err error) {
	startIndex = 1
	count = 100

	if raw := strings.TrimSpace(r.URL.Query().Get("startIndex")); raw != "" {
		v, parseErr := strconv.Atoi(raw)
		if parseErr != nil {
			return 0, 0, fmt.Errorf("invalid startIndex")
		}
		startIndex = v
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("count")); raw != "" {
		v, parseErr := strconv.Atoi(raw)
		if parseErr != nil {
			return 0, 0, fmt.Errorf("invalid count")
		}
		count = v
	}
	startIndex, count = sanitizePagination(startIndex, count)
	return startIndex, count, nil
}

func parseAttributes(r *http.Request) (map[string]struct{}, map[string]struct{}) {
	toSet := func(raw string) map[string]struct{} {
		set := make(map[string]struct{})
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			set[strings.ToLower(part)] = struct{}{}
		}
		return set
	}
	include := toSet(strings.TrimSpace(r.URL.Query().Get("attributes")))
	exclude := toSet(strings.TrimSpace(r.URL.Query().Get("excludedAttributes")))
	return include, exclude
}

func shouldIncludeTopLevel(attr string, include, exclude map[string]struct{}) bool {
	lower := strings.ToLower(attr)
	if len(include) > 0 {
		if _, ok := include[lower]; !ok {
			prefix := lower + "."
			found := false
			for inc := range include {
				if strings.HasPrefix(inc, prefix) || strings.HasPrefix(inc, lower+":") {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	if _, ok := exclude[lower]; ok {
		return false
	}
	return true
}

func projectSCIMUser(user SCIMUser, include, exclude map[string]struct{}) map[string]interface{} {
	projected := map[string]interface{}{
		"schemas": user.Schemas,
		"id":      user.ID,
		"meta":    user.Meta,
	}
	if shouldIncludeTopLevel("externalId", include, exclude) && user.ExternalID != "" {
		projected["externalId"] = user.ExternalID
	}
	if shouldIncludeTopLevel("userName", include, exclude) && user.UserName != "" {
		projected["userName"] = user.UserName
	}
	if shouldIncludeTopLevel("displayName", include, exclude) && user.DisplayName != "" {
		projected["displayName"] = user.DisplayName
	}
	if shouldIncludeTopLevel("emails", include, exclude) && len(user.Emails) > 0 {
		projected["emails"] = user.Emails
	}
	if shouldIncludeTopLevel("active", include, exclude) && user.Active != nil {
		projected["active"] = *user.Active
	}
	if shouldIncludeTopLevel("locale", include, exclude) && user.Locale != "" {
		projected["locale"] = user.Locale
	}
	if shouldIncludeTopLevel("timezone", include, exclude) && user.Timezone != "" {
		projected["timezone"] = user.Timezone
	}
	if shouldIncludeTopLevel(strings.ToLower(SchemaWorkflowUserExtension), include, exclude) && user.WorkflowUserExtension != nil {
		projected[SchemaWorkflowUserExtension] = user.WorkflowUserExtension
	}
	return projected
}

func projectSCIMGroup(group SCIMGroup, include, exclude map[string]struct{}) map[string]interface{} {
	projected := map[string]interface{}{
		"schemas": group.Schemas,
		"id":      group.ID,
		"meta":    group.Meta,
	}
	if shouldIncludeTopLevel("externalId", include, exclude) && group.ExternalID != "" {
		projected["externalId"] = group.ExternalID
	}
	if shouldIncludeTopLevel("displayName", include, exclude) && group.DisplayName != "" {
		projected["displayName"] = group.DisplayName
	}
	if shouldIncludeTopLevel("members", include, exclude) {
		projected["members"] = group.Members
	}
	return projected
}

func parseIDFromPath(r *http.Request, key string) string {
	if r == nil {
		return ""
	}
	id := strings.TrimSpace(r.PathValue(key))
	if id != "" {
		return id
	}
	parts := strings.Split(strings.Trim(strings.TrimSpace(r.URL.Path), "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func boolPtr(v bool) *bool {
	return &v
}

func normalizeSortOrder(raw string) string {
	order := strings.ToLower(strings.TrimSpace(raw))
	if order != "descending" {
		return "ascending"
	}
	return order
}

func anonymizeIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	raw := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if raw == "" {
		raw = strings.TrimSpace(r.RemoteAddr)
	}
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, ",") {
		raw = strings.TrimSpace(strings.Split(raw, ",")[0])
	}
	host, _, err := net.SplitHostPort(raw)
	if err == nil {
		raw = host
	}
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		v4[3] = 0
		return v4.String()
	}
	v6 := ip.To16()
	if v6 == nil {
		return ""
	}
	v6[14] = 0
	v6[15] = 0
	return net.IP(v6).String()
}

func scopeContains(scopes []string, required string) bool {
	required = strings.TrimSpace(required)
	if required == "" {
		return true
	}
	for i := range scopes {
		if strings.EqualFold(strings.TrimSpace(scopes[i]), required) {
			return true
		}
	}
	return false
}

func parseStatusReasonFromRateLimitError(err error) int {
	if err == nil {
		return 60
	}
	msg := err.Error()
	idx := strings.Index(msg, "retry_after=")
	if idx < 0 {
		return 60
	}
	raw := msg[idx+len("retry_after="):]
	end := strings.IndexAny(raw, " ,)")
	if end >= 0 {
		raw = raw[:end]
	}
	v, parseErr := strconv.Atoi(strings.TrimSpace(raw))
	if parseErr != nil || v <= 0 {
		return 60
	}
	return v
}

func extractPrimaryEmail(emails []SCIMEmail) string {
	if len(emails) == 0 {
		return ""
	}
	for i := range emails {
		if emails[i].Primary {
			return strings.TrimSpace(emails[i].Value)
		}
	}
	return strings.TrimSpace(emails[0].Value)
}

func anyToString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case fmt.Stringer:
		return strings.TrimSpace(x.String())
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
}
