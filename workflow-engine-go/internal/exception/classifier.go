package exception

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"workflow-engine/pkg/model"
)

var transientErrorCodes = map[string]struct{}{
	"DOWNSTREAM_TIMEOUT":   {},
	"NETWORK_TIMEOUT":      {},
	"DB_DEADLOCK":          {},
	"DOWNSTREAM_503":       {},
	"SERVICE_UNAVAILABLE":  {},
	"CONNECTION_RESET":     {},
	"CONNECTION_REFUSED":   {},
	"TEMPORARY_UNAVAILABLE": {},
	"THROTTLED":            {},
}

var permanentErrorCodes = map[string]struct{}{
	"VALIDATION_FAILED":       {},
	"MALFORMED_PAYLOAD":       {},
	"MISSING_REQUIRED_FIELD":  {},
	"BUSINESS_RULE_VIOLATION": {},
	"UNAUTHORIZED":            {},
	"FORBIDDEN":               {},
	"NOT_FOUND":               {},
}

// ClassifyErrorCode returns a deterministic class for a normalized error code.
func ClassifyErrorCode(code string) model.ErrorClass {
	normalized := NormalizeErrorCode(code)
	if normalized == "" {
		return model.ErrorClassUnknown
	}
	if _, ok := transientErrorCodes[normalized]; ok {
		return model.ErrorClassTransient
	}
	if _, ok := permanentErrorCodes[normalized]; ok {
		return model.ErrorClassPermanent
	}
	return model.ErrorClassUnknown
}

// NormalizeErrorCode produces canonical uppercase underscore error codes.
func NormalizeErrorCode(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, "-", "_")
	trimmed = strings.ReplaceAll(trimmed, " ", "_")
	return strings.ToUpper(trimmed)
}

// ExtractErrorCode derives a deterministic code from typed or generic errors.
func ExtractErrorCode(err error) string {
	if err == nil {
		return "UNKNOWN_ERROR"
	}

	var coded ErrorCodeCarrier
	if errors.As(err, &coded) {
		if code := NormalizeErrorCode(coded.ErrorCode()); code != "" {
			return code
		}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return "DOWNSTREAM_TIMEOUT"
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "NETWORK_TIMEOUT"
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "deadlock"):
		return "DB_DEADLOCK"
	case strings.Contains(message, "503") || strings.Contains(message, "service unavailable"):
		return "DOWNSTREAM_503"
	case strings.Contains(message, "timeout"):
		return "DOWNSTREAM_TIMEOUT"
	case strings.Contains(message, "missing required"):
		return "MISSING_REQUIRED_FIELD"
	case strings.Contains(message, "validation"):
		return "VALIDATION_FAILED"
	case strings.Contains(message, "malformed"):
		return "MALFORMED_PAYLOAD"
	case strings.Contains(message, "business rule"):
		return "BUSINESS_RULE_VIOLATION"
	case strings.Contains(message, "unauthorized"):
		return "UNAUTHORIZED"
	case strings.Contains(message, "forbidden"):
		return "FORBIDDEN"
	case strings.Contains(message, "not found"):
		return "NOT_FOUND"
	default:
		return "UNKNOWN_ERROR"
	}
}

