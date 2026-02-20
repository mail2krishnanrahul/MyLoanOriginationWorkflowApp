package versioning

import (
	"context"
	"strings"
)

type actorContextKey string

const (
	actorKey actorContextKey = "versioning_actor"

	defaultPageSize = 50
	maxPageSize     = 200
)

// WithActor stores actor identity in context for governance/audit logging.
func WithActor(ctx context.Context, actor string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ctx
	}
	return context.WithValue(ctx, actorKey, actor)
}

func actorFromContext(ctx context.Context) string {
	if ctx == nil {
		return "unknown"
	}

	if v := ctx.Value(actorKey); v != nil {
		if actor, ok := v.(string); ok {
			actor = strings.TrimSpace(actor)
			if actor != "" {
				return actor
			}
		}
	}

	for _, key := range []string{"actor", "actor_id", "user_id", "requested_by", "service_name"} {
		if v := ctx.Value(key); v != nil {
			if actor, ok := v.(string); ok {
				actor = strings.TrimSpace(actor)
				if actor != "" {
					return actor
				}
			}
		}
	}

	return "unknown"
}

func normalizePagination(page int, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	return page, size
}
