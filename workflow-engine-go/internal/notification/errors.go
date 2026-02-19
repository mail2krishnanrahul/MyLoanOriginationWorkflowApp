package notification

import (
	"errors"
	"fmt"
)

var (
	ErrCircuitOpen                    = errors.New("circuit breaker open")
	ErrInvalidNotificationTransition  = errors.New("invalid notification transition")
	ErrMissingTemplateVariable        = errors.New("missing template variable")
)

// ChannelError classifies channel failures as transient (retryable) or permanent.
type ChannelError struct {
	Message   string
	Code      string
	Transient bool
	Cause     error
}

func (e *ChannelError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != "" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Code)
	}
	return e.Message
}

func (e *ChannelError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func newTransientChannelError(message, code string, cause error) error {
	return &ChannelError{
		Message:   message,
		Code:      code,
		Transient: true,
		Cause:     cause,
	}
}

func newPermanentChannelError(message, code string, cause error) error {
	return &ChannelError{
		Message:   message,
		Code:      code,
		Transient: false,
		Cause:     cause,
	}
}

func isTransientChannelError(err error) bool {
	if err == nil {
		return false
	}
	var chErr *ChannelError
	if errors.As(err, &chErr) {
		return chErr.Transient
	}
	return false
}