// ResolveRetryPolicy applies strict defaults for task retry configuration.
func ResolveRetryPolicy(taskDef model.TaskDefinitionV2) model.TaskRetryPolicy {
	policy := model.TaskRetryPolicy{
		MaxRetries:          3,
		BackoffStrategy:     model.RetryBackoffStrategyExponential,
		BaseIntervalSeconds: 5,
		MaxIntervalSeconds:  300,
	}
	if taskDef.RetryPolicy == nil {
		return policy
	}
	if taskDef.RetryPolicy.MaxRetries >= 0 {
		policy.MaxRetries = taskDef.RetryPolicy.MaxRetries
	}
	if taskDef.RetryPolicy.BaseIntervalSeconds > 0 {
		policy.BaseIntervalSeconds = taskDef.RetryPolicy.BaseIntervalSeconds
	}
	if taskDef.RetryPolicy.MaxIntervalSeconds > 0 {
		policy.MaxIntervalSeconds = taskDef.RetryPolicy.MaxIntervalSeconds
	}
	switch taskDef.RetryPolicy.BackoffStrategy {
	case model.RetryBackoffStrategyFixed, model.RetryBackoffStrategyLinear, model.RetryBackoffStrategyExponential:
		policy.BackoffStrategy = taskDef.RetryPolicy.BackoffStrategy
	}
	if len(taskDef.RetryPolicy.RetryableErrorCodes) > 0 {
		policy.RetryableErrorCodes = make([]string, 0, len(taskDef.RetryPolicy.RetryableErrorCodes))
		seen := map[string]struct{}{}
		for _, code := range taskDef.RetryPolicy.RetryableErrorCodes {
			normalized := NormalizeErrorCode(code)
			if normalized == "" {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			policy.RetryableErrorCodes = append(policy.RetryableErrorCodes, normalized)
		}
	}
	if policy.MaxIntervalSeconds < policy.BaseIntervalSeconds {
		policy.MaxIntervalSeconds = policy.BaseIntervalSeconds
	}
	if policy.MaxRetries < 0 {
		policy.MaxRetries = 0
	}
	return policy
}

// ResolvePoisonPillThreshold chooses per-task override, then case-level, then default.
func ResolvePoisonPillThreshold(taskDef model.TaskDefinitionV2, config *model.CaseTypeConfig) int {
	if taskDef.PoisonPillThreshold > 0 {
		return taskDef.PoisonPillThreshold
	}
	if config != nil && config.PoisonPillThreshold > 0 {
		return config.PoisonPillThreshold
	}
	return 12
}

// ResolveFailureSeverity chooses configured severity with BLOCKING default.
func ResolveFailureSeverity(taskDef model.TaskDefinitionV2) model.TaskFailureSeverity {
	switch taskDef.FailureSeverity {
	case model.TaskFailureSeverityLow,
		model.TaskFailureSeverityMedium,
		model.TaskFailureSeverityHigh,
		model.TaskFailureSeverityCritical,
		model.TaskFailureSeverityBlocking:
		return taskDef.FailureSeverity
	default:
		return model.TaskFailureSeverityBlocking
	}
}

// ShouldEscalateCase returns true if task failure should move the case to EXCEPTION.
func ShouldEscalateCase(taskDef model.TaskDefinitionV2, class model.ErrorClass, retriesExhausted bool) bool {
	severity := ResolveFailureSeverity(taskDef)
	if taskDef.EscalateCaseOnFailure {
		return true
	}
	if severity == model.TaskFailureSeverityCritical || severity == model.TaskFailureSeverityBlocking {
		if class == model.ErrorClassPermanent || retriesExhausted {
			return true
		}
	}
	return false
}

// ComputeBackoffDuration calculates next retry delay from policy and attempt number (1-based).
func ComputeBackoffDuration(policy model.TaskRetryPolicy, attemptNumber int) (time.Duration, error) {
	if attemptNumber < 1 {
		return 0, fmt.Errorf("ComputeBackoffDuration: attempt number must be >= 1")
	}
	if policy.BaseIntervalSeconds <= 0 {
		return 0, fmt.Errorf("ComputeBackoffDuration: base interval must be > 0")
	}
	if policy.MaxIntervalSeconds <= 0 {
		return 0, fmt.Errorf("ComputeBackoffDuration: max interval must be > 0")
	}

	base := time.Duration(policy.BaseIntervalSeconds) * time.Second
	maxInterval := time.Duration(policy.MaxIntervalSeconds) * time.Second

	var interval time.Duration
	switch policy.BackoffStrategy {
	case model.RetryBackoffStrategyFixed:
		interval = base
	case model.RetryBackoffStrategyLinear:
		interval = time.Duration(attemptNumber) * base
	case model.RetryBackoffStrategyExponential, "":
		interval = base
		for i := 1; i < attemptNumber; i++ {
			if interval >= maxInterval {
				interval = maxInterval
				break
			}
			if interval > maxInterval/2 {
				interval = maxInterval
				break
			}
			interval *= 2
		}
	default:
		return 0, fmt.Errorf("ComputeBackoffDuration: unsupported backoff strategy %s", policy.BackoffStrategy)
	}

	if interval > maxInterval {
		interval = maxInterval
	}
	if interval < 0 {
		return 0, fmt.Errorf("ComputeBackoffDuration: computed negative interval")
	}
	return interval, nil
}

func isErrorCodeRetryable(policy model.TaskRetryPolicy, errorCode string) bool {
	if len(policy.RetryableErrorCodes) == 0 {
		return true
	}
	normalized := NormalizeErrorCode(errorCode)
	for _, candidate := range policy.RetryableErrorCodes {
		if NormalizeErrorCode(candidate) == normalized {
			return true
		}
	}
	return false
}
