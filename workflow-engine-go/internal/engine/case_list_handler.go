package engine

import (
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"workflow-engine/internal/multitenancy"
	"workflow-engine/internal/repository"
)

// uuidRE matches a canonical UUID (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
var uuidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// isValidUUID returns true only when s is a well-formed UUID string.
// This guards against sentinel values like "me" reaching SQL ::uuid casts.
func isValidUUID(s string) bool {
	return uuidRE.MatchString(s)
}

// RegisterCaseListHandler attaches GET /cases to the mux.
// This handler fixes the frontend 400 error caused by the missing list endpoint.
func RegisterCaseListHandler(mux *http.ServeMux, repo *repository.Repository) {
	mux.HandleFunc("GET /cases/summary-stats", handleGetCaseSummaryStats(repo))
	mux.HandleFunc("GET /cases", handleListCases(repo))
}

// handleListCases returns an http.HandlerFunc for GET /cases.
// Supports query parameters:
//
//	scope        = my | team | all
//	status       = comma-separated CaseStatus values
//	stage        = comma-separated stage codes
//	caseType     = single case type code
//	dateFrom     = RFC3339 date
//	dateTo       = RFC3339 date
//	query        = text search substring
//	slaStatus    = comma-separated ON_TRACK|WARNING|BREACHED
//	priority     = comma-separated priority codes
//	assignedTo   = user UUID (non-UUID values like "me" are ignored)
//	tags         = comma-separated tag codes (AND logic)
//	page         = int (default 1)
//	limit        = int (default 25, max 100)
func handleListCases(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		// Resolve tenant from context (set by TenantMiddleware) or fall back.
		tenantID, _ := multitenancy.TenantFromContext(r.Context())
		if tenantID == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "tenant context required",
			})
			return
		}

		// Resolve the acting user (set by TenantMiddleware or auth middleware).
		userID := multitenancy.UserIDFromContext(r.Context())

		scope := q.Get("scope")
		if scope == "" {
			scope = "my"
		}
		if scope != "my" && scope != "team" && scope != "all" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "scope must be one of: my, team, all",
			})
			return
		}

		page := parseInt(q.Get("page"), 1)
		limit := parseInt(q.Get("limit"), 25)

		// Sanitise assignedTo: only forward it when it is a valid UUID.
		// Sentinel values like "me" from older frontend code are silently dropped;
		// the scope filter already handles the "my" case via the acting userID.
		assignedTo := q.Get("assignedTo")
		if !isValidUUID(assignedTo) {
			assignedTo = ""
		}

		filters := repository.CaseListFilters{
			TenantID:   tenantID,
			Scope:      scope,
			UserID:     userID,
			Status:     splitCSV(q.Get("status")),
			Stage:      splitCSV(q.Get("stage")),
			CaseType:   q.Get("caseType"),
			DateFrom:   q.Get("dateFrom"),
			DateTo:     q.Get("dateTo"),
			Query:      q.Get("query"),
			SLAStatus:  splitCSV(q.Get("slaStatus")),
			Priority:   splitCSV(q.Get("priority")),
			AssignedTo: assignedTo,
			Tags:       splitCSV(q.Get("tags")),
			Page:       page,
			Limit:      limit,
		}

		result, err := repo.ListCases(r.Context(), filters)
		if err != nil {
			slog.Error("GET /cases failed", "error", err, "tenant_id", tenantID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to list cases",
			})
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

// splitCSV splits a comma-separated query param value into a non-empty slice.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// parseInt parses a string to int, returning defaultVal on failure or empty input.
func parseInt(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return defaultVal
	}
	return n
}

// handleGetCaseSummaryStats returns an http.HandlerFunc for GET /cases/summary-stats.
func handleGetCaseSummaryStats(repo *repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, _ := multitenancy.TenantFromContext(r.Context())
		if tenantID == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "tenant context required",
			})
			return
		}

		userID := multitenancy.UserIDFromContext(r.Context())

		stats, err := repo.GetCaseSummaryStats(r.Context(), tenantID, userID)
		if err != nil {
			slog.Error("GET /cases/summary-stats failed", "error", err, "tenant_id", tenantID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to get case summary stats",
			})
			return
		}

		writeJSON(w, http.StatusOK, stats)
	}
}
