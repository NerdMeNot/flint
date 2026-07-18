// Package ci is the Flint CI product: the jobs → steps pipeline language and the
// compiler that turns a pipeline into execution waves for the shared engine.
//
// A pipeline is parsed and validated here, then compiled to [][]pipeline.Step
// (the engine's step IR) and handed to engine.StartWorkflowWithWaves. The engine
// stays product-neutral; CI owns the language, triggers, and forge coupling.
//
// Per the family layering, this package may depend on internal/core and pkg/*,
// but never on another product.
package ci

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"gopkg.in/yaml.v3"
)

// Pipeline is the top-level representation of a .flint/*.yaml file: a DAG of jobs.
type Pipeline struct {
	// Defaults applied to every job that doesn't override them.
	Image          string `yaml:"image,omitempty" json:"image,omitempty"`
	Runner         string `yaml:"runner,omitempty" json:"runner,omitempty"`
	ServiceAccount string `yaml:"serviceAccount,omitempty" json:"serviceAccount,omitempty"`

	// Pipeline-wide env / secrets, merged into every job (narrower scope wins).
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Secrets []Secret          `yaml:"secrets,omitempty" json:"secrets,omitempty"`

	// Environments restricts which environments this pipeline can target.
	Environments []string `yaml:"environments,omitempty" json:"environments,omitempty"`

	// Concurrency bounds run-level concurrency (cancel-in-progress or queue).
	Concurrency *Concurrency `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`

	// Extends references a pipeline module to inherit jobs from; With supplies
	// its inputs. Module resolution happens before parse-into-this-struct.
	Extends string         `yaml:"extends,omitempty" json:"extends,omitempty"`
	With    map[string]any `yaml:"with,omitempty" json:"with,omitempty"`

	// Triggers defines when runs are created (reuses the engine trigger types).
	Triggers pipeline.Triggers `yaml:"triggers" json:"triggers"`

	// Jobs is the DAG, keyed by job name.
	Jobs map[string]Job `yaml:"jobs" json:"jobs"`
}

// Job is one node of the pipeline DAG. A job is the unit of isolation: its own
// image, disk, runner, and resources. It runs either a sequence of steps (a pod)
// or a gate (a non-pod approval).
type Job struct {
	// Isolation (per-pod).
	Image          string     `yaml:"image,omitempty" json:"image,omitempty"`
	Disk           string     `yaml:"disk,omitempty" json:"disk,omitempty"`
	Runner         string     `yaml:"runner,omitempty" json:"runner,omitempty"`
	ServiceAccount string     `yaml:"serviceAccount,omitempty" json:"serviceAccount,omitempty"`
	Resources      *Resources `yaml:"resources,omitempty" json:"resources,omitempty"`

	// Graph: the only cross-pod edges.
	Needs []string `yaml:"needs,omitempty" json:"needs,omitempty"`

	// Scoping / conditions.
	Environments []string `yaml:"environments,omitempty" json:"environments,omitempty"`
	If           string   `yaml:"if,omitempty" json:"if,omitempty"`
	// When gates the job on the outcome of its `needs` subgraph: onSuccess
	// (default) runs only if no needed job failed, onFailure only if one did,
	// always runs regardless. Scoped to this job's ancestors, not the whole
	// pipeline — an onFailure notify in one branch is unaffected by a failure in
	// an independent branch. This is the declarative failure-handling primitive;
	// `if:` (with success()/failure()/always()) refines it further.
	When string `yaml:"when,omitempty" json:"when,omitempty"`

	// Body — exactly one of steps | gate.
	Steps []Step         `yaml:"steps,omitempty" json:"steps,omitempty"`
	Gate  *pipeline.Gate `yaml:"gate,omitempty" json:"gate,omitempty"`

	// Env / secrets (merged with pipeline-level).
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Secrets []Secret          `yaml:"secrets,omitempty" json:"secrets,omitempty"`

	// Handoff out.
	Outputs   map[string]string `yaml:"outputs,omitempty" json:"outputs,omitempty"`
	Artifacts []string          `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`

	// Pod features.
	Services []pipeline.Service  `yaml:"services,omitempty" json:"services,omitempty"`
	Cache    *pipeline.CacheSpec `yaml:"cache,omitempty" json:"cache,omitempty"`

	// Matrix / fan-out.
	Matrix      *MatrixSpec `yaml:"matrix,omitempty" json:"matrix,omitempty"`
	FailFast    *bool       `yaml:"failFast,omitempty" json:"failFast,omitempty"`
	MaxParallel int         `yaml:"maxParallel,omitempty" json:"maxParallel,omitempty"`

	// Limits.
	Timeout     string       `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Concurrency *Concurrency `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`

	// Reuse: a job module reference + its inputs (resolved before validation).
	Use  string         `yaml:"use,omitempty" json:"use,omitempty"`
	With map[string]any `yaml:"with,omitempty" json:"with,omitempty"`
}

// MatrixSpec is a job's fan-out: base Dimensions (the cartesian axes) plus
// optional Include (extra or extended combinations) and Exclude (combinations to
// drop), following the GitHub Actions model. Because include/exclude nest under
// matrix: alongside the dimension axes, the spec needs a custom unmarshaler to
// separate them from the axes.
type MatrixSpec struct {
	Dimensions map[string][]string `json:"dimensions,omitempty"`
	Include    []map[string]string `json:"include,omitempty"`
	Exclude    []map[string]string `json:"exclude,omitempty"`
}

// UnmarshalYAML splits the reserved include/exclude keys from the dimension axes.
func (m *MatrixSpec) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("matrix must be a mapping of dimensions (plus optional include/exclude)")
	}
	m.Dimensions = map[string][]string{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, val := node.Content[i].Value, node.Content[i+1]
		switch key {
		case "include":
			if err := val.Decode(&m.Include); err != nil {
				return fmt.Errorf("matrix.include: %w", err)
			}
		case "exclude":
			if err := val.Decode(&m.Exclude); err != nil {
				return fmt.Errorf("matrix.exclude: %w", err)
			}
		default:
			var vals []string
			if err := val.Decode(&vals); err != nil {
				return fmt.Errorf("matrix dimension %q must be a list of values: %w", key, err)
			}
			m.Dimensions[key] = vals
		}
	}
	return nil
}

// Empty reports whether the matrix produces no fan-out at all.
func (m *MatrixSpec) Empty() bool {
	return m == nil || (len(m.Dimensions) == 0 && len(m.Include) == 0)
}

// AllKeys is every dimension key, including keys introduced only by include
// entries — so a matrix.<key> reference to an include-only key still validates.
func (m *MatrixSpec) AllKeys() map[string]bool {
	keys := map[string]bool{}
	if m == nil {
		return keys
	}
	for k := range m.Dimensions {
		keys[k] = true
	}
	for _, inc := range m.Include {
		for k := range inc {
			keys[k] = true
		}
	}
	return keys
}

// Step is a single command inside a job's pod. Steps run sequentially and share
// the job's disk and image. Steps have no pods, needs, environments, or matrix.
type Step struct {
	Name            string              `yaml:"name,omitempty" json:"name,omitempty"`
	Run             pipeline.RunCommand `yaml:"run,omitempty" json:"run,omitempty"`
	Use             string              `yaml:"use,omitempty" json:"use,omitempty"`
	With            map[string]any      `yaml:"with,omitempty" json:"with,omitempty"`
	Inject          string              `yaml:"inject,omitempty" json:"inject,omitempty"` // steps-hole (modules)
	Env             map[string]string   `yaml:"env,omitempty" json:"env,omitempty"`
	Secrets         []Secret            `yaml:"secrets,omitempty" json:"secrets,omitempty"`
	Shell           string              `yaml:"shell,omitempty" json:"shell,omitempty"`
	WorkingDir      string              `yaml:"workingDir,omitempty" json:"workingDir,omitempty"`
	Timeout         string              `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	ContinueOnError bool                `yaml:"continueOnError,omitempty" json:"continueOnError,omitempty"`
	Retry           *pipeline.RetrySpec `yaml:"retry,omitempty" json:"retry,omitempty"`
	If              string              `yaml:"if,omitempty" json:"if,omitempty"`
}

