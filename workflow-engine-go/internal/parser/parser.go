package parser

import (
	"fmt"
	"sync"
	"workflow-engine/pkg/model"

	"gopkg.in/yaml.v3"
)

// Registry holds loaded workflow definitions in memory
type Registry struct {
	mu        sync.RWMutex
	workflows map[string]*model.WorkflowYAML // Key: version_hash or id:version
}

// NewRegistry creates a new workflow registry
func NewRegistry() *Registry {
	return &Registry{
		workflows: make(map[string]*model.WorkflowYAML),
	}
}

// Load parses YAML content and registers the workflow if valid
func (r *Registry) Load(yamlContent []byte) (*model.WorkflowYAML, error) {
	var wf model.WorkflowYAML
	if err := yaml.Unmarshal(yamlContent, &wf); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if err := r.Validate(&wf); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Store in registry using a key (e.g., "$ID:$Version")
	key := fmt.Sprintf("%s:%d", wf.ID, wf.Version)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.workflows[key] = &wf

	return &wf, nil
}

// Get retrieves a workflow definition by ID and Version
func (r *Registry) Get(id string, version int) (*model.WorkflowYAML, bool) {
	key := fmt.Sprintf("%s:%d", id, version)
	r.mu.RLock()
	defer r.mu.RUnlock()
	wf, ok := r.workflows[key]
	return wf, ok
}

// Validate performs structural and logical checks on the workflow
func (r *Registry) Validate(wf *model.WorkflowYAML) error {
	if wf.ID == "" {
		return fmt.Errorf("workflow ID is required")
	}
	if len(wf.Stages) == 0 {
		return fmt.Errorf("workflow must have at least one stage")
	}

	stageMap := make(map[string]bool)
	for _, stage := range wf.Stages {
		if stage.Name == "" {
			return fmt.Errorf("stage name is required")
		}
		if stageMap[stage.Name] {
			return fmt.Errorf("duplicate stage name: %s", stage.Name)
		}
		stageMap[stage.Name] = true
	}

	// Check Routing references and DAG cycles
	if err := r.checkDAG(wf, stageMap); err != nil {
		return err
	}

	return nil
}

// checkDAG verifies that next_stage references exist and there are no cycles
func (r *Registry) checkDAG(wf *model.WorkflowYAML, stageMap map[string]bool) error {
	// 1. Verify links
	adj := make(map[string]string)
	for _, stage := range wf.Stages {
		if stage.Routing.NextStage != "" {
			if !stageMap[stage.Routing.NextStage] {
				return fmt.Errorf("stage '%s' routes to unknown stage '%s'", stage.Name, stage.Routing.NextStage)
			}
			adj[stage.Name] = stage.Routing.NextStage
		}
	}

	// 2. Detect Cycles (DFS)
	visited := make(map[string]bool)
	recursionStack := make(map[string]bool)

	var detectCycle func(node string) error
	detectCycle = func(node string) error {
		visited[node] = true
		recursionStack[node] = true

		next, hasNext := adj[node]
		if hasNext {
			if !visited[next] {
				if err := detectCycle(next); err != nil {
					return err
				}
			} else if recursionStack[next] {
				return fmt.Errorf("cycle detected involving stage '%s'", next)
			}
		}

		recursionStack[node] = false
		return nil
	}

	for stageName := range stageMap {
		if !visited[stageName] {
			if err := detectCycle(stageName); err != nil {
				return err
			}
		}
	}

	return nil
}
