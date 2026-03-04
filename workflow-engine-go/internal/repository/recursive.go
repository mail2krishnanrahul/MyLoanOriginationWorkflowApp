package repository

import (
	"context"
	"fmt"
	"workflow-engine/pkg/model"
)

// GetRootComponents fetches the top-level components for a specific version
func (r *Repository) GetRootComponents(ctx context.Context, executor DBExecutor, versionID string) ([]*model.WorkflowComponent, error) {
	query := `
		SELECT id, version_id, parent_component_id, type, name, execution_strategy, execution_order, config, created_at
		FROM workflow_components
		WHERE version_id = $1::uuid AND parent_component_id IS NULL
		ORDER BY execution_order ASC`

	if executor == nil {
		executor = r.Pool
	}

	rows, err := executor.Query(ctx, query, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var components []*model.WorkflowComponent
	for rows.Next() {
		var c model.WorkflowComponent
		if err := rows.Scan(&c.ID, &c.VersionID, &c.ParentComponentID, &c.Type, &c.Name, &c.ExecutionStrategy, &c.ExecutionOrder, &c.Config, &c.CreatedAt); err != nil {
			return nil, err
		}
		components = append(components, &c)
	}
	return components, nil
}

// GetChildren fetches the immediate children of a component
func (r *Repository) GetChildren(ctx context.Context, executor DBExecutor, componentID string) ([]*model.WorkflowComponent, error) {
	query := `
		SELECT id, version_id, parent_component_id, type, name, execution_strategy, execution_order, config, created_at
		FROM workflow_components
		WHERE parent_component_id = $1::uuid
		ORDER BY execution_order ASC`

	if executor == nil {
		executor = r.Pool
	}

	rows, err := executor.Query(ctx, query, componentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var children []*model.WorkflowComponent
	for rows.Next() {
		var c model.WorkflowComponent
		if err := rows.Scan(&c.ID, &c.VersionID, &c.ParentComponentID, &c.Type, &c.Name, &c.ExecutionStrategy, &c.ExecutionOrder, &c.Config, &c.CreatedAt); err != nil {
			return nil, err
		}
		children = append(children, &c)
	}
	return children, nil
}

// GetNextSibling finds the next component in sequence within the same parent
func (r *Repository) GetNextSibling(ctx context.Context, executor DBExecutor, currentComponentID string) (*model.WorkflowComponent, error) {
	// First, get the current component to know its parent and order
	// This could be optimized with a single query join, but let's keep it simple for now or use a subquery
	query := `
		WITH current_comp AS (
			SELECT parent_component_id, execution_order, version_id
			FROM workflow_components
			WHERE id = $1::uuid
		)
		SELECT wc.id, wc.version_id, wc.parent_component_id, wc.type, wc.name, wc.execution_strategy, wc.execution_order, wc.config, wc.created_at
		FROM workflow_components wc, current_comp cc
		WHERE 
			((wc.parent_component_id IS NULL AND cc.parent_component_id IS NULL) OR wc.parent_component_id = cc.parent_component_id)
			AND wc.version_id = cc.version_id
			AND wc.execution_order > cc.execution_order
		ORDER BY wc.execution_order ASC
		LIMIT 1`

	if executor == nil {
		executor = r.Pool
	}

	var c model.WorkflowComponent
	err := executor.QueryRow(ctx, query, currentComponentID).Scan(
		&c.ID, &c.VersionID, &c.ParentComponentID, &c.Type, &c.Name, &c.ExecutionStrategy, &c.ExecutionOrder, &c.Config, &c.CreatedAt,
	)
	if err != nil {
		// If no rows, it means no next sibling
		return nil, nil // Return nil, nil for no next sibling (end of sequence)
	}
	return &c, nil
}

// CreateComponentInstance creates a tracking record for a case's execution of a component
func (r *Repository) CreateComponentInstance(ctx context.Context, executor DBExecutor, instance *model.ComponentInstance) error {
	query := `
		INSERT INTO component_instances (case_id, component_id, status, data_payload, created_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5)
		RETURNING id`

	if executor == nil {
		executor = r.Pool
	}

	return executor.QueryRow(ctx, query, instance.CaseID, instance.ComponentID, instance.Status, instance.DataPayload, instance.CreatedAt).Scan(&instance.ID)
}

// UpdateComponentInstanceStatus updates the status of a component instance
func (r *Repository) UpdateComponentInstanceStatus(ctx context.Context, executor DBExecutor, instanceID string, status string) error {
	query := `
		UPDATE component_instances
		SET status = $1, updated_at = NOW() -- Assuming updated_at exists or we just update status
		WHERE id = $2::uuid`
	// Note: Schema in 005 didn't explicitly have updated_at, but good practice.
	// Re-checking 005... it has started_at, completed_at, created_at.
	// Let's set completed_at if status is COMPLETED

	switch status {
	case "COMPLETED":
		query = `
			UPDATE component_instances
			SET status = $1, completed_at = NOW()
			WHERE id = $2::uuid`
	case "IN_PROGRESS":
		query = `
			UPDATE component_instances
			SET status = $1, started_at = NOW()
			WHERE id = $2::uuid`
	default:
		query = `UPDATE component_instances SET status = $1 WHERE id = $2::uuid`
	}

	if executor == nil {
		executor = r.Pool
	}

	_, err := executor.Exec(ctx, query, status, instanceID)
	return err
}

// GetComponentInstance fetches an instance by ID
func (r *Repository) GetComponentInstance(ctx context.Context, executor DBExecutor, instanceID string) (*model.ComponentInstance, error) {
	query := `
		SELECT id, case_id, component_id, status, data_payload, started_at, completed_at, created_at
		FROM component_instances
		WHERE id = $1::uuid`

	if executor == nil {
		executor = r.Pool
	}

	var ci model.ComponentInstance
	err := executor.QueryRow(ctx, query, instanceID).Scan(
		&ci.ID, &ci.CaseID, &ci.ComponentID, &ci.Status, &ci.DataPayload, &ci.StartedAt, &ci.CompletedAt, &ci.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &ci, nil
}

// GetWorkflowComponent fetches a component definition by ID
func (r *Repository) GetWorkflowComponent(ctx context.Context, executor DBExecutor, componentID string) (*model.WorkflowComponent, error) {
	query := `
		SELECT id, version_id, parent_component_id, type, name, execution_strategy, execution_order, config, created_at
		FROM workflow_components
		WHERE id = $1::uuid`

	if executor == nil {
		executor = r.Pool
	}

	var c model.WorkflowComponent
	err := executor.QueryRow(ctx, query, componentID).Scan(
		&c.ID, &c.VersionID, &c.ParentComponentID, &c.Type, &c.Name, &c.ExecutionStrategy, &c.ExecutionOrder, &c.Config, &c.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetActiveInstanceID finds the ID of an active instance (IN_PROGRESS) for a component
func (r *Repository) GetActiveInstanceID(ctx context.Context, executor DBExecutor, caseID string, componentID string) (string, error) {
	query := `
		SELECT id 
		FROM component_instances
		WHERE case_id = $1::uuid AND component_id = $2::uuid AND status = 'IN_PROGRESS'
		LIMIT 1`

	if executor == nil {
		executor = r.Pool
	}

	var id string
	err := executor.QueryRow(ctx, query, caseID, componentID).Scan(&id)
	return id, err
}

// AreAllChildrenComplete checks if all children of a parent component have completed execution for a case
func (r *Repository) AreAllChildrenComplete(ctx context.Context, executor DBExecutor, caseID string, parentComponentID string) (bool, error) {
	// Logic:
	// 1. Get all children IDs of the parent
	// 2. Check if all of them have a COMPLETED instance for this case
	// OR simpler: Count total children vs Count completed children
	// OR: Check if there is any child that does NOT have a COMPLETED instance.

	query := `
		SELECT COUNT(*) 
		FROM workflow_components child
		LEFT JOIN component_instances ci ON child.id = ci.component_id AND ci.case_id = $1::uuid AND ci.status = 'COMPLETED'
		WHERE child.parent_component_id = $2::uuid 
		AND ci.id IS NULL`

	// If the count is > 0, it means there is a child without a COMPLETED instance.
	// So AllComplete = (Count == 0)

	if executor == nil {
		executor = r.Pool
	}

	var startCount int
	err := executor.QueryRow(ctx, query, caseID, parentComponentID).Scan(&startCount)
	if err != nil {
		return false, err
	}
	return startCount == 0, nil
}

// CreateCase creates a new case instance for a given definition name
func (r *Repository) CreateCase(ctx context.Context, executor DBExecutor, caseDefinitionName string, data map[string]interface{}) (string, error) {
	// 1. Find Case Definition ID
	var caseDefID string
	err := r.Pool.QueryRow(ctx, `SELECT id FROM case_definitions WHERE name = $1`, caseDefinitionName).Scan(&caseDefID)
	if err != nil {
		return "", fmt.Errorf("case definition not found: %w", err)
	}

	// 2. Find Active Version
	var versionID string
	err = r.Pool.QueryRow(ctx, `SELECT id FROM version_registry WHERE case_definition_id = $1 AND status = 'ACTIVE' ORDER BY version DESC LIMIT 1`, caseDefID).Scan(&versionID)
	if err != nil {
		return "", fmt.Errorf("active version not found: %w", err)
	}

	// 3. Insert Case
	// Note: We need a cases table. The migration 005 commented it out as replacement.
	// We need to ensure the 'cases' table exists and matches.
	// For this test, let's assume 'cases' table has: id, case_definition_id, pinned_version_id, global_status, applicant_data

	// Create table if not exists (Hack for test or ensure migration is run)
	// But strictly, we should have a migration for `cases`.
	// I'll assume standard table structure.

	var caseID string
	query := `
		INSERT INTO cases (case_definition_id, pinned_version_id, global_status, applicant_data)
		VALUES ($1, $2, 'OPEN', $3)
		RETURNING id`

	if executor == nil {
		executor = r.Pool
	}

	err = executor.QueryRow(ctx, query, caseDefID, versionID, data).Scan(&caseID)
	if err != nil {
		return "", fmt.Errorf("failed to create case: %w", err)
	}
	return caseID, nil
}