// Resources is a per-job compute request, bounded by the selected runner pool.
type Resources struct {
	CPU    string          `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	Memory string          `yaml:"memory,omitempty" json:"memory,omitempty"`
	GPU    int             `yaml:"gpu,omitempty" json:"gpu,omitempty"`
	Limits *ResourceLimits `yaml:"limits,omitempty" json:"limits,omitempty"`
}

// ResourceLimits caps a job's compute (defaults to the requests when omitted).
type ResourceLimits struct {
	CPU    string `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty" json:"memory,omitempty"`
}

// Secret is a binding: one source (built-in store via Name, or external via From)
// delivered to exactly one target (Env var or File path).
type Secret struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"` // built-in store key
	From string `yaml:"from,omitempty" json:"from,omitempty"` // external: provider:ref
	Env  string `yaml:"env,omitempty" json:"env,omitempty"`   // target: env var name
	File string `yaml:"file,omitempty" json:"file,omitempty"` // target: file path
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"` // file mode (file target)
}

// Concurrency bounds how many runs/jobs in a group run at once.
type Concurrency struct {
	Group            string `yaml:"group" json:"group"`
	CancelInProgress bool   `yaml:"cancelInProgress,omitempty" json:"cancelInProgress,omitempty"`
}

// ExecType reports a job's body kind: "steps", "gate", or "" if neither/invalid.
func (j *Job) ExecType() string {
	switch {
	case j.Gate != nil:
		return "gate"
	case len(j.Steps) > 0:
		return "steps"
	default:
		return ""
	}
}

// Parse decodes and validates a pipeline from YAML. Decoding is STRICT: an
// unknown field (a typo like `timout:` or `dependson:`) is an error, never a
// silent drop — a misspelled field that vanishes is a pipeline that misbehaves
// with no explanation.
func Parse(data []byte) (*Pipeline, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("ci: empty pipeline YAML")
	}
	if len(data) > pipeline.MaxYAMLSize {
		return nil, fmt.Errorf("ci: pipeline YAML exceeds %d bytes", pipeline.MaxYAMLSize)
	}
	// Same anchor/alias-bomb defense as pkg/pipeline — webhook-supplied YAML is
	// untrusted input.
	if err := pipeline.CheckYAMLComplexity(data); err != nil {
		return nil, fmt.Errorf("ci: %s", err.Error())
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var p Pipeline
	if err := dec.Decode(&p); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("ci: parse: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}
