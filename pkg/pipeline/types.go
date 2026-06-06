// Package pipeline provides the canonical types, parser, validator, DAG resolver,
// expression evaluator, and environment simulator for Flint pipeline YAML files.
package pipeline

// ---------------------------------------------------------------------------
// Pipeline — top-level representation of a .flint/*.yaml file
// ---------------------------------------------------------------------------

// Pipeline is the top-level representation of a Flint pipeline YAML file.
// Pipelines are identified by filename, not by a name field.
type Pipeline struct {
	// Environments restricts which environments this pipeline can target.
	// If empty, the pipeline can target any environment (or none).
	Environments []string `yaml:"environments,omitempty" json:"environments,omitempty"`

	// Image is the default container image for all steps.
	// Can be an image preset name or a full image reference.
	Image string `yaml:"image,omitempty" json:"image,omitempty"`

	// Runner is the default runner pool for all steps that do not specify
	// their own runner. When set, consecutive top-level steps that share this
	// runner (and have no explicit dependsOn or gate) are automatically
	// coalesced into a single nested steps: group so they run in one K8s Job
	// with a shared emptyDir workspace — eliminating the need for explicit
	// workspace sync between sequential steps.
	Runner string `yaml:"runner,omitempty" json:"runner,omitempty"`

	// ServiceAccount is the K8s ServiceAccount for step pods in this pipeline.
	// Overrides the runner pool's service account. Individual steps can override
	// this further with their own serviceAccount field.
	// Used for IAM role-based auth (IRSA, Workload Identity) scoped to this
	// pipeline rather than the entire runner pool.
	ServiceAccount string `yaml:"serviceAccount,omitempty" json:"serviceAccount,omitempty"`

	// Triggers defines when and how pipeline runs are created.
	Triggers Triggers `yaml:"triggers" json:"triggers"`

	// Steps defines the pipeline's execution graph.
	Steps []Step `yaml:"steps" json:"steps"`
}

// ---------------------------------------------------------------------------
// Triggers
// ---------------------------------------------------------------------------

// Triggers defines all trigger types for a pipeline.
type Triggers struct {
	Push        *PushTrigger        `yaml:"push,omitempty" json:"push,omitempty"`
	PullRequest *PullRequestTrigger `yaml:"pull_request,omitempty" json:"pullRequest,omitempty"`
	Manual      *ManualTrigger      `yaml:"manual,omitempty" json:"manual,omitempty"`
	Schedule    *ScheduleTrigger    `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	Tag         *TagTrigger         `yaml:"tag,omitempty" json:"tag,omitempty"`
	Promotion   []PromotionTrigger  `yaml:"promotion,omitempty" json:"promotion,omitempty"`
	Webhook     *WebhookTrigger     `yaml:"webhook,omitempty" json:"webhook,omitempty"`
}

// HasAny returns true if at least one trigger is defined.
func (t *Triggers) HasAny() bool {
	return t.Push != nil ||
		t.PullRequest != nil ||
		t.Manual != nil ||
		t.Schedule != nil ||
		t.Tag != nil ||
		len(t.Promotion) > 0 ||
		t.Webhook != nil
}

// PushTrigger runs when commits are pushed to matching branches.
type PushTrigger struct {
	Branches     []string `yaml:"branches" json:"branches"`
	Paths        []string `yaml:"paths,omitempty" json:"paths,omitempty"`
	Environments []string `yaml:"environments,omitempty" json:"environments,omitempty"`
}

// PullRequestTrigger runs when a PR is opened/updated against matching branches.
// Pull request triggers cannot have environments — they are always plain CI.
type PullRequestTrigger struct {
	Branches []string `yaml:"branches" json:"branches"`
	Paths    []string `yaml:"paths,omitempty" json:"paths,omitempty"`
}

// ManualTrigger allows runs triggered by a user from the UI or CLI.
type ManualTrigger struct {
	Environments []string      `yaml:"environments,omitempty" json:"environments,omitempty"`
	Inputs       []ManualInput `yaml:"inputs,omitempty" json:"inputs,omitempty"`
}

// ManualInput defines a user-provided form field for manual triggers.
type ManualInput struct {
	Name        string   `yaml:"name" json:"name"`
	Type        string   `yaml:"type" json:"type"` // string, boolean, choice
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool     `yaml:"required,omitempty" json:"required,omitempty"`
	Default     string   `yaml:"default,omitempty" json:"default,omitempty"`
	Options     []string `yaml:"options,omitempty" json:"options,omitempty"` // for type: choice
}

// ScheduleTrigger runs on a cron schedule.
type ScheduleTrigger struct {
	Cron         string   `yaml:"cron" json:"cron"`
	Environments []string `yaml:"environments,omitempty" json:"environments,omitempty"`
}

// TagTrigger runs when a tag matching the pattern is pushed.
type TagTrigger struct {
	Patterns     []string `yaml:"patterns" json:"patterns"`
	Environments []string `yaml:"environments,omitempty" json:"environments,omitempty"`
}

// PromotionTrigger runs when a previous environment run succeeds.
type PromotionTrigger struct {
	From          string   `yaml:"from" json:"from"`
	Environments  []string `yaml:"environments" json:"environments"`
	RequireStatus string   `yaml:"requireStatus,omitempty" json:"requireStatus,omitempty"` // default: succeeded
}

// WebhookTrigger runs when an external HTTP request hits the pipeline's endpoint.
type WebhookTrigger struct {
	Secret       string   `yaml:"secret,omitempty" json:"secret,omitempty"`
	Environments []string `yaml:"environments,omitempty" json:"environments,omitempty"`
}

// ---------------------------------------------------------------------------
// Steps
// ---------------------------------------------------------------------------

// Step represents a single step in the pipeline. A step has exactly one
// execution type: run, use, steps (nested), or gate. Never a combination.
type Step struct {
	// Identity
	Name string `yaml:"name" json:"name"`

	// Execution — exactly one of these must be set
	Run   RunCommand `yaml:"run,omitempty" json:"run,omitempty"`     // shell command(s) — string or list
	Use   string     `yaml:"use,omitempty" json:"use,omitempty"`     // step template reference
	Steps []Step     `yaml:"steps,omitempty" json:"steps,omitempty"` // nested sub-steps (shared pod)
	Gate  *Gate      `yaml:"gate,omitempty" json:"gate,omitempty"`   // approval checkpoint

	// Template inputs (when using use:)
	With map[string]string `yaml:"with,omitempty" json:"with,omitempty"`

	// Container
	Image      string `yaml:"image,omitempty" json:"image,omitempty"`
	Shell      string `yaml:"shell,omitempty" json:"shell,omitempty"`           // sh (default), bash, python
	WorkingDir string `yaml:"workingDir,omitempty" json:"workingDir,omitempty"` // default: /workspace

	// Execution graph
	DependsOn []string `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`

	// Compute
	Runner         string `yaml:"runner,omitempty" json:"runner,omitempty"`                 // runner pool name
	ServiceAccount string `yaml:"serviceAccount,omitempty" json:"serviceAccount,omitempty"` // K8s SA override

	// Environment filtering
	Environments []string `yaml:"environments,omitempty" json:"environments,omitempty"`

	// Execution control
	Timeout         string     `yaml:"timeout,omitempty" json:"timeout,omitempty"` // default: 1h
	If              string     `yaml:"if,omitempty" json:"if,omitempty"`           // conditional expression
	When            string     `yaml:"when,omitempty" json:"when,omitempty"`       // onSuccess (default), onFailure, always
	ContinueOnError bool       `yaml:"continueOnError,omitempty" json:"continueOnError,omitempty"`
	Retry           *RetrySpec `yaml:"retry,omitempty" json:"retry,omitempty"`

	// Environment variables
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Secrets map[string]string `yaml:"secrets,omitempty" json:"secrets,omitempty"` // shorthand for secret references

	// Artifacts
	Inputs  []ArtifactInput  `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Outputs []ArtifactOutput `yaml:"outputs,omitempty" json:"outputs,omitempty"`

	// Sidecars
	Services []Service `yaml:"services,omitempty" json:"services,omitempty"`

	// Caching
	Cache *CacheSpec `yaml:"cache,omitempty" json:"cache,omitempty"`

	// Matrix
	Matrix map[string][]string `yaml:"matrix,omitempty" json:"matrix,omitempty"`
}

