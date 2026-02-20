package engine

import (
	"context"
	"fmt"

	"workflow-engine/pkg/model"
)

// MultiEventObserver fan-outs events to multiple observers in sequence.
type MultiEventObserver struct {
	observers []EventObserver
}

// NewMultiEventObserver creates an observer chain, skipping nil observers.
func NewMultiEventObserver(observers ...EventObserver) *MultiEventObserver {
	filtered := make([]EventObserver, 0, len(observers))
	for _, observer := range observers {
		if observer != nil {
			filtered = append(filtered, observer)
		}
	}
	return &MultiEventObserver{observers: filtered}
}

// HandleEvent forwards the event to each observer and returns the first error.
func (m *MultiEventObserver) HandleEvent(ctx context.Context, event model.Event) error {
	if m == nil {
		return nil
	}
	for _, observer := range m.observers {
		if err := observer.HandleEvent(ctx, event); err != nil {
			return fmt.Errorf("MultiEventObserver.HandleEvent: %w", err)
		}
	}
	return nil
}
