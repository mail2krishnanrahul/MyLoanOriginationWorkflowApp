package sla

import (
	"context"
	"testing"
	"time"

	"workflow-engine/pkg/model"

	"github.com/stretchr/testify/assert"
)

func TestResolveEffectiveSLADefinition(t *testing.T) {
	tests := []struct {
		name      string
		config    model.CaseTypeConfig
		stageCode string
		activity  string
		task      string
		wantHours float64
		wantErr   bool
	}{
		{
			name: "happy path task override wins",
			config: model.CaseTypeConfig{
				DefaultCalendarID: "default",
				SLA: &model.SLAHierarchyConfig{
					Case: &model.SLADefinition{DurationHours: 24, WarningThresholdPct: 80, CriticalThresholdPct: 95, BreachAction: model.SLABreachActionNotifyOnly},
				},
				Stages: []model.StageDefinitionV2{{
					Code: "UNDERWRITING",
					Activities: []model.ActivityConfig{{
						Code: "DOC_CHECK",
						TaskDefs: []model.TaskDefinitionV2{{Code: "VERIFY_INCOME", SLA: &model.SLADefinition{DurationHours: 2}}},
					}},
				}},
			},
			stageCode: "UNDERWRITING",
			activity:  "DOC_CHECK",
			task:      "VERIFY_INCOME",
			wantHours: 2,
		},
		{
			name: "edge case inherits case level",
			config: model.CaseTypeConfig{
				SLA: &model.SLAHierarchyConfig{Case: &model.SLADefinition{DurationHours: 48}},
			},
			stageCode: "ANY",
			activity:  "ANY",
			task:      "ANY",
			wantHours: 48,
		},
		{
			name: "failure missing duration",
			config: model.CaseTypeConfig{
				SLA: &model.SLAHierarchyConfig{Case: &model.SLADefinition{WarningThresholdPct: 80}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveEffectiveSLADefinition(tt.config, tt.stageCode, tt.activity, tt.task)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			if got != nil {
				assert.Equal(t, tt.wantHours, got.DurationHours)
			}
		})
	}
}

func TestValidateSLAOperation(t *testing.T) {
	tests := []struct {
		name      string
		op        model.SLAOperation
		entity    SLAEntity
		actor     Actor
		wantErr   bool
	}{
		{
			name: "happy path active to pause",
			op:   model.SLAOperationPause,
			entity: SLAEntity{EntityType: model.SLAEntityTypeTask, EntityID: "task-1", State: model.SLAStateActive},
			actor: Actor{ID: "system", IsSystem: true},
		},
		{
			name: "edge case breached cannot extend",
			op:   model.SLAOperationExtend,
			entity: SLAEntity{EntityType: model.SLAEntityTypeTask, EntityID: "task-2", State: model.SLAStateBreached, IsBreached: true, ExtensionDuration: time.Hour},
			actor: Actor{ID: "sup-1", IsSupervisor: true},
			wantErr: true,
		},
		{
			name: "failure reset by non supervisor",
			op:   model.SLAOperationReset,
			entity: SLAEntity{EntityType: model.SLAEntityTypeCase, EntityID: "case-1", State: model.SLAStateActive},
			actor: Actor{ID: "user-1"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSLAOperation(context.Background(), tt.op, tt.entity, tt.actor)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
