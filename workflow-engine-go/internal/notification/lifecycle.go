package notification

import (
	"context"
	"fmt"

	"workflow-engine/pkg/model"
)

// ValidateNotificationTransition enforces notification lifecycle rules.
func ValidateNotificationTransition(
	ctx context.Context,
	current model.NotificationStatus,
	requested model.NotificationStatus,
) error {
	_ = ctx

	if current == "" || requested == "" {
		return fmt.Errorf("ValidateNotificationTransition: %w: status is required", ErrInvalidNotificationTransition)
	}
	if current == requested {
		return fmt.Errorf("ValidateNotificationTransition: %w: no-op transition %s", ErrInvalidNotificationTransition, current)
	}

	switch current {
	case model.NotificationStatusPending:
		switch requested {
		case model.NotificationStatusSent,
			model.NotificationStatusFailed,
			model.NotificationStatusSuppressed,
			model.NotificationStatusCancelled:
			return nil
		default:
			return fmt.Errorf("ValidateNotificationTransition: %w: %s -> %s", ErrInvalidNotificationTransition, current, requested)
		}

	case model.NotificationStatusFailed:
		if requested == model.NotificationStatusPending {
			return nil
		}
		return fmt.Errorf("ValidateNotificationTransition: %w: %s -> %s", ErrInvalidNotificationTransition, current, requested)

	case model.NotificationStatusSent,
		model.NotificationStatusSuppressed,
		model.NotificationStatusCancelled:
		return fmt.Errorf("ValidateNotificationTransition: %w: terminal state %s", ErrInvalidNotificationTransition, current)

	default:
		return fmt.Errorf("ValidateNotificationTransition: %w: unknown current state %s", ErrInvalidNotificationTransition, current)
	}
}
