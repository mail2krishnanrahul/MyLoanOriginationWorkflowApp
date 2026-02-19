package notification

import (
	"context"
	"testing"

	"workflow-engine/pkg/model"

	"github.com/stretchr/testify/assert"
)

func TestValidateNotificationTransition(t *testing.T) {
	tests := []struct {
		name      string
		current   model.NotificationStatus
		requested model.NotificationStatus
		wantErr   bool
	}{
		{
			name:      "happy path pending to sent",
			current:   model.NotificationStatusPending,
			requested: model.NotificationStatusSent,
		},
		{
			name:      "edge failed to pending manual retry",
			current:   model.NotificationStatusFailed,
			requested: model.NotificationStatusPending,
		},
		{
			name:      "failure mode sent terminal",
			current:   model.NotificationStatusSent,
			requested: model.NotificationStatusFailed,
			wantErr:   true,
		},
		{
			name:      "failure mode invalid pending backward",
			current:   model.NotificationStatusPending,
			requested: model.NotificationStatusPending,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNotificationTransition(context.Background(), tt.current, tt.requested)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
