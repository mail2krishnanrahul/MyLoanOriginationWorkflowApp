package integration

import (
	"context"
	"fmt"
	"strings"

	"workflow-engine/internal/multitenancy"
)

func normalizePagination(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	return page, size
}

func tenantFromCtxOrArg(ctx context.Context, tenantID string) (string, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID != "" {
		return tenantID, nil
	}
	fromCtx, err := multitenancy.TenantFromContext(ctx)
	if err != nil {
		return "", fmt.Errorf("tenantFromCtxOrArg: %w", err)
	}
	fromCtx = strings.TrimSpace(fromCtx)
	if fromCtx == "" {
		return "", fmt.Errorf("tenantFromCtxOrArg: empty tenant in context")
	}
	return fromCtx, nil
}
