package exception

import (
	"context"
	"errors"
	"testing"
	"time"

	"workflow-engine/pkg/model"

	"github.com/stretchr/testify/assert"
)

type codedErr struct {
	code string
	msg  string
}

func (e codedErr) Error() string {
	return e.msg
}

func (e codedErr) ErrorCode() string {
	return e.code
}

type upstreamErr struct {
	codedErr
	upstream map[string]interface{}
}

func (e upstreamErr) UpstreamError() interface{} {
	return e.upstream
}

func TestTaskLevelErrorClassification_HappyPath(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected model.ErrorClass
	}{
		{name: "transient timeout", code: "DOWNSTREAM_TIMEOUT", expected: model.ErrorClassTransient},
		{name: "permanent validation", code: "VALIDATION_FAILED", expected: model.ErrorClassPermanent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyErrorCode(tt.code)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestTaskLevelErrorClassification_EdgeCase(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected model.ErrorClass
	}{
		{name: "unknown code is unknown class", code: "SOMETHING_NEW", expected: model.ErrorClassUnknown},
		{name: "case and separator normalization", code: "network-timeout", expected: model.ErrorClassTransient},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyErrorCode(tt.code)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestTaskLevelErrorClassification_FailureMode(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{name: "nil error falls back to unknown code", err: nil, expected: "UNKNOWN_ERROR"},
		{name: "typed coded error wins", err: codedErr{code: "MALFORMED_PAYLOAD", msg: "bad input"}, expected: "MALFORMED_PAYLOAD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractErrorCode(tt.err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestRetryPolicyEngine_HappyPath(t *testing.T) {
	tests := []struct {
		name          string
		policy        model.TaskRetryPolicy
		attemptNumber int
		want          time.Duration
	}{
		{
			name: "exponential backoff",
			policy: model.TaskRetryPolicy{
				BackoffStrategy:     model.RetryBackoffStrategyExponential,
				BaseIntervalSeconds: 5,
				MaxIntervalSeconds:  300,
			},
			attemptNumber: 3,
			want:          20 * time.Second,
		},
		{
			name: "linear backoff",
			policy: model.TaskRetryPolicy{
				BackoffStrategy:     model.RetryBackoffStrategyLinear,
				BaseIntervalSeconds: 4,
				MaxIntervalSeconds:  60,
			},
			attemptNumber: 2,
			want:          8 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ComputeBackoffDuration(tt.policy, tt.attemptNumber)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRetryPolicyEngine_EdgeCase(t *testing.T) {
	tests := []struct {
		name          string
		policy        model.TaskRetryPolicy
		attemptNumber int
		want          time.Duration
	}{
		{
			name: "max interval caps exponential growth",
			policy: model.TaskRetryPolicy{
				BackoffStrategy:     model.RetryBackoffStrategyExponential,
				BaseIntervalSeconds: 30,
				MaxIntervalSeconds:  45,
			},
			attemptNumber: 4,
			want:          45 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ComputeBackoffDuration(tt.policy, tt.attemptNumber)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRetryPolicyEngine_FailureMode(t *testing.T) {
	tests := []struct {
		name          string
		policy        model.TaskRetryPolicy
		attemptNumber int
	}{
		{
			name: "invalid attempt number",
			policy: model.TaskRetryPolicy{
				BackoffStrategy:     model.RetryBackoffStrategyFixed,
				BaseIntervalSeconds: 5,
				MaxIntervalSeconds:  5,
			},
			attemptNumber: 0,
		},
		{
			name: "invalid base interval",
			policy: model.TaskRetryPolicy{
				BackoffStrategy:     model.RetryBackoffStrategyFixed,
				BaseIntervalSeconds: 0,
				MaxIntervalSeconds:  10,
			},
			attemptNumber: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ComputeBackoffDuration(tt.policy, tt.attemptNumber)
			assert.Error(t, err)
		})
	}
}

func TestErrorDetailCapture_HappyPath(t *testing.T) {
	tests := []struct {
		name string
		in   TaskFailureInput
	}{
		{
			name: "captures code class message source and upstream",
			in: TaskFailureInput{
				TaskID:        "task-1",
				SourceService: "credit-service",
				Err: upstreamErr{
					codedErr: codedErr{code: "DOWNSTREAM_503", msg: "service unavailable"},
					upstream: map[string]interface{}{"status": 503},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now().UTC()
			detail, raw, err := buildTaskErrorDetail(tt.in, tt.in.SourceService, ExtractErrorCode(tt.in.Err), ClassifyErrorCode(ExtractErrorCode(tt.in.Err)), now)
			assert.NoError(t, err)
			assert.NotEmpty(t, raw)
			assert.Equal(t, "DOWNSTREAM_503", detail.ErrorCode)
			assert.Equal(t, model.ErrorClassTransient, detail.ErrorClass)
			assert.Equal(t, "credit-service", detail.SourceService)
		})
	}
}

func TestErrorDetailCapture_EdgeCase(t *testing.T) {
	stack := "panic: test\nstack line"
	in := TaskFailureInput{
		TaskID:         "task-2",
		SourceService:  "worker",
		Err:            errors.New("boom"),
		RecoveredStack: &stack,
	}
	detail, _, err := buildTaskErrorDetail(in, in.SourceService, "UNKNOWN_ERROR", model.ErrorClassUnknown, time.Now().UTC())
	assert.NoError(t, err)
	if assert.NotNil(t, detail.StackContext) {
		assert.Contains(t, *detail.StackContext, "panic")
	}
}

func TestErrorDetailCapture_FailureMode(t *testing.T) {
	_, _, err := buildTaskErrorDetail(TaskFailureInput{Err: errors.New("x")}, "svc", "ERR", model.ErrorClassTransient, time.Now().UTC())
	assert.NoError(t, err)
}

func TestPoisonPillDetection_HappyPath(t *testing.T) {
	ctx := failureContextRow{RetryCount: 2, MaxRetries: 5, TotalFailureCount: 4}
	taskDef := model.TaskDefinitionV2{
		RetryPolicy: &model.TaskRetryPolicy{
			MaxRetries:          5,
			BackoffStrategy:     model.RetryBackoffStrategyFixed,
			BaseIntervalSeconds: 5,
			MaxIntervalSeconds:  30,
		},
		PoisonPillThreshold: 10,
	}
	decision, err := evaluateRetryDecision(ctx, taskDef, &model.CaseTypeConfig{}, "DOWNSTREAM_TIMEOUT", model.ErrorClassTransient)
	assert.NoError(t, err)
	assert.True(t, decision.ShouldRetry)
	assert.False(t, decision.PoisonPill)
}

func TestPoisonPillDetection_EdgeCase(t *testing.T) {
	ctx := failureContextRow{RetryCount: 2, MaxRetries: 5, TotalFailureCount: 9}
	taskDef := model.TaskDefinitionV2{
		RetryPolicy: &model.TaskRetryPolicy{
			MaxRetries:          5,
			BackoffStrategy:     model.RetryBackoffStrategyFixed,
			BaseIntervalSeconds: 5,
			MaxIntervalSeconds:  30,
		},
		PoisonPillThreshold: 10,
	}
	decision, err := evaluateRetryDecision(ctx, taskDef, &model.CaseTypeConfig{}, "DOWNSTREAM_TIMEOUT", model.ErrorClassTransient)
	assert.NoError(t, err)
	assert.True(t, decision.PoisonPill)
	assert.False(t, decision.ShouldRetry)
}

func TestPoisonPillDetection_FailureMode(t *testing.T) {
	ctx := failureContextRow{RetryCount: 1, MaxRetries: 0, TotalFailureCount: 1}
	taskDef := model.TaskDefinitionV2{
		RetryPolicy: &model.TaskRetryPolicy{
			MaxRetries:          0,
			BackoffStrategy:     model.RetryBackoffStrategyFixed,
			BaseIntervalSeconds: 5,
			MaxIntervalSeconds:  30,
		},
	}
	decision, err := evaluateRetryDecision(ctx, taskDef, &model.CaseTypeConfig{}, "VALIDATION_FAILED", model.ErrorClassPermanent)
	assert.NoError(t, err)
	assert.False(t, decision.ShouldRetry)
	assert.True(t, decision.RetriesExhausted)
}

func TestCaseLevelExceptionEscalation_HappyPath(t *testing.T) {
	taskDef := model.TaskDefinitionV2{FailureSeverity: model.TaskFailureSeverityBlocking}
	assert.True(t, ShouldEscalateCase(taskDef, model.ErrorClassPermanent, false))
}

func TestCaseLevelExceptionEscalation_EdgeCase(t *testing.T) {
	taskDef := model.TaskDefinitionV2{FailureSeverity: model.TaskFailureSeverityCritical}
	assert.True(t, ShouldEscalateCase(taskDef, model.ErrorClassTransient, true))
}

func TestCaseLevelExceptionEscalation_FailureMode(t *testing.T) {
	taskDef := model.TaskDefinitionV2{FailureSeverity: model.TaskFailureSeverityLow}
	assert.False(t, ShouldEscalateCase(taskDef, model.ErrorClassTransient, false))
}

func TestResolveRetryPolicy_HappyPath(t *testing.T) {
	taskDef := model.TaskDefinitionV2{
		RetryPolicy: &model.TaskRetryPolicy{
			MaxRetries:          4,
			BackoffStrategy:     model.RetryBackoffStrategyLinear,
			BaseIntervalSeconds: 10,
			MaxIntervalSeconds:  60,
			RetryableErrorCodes: []string{"downstream_timeout", "network-timeout"},
		},
	}
	policy := ResolveRetryPolicy(taskDef)
	assert.Equal(t, 4, policy.MaxRetries)
	assert.Equal(t, model.RetryBackoffStrategyLinear, policy.BackoffStrategy)
	assert.Equal(t, []string{"DOWNSTREAM_TIMEOUT", "NETWORK_TIMEOUT"}, policy.RetryableErrorCodes)
}

func TestResolveRetryPolicy_EdgeCase(t *testing.T) {
	taskDef := model.TaskDefinitionV2{}
	policy := ResolveRetryPolicy(taskDef)
	assert.Equal(t, 3, policy.MaxRetries)
	assert.Equal(t, model.RetryBackoffStrategyExponential, policy.BackoffStrategy)
}

func TestResolveRetryPolicy_FailureMode(t *testing.T) {
	policy := ResolveRetryPolicy(model.TaskDefinitionV2{RetryPolicy: &model.TaskRetryPolicy{MaxRetries: -3}})
	assert.Equal(t, 3, policy.MaxRetries)
}

func TestBuildTaskErrorDetail_ContextPropagation(t *testing.T) {
	detail, _, err := buildTaskErrorDetail(TaskFailureInput{
		TaskID:        "t1",
		SourceService: "svc",
		Err:           codedErr{code: "VALIDATION_FAILED", msg: "missing required"},
	}, "svc", "VALIDATION_FAILED", model.ErrorClassPermanent, time.Now().UTC())
	assert.NoError(t, err)
	assert.Equal(t, "svc", detail.SourceService)
}

func TestHandleTaskFailure_FailureModeValidation(t *testing.T) {
	err := HandleTaskFailure(context.Background(), nil, TaskFailureInput{})
	assert.Error(t, err)
}
