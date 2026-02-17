package model

import "errors"

// Sentinel errors for domain failures. Use errors.Is() to check.
var (
	ErrNoEligibleWorker       = errors.New("no eligible worker found")
	ErrWorkerAtCapacity       = errors.New("worker at maximum capacity")
	ErrInvalidStateTransition = errors.New("invalid state transition")
	ErrCaseAlreadySuspended   = errors.New("case is already suspended")
	ErrDelegationChainBroken  = errors.New("delegation chain integrity violated")
	ErrFourEyesTokenRequired  = errors.New("four-eyes approval token required")
	ErrFourEyesTokenInvalid   = errors.New("four-eyes approval token invalid or expired")
	ErrFourEyesSameSupervisor = errors.New("four-eyes requires a different supervisor")
	ErrTaskNotFound           = errors.New("task not found")
	ErrCaseNotFound           = errors.New("case not found")
	ErrCaseTerminal           = errors.New("case is in a terminal state")
)
