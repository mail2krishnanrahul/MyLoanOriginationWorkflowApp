package assignment

import (
	"context"
	"fmt"

	"workflow-engine/pkg/model"
)

type AssignmentState string

const (
	StateUnassigned       AssignmentState = "UNASSIGNED"
	StateWorkbasketQueued AssignmentState = "WORKBASKET_QUEUED"
	StateAssigned         AssignmentState = "ASSIGNED"
	StateDelegated        AssignmentState = "DELEGATED" // Logic state, maps to ASSIGNED in DB but tracked
	StateReassigned       AssignmentState = "REASSIGNED"
)

func ValidateAssignmentTransition(
	ctx context.Context,
	current AssignmentState,
	requested AssignmentState,
	initiatedBy string,
	role string, // SYSTEM, SUPERVISOR, WORKER
) error {
	// Map allowed transitions
	// From -> [To] -> Roles allowed
	allowed := map[AssignmentState]map[AssignmentState][]string{
		StateUnassigned: {
			StateWorkbasketQueued: {"SYSTEM", "SUPERVISOR"},
			StateAssigned:         {"SYSTEM", "SUPERVISOR"},
		},
		StateWorkbasketQueued: {
			StateAssigned: {"WORKER", "SYSTEM", "SUPERVISOR"}, // Worker claim, Auto-Assign
		},
		StateAssigned: {
			StateDelegated:        {"WORKER", "SUPERVISOR", "SYSTEM"},
			StateReassigned:       {"SUPERVISOR"},
			StateWorkbasketQueued: {"SUPERVISOR"}, // Return to basket
			StateUnassigned:       {"SUPERVISOR"}, // Force unassign
		},
		StateDelegated: {
			StateAssigned: {"WORKER"}, // Accept delegation
		},
	}

	// Check From
	targets, ok := allowed[current]
	if !ok {
		return fmt.Errorf("%w: invalid source state %s",
			model.ErrInvalidStateTransition, current)
	}

	// Check To
	roles, ok := targets[requested]
	if !ok {
		return fmt.Errorf("%w: from %s to %s",
			model.ErrInvalidStateTransition, current, requested)
	}

	// Check Role
	roleAllowed := false
	for _, r := range roles {
		if r == role {
			roleAllowed = true
			break
		}
	}

	if !roleAllowed {
		return fmt.Errorf("%w: role %s not allowed for %s -> %s",
			model.ErrInvalidStateTransition, role, current, requested)
	}

	return nil
}
