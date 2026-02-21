package scim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

func resolveBulkPath(path string, bulkIDs map[string]string) string {
	path = strings.TrimSpace(path)
	for key, value := range bulkIDs {
		path = strings.ReplaceAll(path, key, value)
	}
	return path
}

func resolveBulkData(data json.RawMessage, bulkIDs map[string]string) json.RawMessage {
	if len(data) == 0 {
		return data
	}
	replaced := string(data)
	for key, value := range bulkIDs {
		replaced = strings.ReplaceAll(replaced, key, value)
	}
	return json.RawMessage(replaced)
}

func extractIDFromLocation(location string) string {
	location = strings.TrimSpace(location)
	if location == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(location, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func (h *SCIMHandler) dispatchBulkOperation(baseCtx *http.Request, method string, path string, data json.RawMessage, version string) (*httptest.ResponseRecorder, error) {
	reqBody := bytes.NewReader(data)
	if len(data) == 0 {
		reqBody = bytes.NewReader([]byte("{}"))
	}
	req, err := http.NewRequestWithContext(baseCtx.Context(), strings.ToUpper(method), path, io.NopCloser(reqBody))
	if err != nil {
		return nil, fmt.Errorf("dispatchBulkOperation: build request: %w", err)
	}
	req.Header.Set("Content-Type", scimContentType)
	if strings.TrimSpace(version) != "" {
		req.Header.Set("If-Match", strings.TrimSpace(version))
	}
	rec := httptest.NewRecorder()

	m := strings.ToUpper(strings.TrimSpace(method))
	switch {
	case m == http.MethodPost && path == "/Users":
		h.CreateUser(rec, req)
	case m == http.MethodPut && strings.HasPrefix(path, "/Users/"):
		h.ReplaceUser(rec, req)
	case m == http.MethodPatch && strings.HasPrefix(path, "/Users/"):
		h.PatchUser(rec, req)
	case m == http.MethodDelete && strings.HasPrefix(path, "/Users/"):
		h.DeleteUser(rec, req)
	case m == http.MethodGet && strings.HasPrefix(path, "/Users/"):
		h.GetUser(rec, req)
	case m == http.MethodGet && path == "/Users":
		h.ListUsers(rec, req)
	case m == http.MethodPost && path == "/Groups":
		h.CreateGroup(rec, req)
	case m == http.MethodPut && strings.HasPrefix(path, "/Groups/"):
		h.ReplaceGroup(rec, req)
	case m == http.MethodPatch && strings.HasPrefix(path, "/Groups/"):
		h.PatchGroup(rec, req)
	case m == http.MethodDelete && strings.HasPrefix(path, "/Groups/"):
		h.DeleteGroup(rec, req)
	case m == http.MethodGet && strings.HasPrefix(path, "/Groups/"):
		h.GetGroup(rec, req)
	case m == http.MethodGet && path == "/Groups":
		h.ListGroups(rec, req)
	default:
		writeSCIMError(rec, http.StatusBadRequest, "invalidPath", "unsupported bulk path")
	}
	return rec, nil
}

// BulkOperation handles POST /scim/v2/Bulk.
func (h *SCIMHandler) BulkOperation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", scimContentType)
	claims, _, err := tenantFromClaims(r.Context())
	if err != nil {
		writeSCIMError(w, http.StatusUnauthorized, "", "missing or invalid bearer token")
		return
	}

	var req SCIMBulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "invalidSyntax", "invalid request body")
		return
	}
	if len(req.Operations) > 1000 {
		writeSCIMError(w, http.StatusBadRequest, "tooMany", "bulk operations exceed limit")
		return
	}
	if err := EnforceSCIMRateLimit(r.Context(), h.db, claims.TokenID, len(req.Operations)); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "retry_after=") {
			retryAfter := parseStatusReasonFromRateLimitError(err)
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			writeSCIMError(w, http.StatusTooManyRequests, "", "rate limit exceeded")
			return
		}
		writeSCIMError(w, http.StatusInternalServerError, "", "rate limit evaluation failed")
		return
	}

	responses := make([]SCIMBulkOperationResponse, 0, len(req.Operations))
	bulkIDs := make(map[string]string)
	errorCount := 0
	failOnErrors := req.FailOnErrors
	if failOnErrors < 0 {
		failOnErrors = 0
	}

	for i := range req.Operations {
		op := req.Operations[i]
		resolvedPath := resolveBulkPath(strings.TrimSpace(op.Path), bulkIDs)
		resolvedData := resolveBulkData(op.Data, bulkIDs)
		rec, dispatchErr := h.dispatchBulkOperation(r, op.Method, resolvedPath, resolvedData, op.Version)
		if dispatchErr != nil {
			responses = append(responses, SCIMBulkOperationResponse{
				Method: strings.ToUpper(strings.TrimSpace(op.Method)),
				BulkID: strings.TrimSpace(op.BulkID),
				Status: "500",
				Response: SCIMError{
					Schemas: []string{SchemaError},
					Status:  "500",
					Detail:  "bulk operation dispatch failed",
				},
			})
			errorCount++
			if failOnErrors > 0 && errorCount >= failOnErrors {
				break
			}
			continue
		}

		result := rec.Result()
		bodyBytes, _ := io.ReadAll(result.Body)
		_ = result.Body.Close()

		operationResponse := SCIMBulkOperationResponse{
			Method:   strings.ToUpper(strings.TrimSpace(op.Method)),
			BulkID:   strings.TrimSpace(op.BulkID),
			Location: strings.TrimSpace(result.Header.Get("Location")),
			Version:  strings.TrimSpace(result.Header.Get("ETag")),
			Status:   fmt.Sprintf("%d", result.StatusCode),
		}
		if len(bodyBytes) > 0 {
			var decoded interface{}
			if err := json.Unmarshal(bodyBytes, &decoded); err == nil {
				operationResponse.Response = decoded
			}
		}
		responses = append(responses, operationResponse)

		if result.StatusCode >= 400 {
			errorCount++
			if failOnErrors > 0 && errorCount >= failOnErrors {
				break
			}
			continue
		}

		if op.BulkID != "" {
			id := extractIDFromLocation(operationResponse.Location)
			if id != "" {
				bulkIDs["bulkId:"+op.BulkID] = id
			}
		}
	}

	resp := SCIMBulkResponse{
		Schemas:    []string{SchemaBulkResponse},
		Operations: responses,
	}
	writeSCIMJSON(w, http.StatusOK, resp)
}
