package reporting

import (
	"context"
	"log/slog"

	"workflow-engine/pkg/model"
)

// EventHintObserver listens to workflow events and triggers an async snapshot refresh hint.
type EventHintObserver struct {
	job    *MetricsRefreshJob
	logger *slog.Logger
}

// NewEventHintObserver creates an observer that nudges MetricsRefreshJob on high-value events.
func NewEventHintObserver(job *MetricsRefreshJob, logger *slog.Logger) *EventHintObserver {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventHintObserver{job: job, logger: logger}
}

// HandleEvent queues a refresh signal for reporting-critical events.
func (o *EventHintObserver) HandleEvent(ctx context.Context, event model.Event) error {
	_ = ctx
	if o == nil {
		return nil
	}
	if o.job == nil {
		o.logger.Warn("metrics refresh trigger skipped because job is nil")
		return nil
	}

	switch event.EventType {
	case model.EventTaskCompleted,
		model.EventTaskFailed,
		model.EventTaskRequeued,
		model.EventCaseCreated,
		model.EventCaseCompleted,
		model.EventCaseStageChanged:
		o.job.TriggerRefresh()
		o.logger.Debug("queued metrics refresh trigger", "event_type", event.EventType, "event_id", event.ID)
	}

	return nil
}
