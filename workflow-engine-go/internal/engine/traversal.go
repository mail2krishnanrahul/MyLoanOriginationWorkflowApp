package engine

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"
)

// StartRecursiveWorkflow initializes the workflow execution by finding the root components
func (e *Engine) StartRecursiveWorkflow(ctx context.Context, tx repository.DBExecutor, caseID string, versionID string) error {
	slog.Info("starting recursive workflow", "case_id", caseID, "version_id", versionID)

	roots, err := e.Repo.GetRootComponents(ctx, tx, versionID)
	if err != nil {
		return fmt.Errorf("failed to fetch root components: %w", err)
	}

	if len(roots) == 0 {
		return fmt.Errorf("no root components found for version %s", versionID)
	}

	// For root level, if multiple, we assume SEQUENTIAL by default or we need a higher level config.
	// But usually there is one root (e.g. "Main Stage").
	// If multiple roots, let's execute them sequentially for now or based on their order.

	// Start the first root component
	return e.ActivateComponent(ctx, tx, caseID, roots[0])
}

// ActivateComponent starts the execution of a component
func (e *Engine) ActivateComponent(ctx context.Context, tx repository.DBExecutor, caseID string, component *model.WorkflowComponent) error {
	slog.Info("activating component", "name", component.Name, "type", component.Type, "strategy", component.ExecutionStrategy)

	// 1. Create Component Instance (Status: IN_PROGRESS)
	instance := &model.ComponentInstance{
		CaseID:      caseID,
		ComponentID: component.ID,
		Status:      "IN_PROGRESS",
		CreatedAt:   time.Now(),
	}
	// Note: CreateComponentInstance in repo takes struct.
	// We need to fetch children if it's a container.

	if err := e.Repo.CreateComponentInstance(ctx, tx, instance); err != nil {
		return fmt.Errorf("failed to create component instance: %w", err)
	}

	// 2. Trigger Pre-Hooks (TODO)

	// 3. specific logic based on Type
	if component.Type == model.ComponentTypeTask {
		// It's a leaf. If it's SYSTEM, schedule it. If USER, wait.
		// For now, let's treat it as "Waiting for Completion" via external event
		// or if we have a TaskType in config...
		// Simplified: Just mark as IN_PROGRESS and wait for TASK_COMPLETED event.
		return nil
	}

	// 4. If Container (STAGE/ACTIVITY), execute children
	children, err := e.Repo.GetChildren(ctx, tx, component.ID)
	if err != nil {
		return fmt.Errorf("failed to fetch children: %w", err)
	}

	if len(children) == 0 {
		// Empty container, complete immediately?
		return e.CompleteComponent(ctx, tx, instance.ID)
	}

	if component.ExecutionStrategy == model.StrategyParallel {
		// Execute ALL children
		for _, child := range children {
			if err := e.ActivateComponent(ctx, tx, caseID, child); err != nil {
				return err
			}
		}
	} else {
		// SEQUENTIAL: Execute FIRST child
		if err := e.ActivateComponent(ctx, tx, caseID, children[0]); err != nil {
			return err
		}
	}

	return nil
}

// CompleteComponent marks a component as done and triggers next steps
func (e *Engine) CompleteComponent(ctx context.Context, tx repository.DBExecutor, instanceID string) error {
	slog.Info("completing component instance", "instance_id", instanceID)

	// 1. Update Status
	if err := e.Repo.UpdateComponentInstanceStatus(ctx, tx, instanceID, "COMPLETED"); err != nil {
		return err
	}

	// 2. Fetch Instance to get Component details
	instance, err := e.Repo.GetComponentInstance(ctx, tx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to fetch instance: %w", err)
	}

	// 3. Fetch current Component Definition to find Parent
	component, err := e.Repo.GetWorkflowComponent(ctx, tx, instance.ComponentID)
	if err != nil {
		return fmt.Errorf("failed to fetch component def: %w", err)
	}

	// 4. If Root, Workflow is done
	if component.ParentComponentID == nil {
		slog.Info("root component completed, workflow finished", "name", component.Name)
		return nil
	}

	// 5. Fetch Parent Component to check Strategy
	parent, err := e.Repo.GetWorkflowComponent(ctx, tx, *component.ParentComponentID)
	if err != nil {
		return fmt.Errorf("failed to fetch parent: %w", err)
	}

	if parent.ExecutionStrategy == model.StrategySequential {
		// Find Next Sibling
		nextSibling, err := e.Repo.GetNextSibling(ctx, tx, component.ID)
		if err != nil {
			return fmt.Errorf("failed to find next sibling: %w", err)
		}

		if nextSibling != nil {
			// Execute Next Sibling
			return e.ActivateComponent(ctx, tx, instance.CaseID, nextSibling)
		}

		// No next sibling -> Parent is Complete
		slog.Info("sequence finished, completing parent", "parent", parent.Name)
		parentInstanceID, err := e.Repo.GetActiveInstanceID(ctx, tx, instance.CaseID, parent.ID)
		if err != nil {
			return fmt.Errorf("failed to find parent instance: %w", err)
		}
		return e.CompleteComponent(ctx, tx, parentInstanceID)

	} else if parent.ExecutionStrategy == model.StrategyParallel {
		// Check if ALL siblings are complete
		allDone, err := e.Repo.AreAllChildrenComplete(ctx, tx, instance.CaseID, parent.ID)
		if err != nil {
			return fmt.Errorf("failed to check siblings: %w", err)
		}

		if allDone {
			slog.Info("parallel execution finished, completing parent", "parent", parent.Name)
			parentInstanceID, err := e.Repo.GetActiveInstanceID(ctx, tx, instance.CaseID, parent.ID)
			if err != nil {
				return fmt.Errorf("failed to find parent instance: %w", err)
			}
			return e.CompleteComponent(ctx, tx, parentInstanceID)
		}
		// Else, wait for others
	}

	return nil
}