// ExecType returns which execution type is set: "run", "use", "steps", or "gate". Returns empty string if none is set.
func (s *Step) ExecType() string {
	switch {
	case !s.Run.IsEmpty():
		return "run"
	case s.Use != "":
		return "use"
	case len(s.Steps) > 0:
		return "steps"
	case s.Gate != nil:
		return "gate"
	default:
		return ""
	}
}

// IsNested returns true if this step contains sub-steps (execution type "steps").
func (s *Step) IsNested() bool {
	return len(s.Steps) > 0
}

// ---------------------------------------------------------------------------
// Gate
// ---------------------------------------------------------------------------

// Gate defines an approval checkpoint. A gate step has no run command —
// it pauses the pipeline until the required approvals are received.
type Gate struct {
	Approvers    []string `yaml:"approvers" json:"approvers"`
	MinApprovals int      `yaml:"minApprovals,omitempty" json:"minApprovals,omitempty"` // default: 1
}

// ---------------------------------------------------------------------------
// Step configuration types
// ---------------------------------------------------------------------------

// RetrySpec defines retry behavior on step failure.
type RetrySpec struct {
	Attempts int    `yaml:"attempts" json:"attempts"` // total attempts including first
	Delay    string `yaml:"delay,omitempty" json:"delay,omitempty"`
}

// ArtifactInput declares an artifact to download before step execution.
type ArtifactInput struct {
	From string `yaml:"from" json:"from"` // source step name
	Path string `yaml:"path" json:"path"` // local path to extract to
}

// ArtifactOutput declares an artifact to upload after step execution.
type ArtifactOutput struct {
	Path string `yaml:"path" json:"path"` // local path to upload
}

// Service defines a sidecar container that runs alongside a step.
type Service struct {
	Name  string            `yaml:"name" json:"name"`
	Image string            `yaml:"image" json:"image"`
	Env   map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

// CacheSpec defines dependency caching for a step.
type CacheSpec struct {
	Key   string   `yaml:"key" json:"key"`     // cache key (supports expressions, e.g. hashFiles)
	Paths []string `yaml:"paths" json:"paths"` // paths to cache
}
