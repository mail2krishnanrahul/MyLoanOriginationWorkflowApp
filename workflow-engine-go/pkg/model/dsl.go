package model

// WorkflowYAML represents the top-level workflow definition in YAML
type WorkflowYAML struct {
	ID          string      `yaml:"id"`
	Version     int         `yaml:"version"`
	CaseType    string      `yaml:"case_type"`
	Description string      `yaml:"description,omitempty"`
	Stages      []StageYAML `yaml:"stages"`
}

// StageYAML represents a stage in the workflow
type StageYAML struct {
	Name      string     `yaml:"name"`
	PreHooks  []HookYAML `yaml:"pre_hooks,omitempty"`
	Tasks     []TaskYAML `yaml:"tasks,omitempty"`
	PostHooks []HookYAML `yaml:"post_hooks,omitempty"`
	Routing   Routing    `yaml:"routing,omitempty"`
}

// TaskYAML represents a unit of work (System or User)
type TaskYAML struct {
	Name                string                 `yaml:"name"`
	Type                string                 `yaml:"type"` // SYSTEM or USER
	UIConfig            map[string]interface{} `yaml:"ui_config,omitempty"`
	IntegrationEndpoint string                 `yaml:"integration_endpoint,omitempty"`
}

// HookYAML represents an automated trigger
type HookYAML struct {
	Name   string                 `yaml:"name"`
	Type   string                 `yaml:"type"` // e.g., NOTIFY, LAMBDA
	Config map[string]interface{} `yaml:"config,omitempty"`
}

// Routing defines navigation logic
type Routing struct {
	NextStage string `yaml:"next_stage,omitempty"`
	// Future: Conditional routing could go here
}
