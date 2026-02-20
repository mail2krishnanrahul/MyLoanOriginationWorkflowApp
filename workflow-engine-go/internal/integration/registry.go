package integration

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"workflow-engine/pkg/model"
)

// TaskHandler is a pluggable Go-native task worker handler.
type TaskHandler interface {
	ServiceName() string
	Handle(ctx context.Context, task model.Task) (TaskResult, error)
}

// TaskResult is the canonical handler output envelope.
type TaskResult struct {
	Status        model.TaskStatus
	OutputPayload []byte
	ErrorDetail   []byte
}

// HandlerRegistry stores task handlers keyed by service name.
type HandlerRegistry struct {
	handlers map[string]TaskHandler
	mu       sync.RWMutex
	started  bool
}

// NewHandlerRegistry creates a new handler registry.
func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{handlers: make(map[string]TaskHandler)}
}

// Register adds a task handler if registration is still open.
func (r *HandlerRegistry) Register(h TaskHandler) error {
	if r == nil {
		return fmt.Errorf("Register: registry is nil")
	}
	if h == nil {
		return fmt.Errorf("Register: handler is nil")
	}
	serviceName := strings.TrimSpace(h.ServiceName())
	if serviceName == "" {
		return fmt.Errorf("Register: service name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return fmt.Errorf("Register: registry started: %w", ErrHandlerAlreadyRegistered)
	}
	if _, exists := r.handlers[serviceName]; exists {
		return fmt.Errorf("Register: service %s: %w", serviceName, ErrHandlerAlreadyRegistered)
	}
	r.handlers[serviceName] = h
	return nil
}

// Lookup returns a registered handler for the service.
func (r *HandlerRegistry) Lookup(serviceName string) (TaskHandler, bool) {
	if r == nil {
		return nil, false
	}
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return nil, false
	}
	r.mu.RLock()
	h, ok := r.handlers[serviceName]
	r.mu.RUnlock()
	return h, ok
}

// MustRegister panics if registration fails.
func (r *HandlerRegistry) MustRegister(h TaskHandler) {
	if err := r.Register(h); err != nil {
		panic(err)
	}
}

// MarkStarted closes registration and allows concurrent lookups only.
func (r *HandlerRegistry) MarkStarted() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.started = true
	r.mu.Unlock()
}
