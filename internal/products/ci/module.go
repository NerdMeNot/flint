package ci

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Module is a reusable, versioned, parameterized unit referenced by use:/extends:.
// Its kind decides where it plugs in and its environment stance:
//
//	action   — containerized, brings its own image (a step). Portable.
//	steps    — inline step-template (runs in the caller's image). Needs requires:.
//	job      — inline job-template (pins its own image). Env-honest.
//	pipeline — inline whole-pipeline template (extends:).
type Module struct {
	Name     string                  `yaml:"name" json:"name"`
	Kind     string                  `yaml:"kind" json:"kind"`
	Inputs   map[string]Input        `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Outputs  map[string]ModuleOutput `yaml:"outputs,omitempty" json:"outputs,omitempty"`
	Requires *Requires               `yaml:"requires,omitempty" json:"requires,omitempty"`

	// Bodies — exactly one, matching Kind.
	Run   *ActionRun     `yaml:"run,omitempty" json:"run,omitempty"`     // kind: action
	Steps []Step         `yaml:"steps,omitempty" json:"steps,omitempty"` // kind: steps
	Job   *Job           `yaml:"job,omitempty" json:"job,omitempty"`     // kind: job
	Jobs  map[string]Job `yaml:"jobs,omitempty" json:"jobs,omitempty"`   // kind: pipeline
}

// Input is a typed module parameter.
type Input struct {
	Type     string   `yaml:"type,omitempty" json:"type,omitempty"` // string|number|boolean|enum|steps
	Required bool     `yaml:"required,omitempty" json:"required,omitempty"`
	Default  string   `yaml:"default,omitempty" json:"default,omitempty"`
	Options  []string `yaml:"options,omitempty" json:"options,omitempty"` // enum
}

// ModuleOutput declares (and maps) a value a job/pipeline module exports.
type ModuleOutput struct {
	Type  string `yaml:"type,omitempty" json:"type,omitempty"`
	Value string `yaml:"value,omitempty" json:"value,omitempty"`
}

// Requires is the environment contract an inline module asserts about the image
// it runs in (statically checked against the consuming job's image).
type Requires struct {
	Family string   `yaml:"family,omitempty" json:"family,omitempty"` // debian|rhel|alpine
	Tools  []string `yaml:"tools,omitempty" json:"tools,omitempty"`
}

// ActionRun is a containerized module's body: its own image + command.
type ActionRun struct {
	Image   string            `yaml:"image" json:"image"`
	Command []string          `yaml:"command,omitempty" json:"command,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

// ParseModule decodes and validates a module definition.
func ParseModule(data []byte) (*Module, error) {
	var m Module
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("ci: parse module: %w", err)
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Module) validate() error {
	if m.Name == "" {
		return fmt.Errorf("ci: module name is required")
	}
	bodies := 0
	switch m.Kind {
	case "action":
		if m.Run == nil || m.Run.Image == "" {
			return fmt.Errorf("ci: action module %q needs run.image", m.Name)
		}
		bodies++
	case "steps":
		if len(m.Steps) > 0 {
			bodies++
		}
	case "job":
		if m.Job != nil {
			bodies++
		}
	case "pipeline":
		if len(m.Jobs) > 0 {
			bodies++
		}
	default:
		return fmt.Errorf("ci: module %q has invalid kind %q (action|steps|job|pipeline)", m.Name, m.Kind)
	}
	if bodies != 1 {
		return fmt.Errorf("ci: module %q body does not match kind %q", m.Name, m.Kind)
	}
	for name, in := range m.Inputs {
		if in.Type == "enum" && len(in.Options) == 0 {
			return fmt.Errorf("ci: module %q input %q is enum without options", m.Name, name)
		}
	}
	return nil
}

// ModuleResolver resolves a use:/extends: reference to a Module. References are
// immutable (exact version, publisher alias, or a local path) — there is no
// lockfile. The registry-backed resolver (GitOps / API) lands with the registry;
// MapResolver and local-file resolution cover in-repo reuse and tests.
type ModuleResolver interface {
	Resolve(ref string) (*Module, error)
}

// MapResolver resolves references from an in-memory map (tests, and the basis for
// a future registry cache).
type MapResolver map[string]*Module

// Resolve returns the module registered under ref.
func (m MapResolver) Resolve(ref string) (*Module, error) {
	mod, ok := m[ref]
	if !ok {
		return nil, fmt.Errorf("ci: module %q not found", ref)
	}
	return mod, nil
}
