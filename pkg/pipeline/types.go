// Package pipeline provides the canonical types, parser, validator, DAG resolver,
// and expression evaluator for Flint pipeline YAML files.
package pipeline

import "time"

// Pipeline is the top-level representation of a .flint/*.yaml file.
type Pipeline struct {
	Name        string            `yaml:"pipeline"`
	Description string            `yaml:"description,omitempty"`
	Version     int               `yaml:"version,omitempty"`
	Parameters  map[string]Param  `yaml:"parameters,omitempty"`
	Secrets     []SecretRef       `yaml:"secrets,omitempty"`
	Triggers    Triggers          `yaml:"triggers,omitempty"`
	Concurrency *Concurrency     `yaml:"concurrency,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	Defaults    *Defaults         `yaml:"defaults,omitempty"`
	Permissions *Permissions      `yaml:"permissions,omitempty"`
	Steps       []Step            `yaml:"steps"`
}

// Param defines a pipeline-level parameter.
type Param struct {
	Type        string   `yaml:"type"`
	Default     any      `yaml:"default,omitempty"`
	Options     []string `yaml:"options,omitempty"`
	Description string   `yaml:"description,omitempty"`
}

// SecretRef is either a plain string name or a structured secret declaration.
// Custom unmarshalling handles both forms.
type SecretRef struct {
	Name        string `yaml:"name"`
	Optional    bool   `yaml:"optional,omitempty"`
	Description string `yaml:"description,omitempty"`
}

// Triggers defines all trigger types for a pipeline.
type Triggers struct {
	Push     *PushTrigger     `yaml:"push,omitempty"`
	PR       *PRTrigger       `yaml:"pr,omitempty"`
	Tag      *TagTrigger      `yaml:"tag,omitempty"`
	Schedule []ScheduleEntry  `yaml:"schedule,omitempty"`
	Manual   *ManualTrigger   `yaml:"manual,omitempty"`
	API      *APITrigger      `yaml:"api,omitempty"`
	On       *OnTrigger       `yaml:"on,omitempty"`
	Webhook  *WebhookTrigger  `yaml:"webhook,omitempty"`
	Call     *CallTrigger     `yaml:"call,omitempty"`
}

type PushTrigger struct {
	Branches       []string `yaml:"branches,omitempty"`
	BranchesIgnore []string `yaml:"branches-ignore,omitempty"`
	Paths          []string `yaml:"paths,omitempty"`
	PathsIgnore    []string `yaml:"paths-ignore,omitempty"`
}

type PRTrigger struct {
	Branches []string `yaml:"branches,omitempty"`
	Types    []string `yaml:"types,omitempty"`
	Paths    []string `yaml:"paths,omitempty"`
	Draft    *bool    `yaml:"draft,omitempty"`
}

type TagTrigger struct {
	Patterns []string     `yaml:"patterns,omitempty"`
	Semver   *SemverSpec  `yaml:"semver,omitempty"`
}

type SemverSpec struct {
	Range      string `yaml:"range,omitempty"`
	Prerelease bool   `yaml:"prerelease,omitempty"`
}

type ScheduleEntry struct {
	Cron string `yaml:"cron"`
}

type ManualTrigger struct {
	Inputs map[string]ManualInput `yaml:"inputs,omitempty"`
}

type ManualInput struct {
	Type        string   `yaml:"type"`
	Options     []string `yaml:"options,omitempty"`
	Required    bool     `yaml:"required,omitempty"`
	Default     any      `yaml:"default,omitempty"`
	Description string   `yaml:"description,omitempty"`
}

type APITrigger struct{}

type OnTrigger struct {
	Workflow string   `yaml:"workflow"`
	Branches []string `yaml:"branches,omitempty"`
	Status   []string `yaml:"status,omitempty"`
}

type WebhookTrigger struct {
	Secret string                   `yaml:"secret,omitempty"`
	Inputs map[string]WebhookInput  `yaml:"inputs,omitempty"`
}

type WebhookInput struct {
	From    string `yaml:"from"`
	Type    string `yaml:"type,omitempty"`
	Default string `yaml:"default,omitempty"`
}

type CallTrigger struct {
	Inputs  map[string]CallInput  `yaml:"inputs,omitempty"`
	Secrets map[string]CallSecret `yaml:"secrets,omitempty"`
	Outputs map[string]CallOutput `yaml:"outputs,omitempty"`
}

type CallInput struct {
	Type    string `yaml:"type,omitempty"`
	Default any    `yaml:"default,omitempty"`
}

type CallSecret struct {
	Required bool `yaml:"required,omitempty"`
}

type CallOutput struct {
	Value string `yaml:"value"`
}

// Concurrency controls how parallel runs of the same pipeline are handled.
type Concurrency struct {
	Group      string `yaml:"group"`
	Mode       string `yaml:"mode,omitempty"` // cancel | queue | allow
	QueueLimit int    `yaml:"queueLimit,omitempty"`
}

type Defaults struct {
	Timeout    string `yaml:"timeout,omitempty"`
	WorkingDir string `yaml:"workingDir,omitempty"`
}

type Permissions struct {
	Secrets        []string `yaml:"secrets,omitempty"`
	Forge          string   `yaml:"forge,omitempty"` // read | write
	ServiceAccount string   `yaml:"serviceAccount,omitempty"`
}

// Step represents a single step in the pipeline.
// Exactly one of Run, Do, Use, Invoke, Gate, or Watch must be set.
type Step struct {
	Name   string `yaml:"name"`
	Image  string `yaml:"image,omitempty"`
	After  []string `yaml:"after,omitempty"`
	If     string   `yaml:"if,omitempty"`

	// Execution types (exactly one must be set)
	Run    string      `yaml:"run,omitempty"`
	Do     []DoTask    `yaml:"do,omitempty"`
	Use    string      `yaml:"use,omitempty"`
	Invoke string      `yaml:"invoke,omitempty"`
	Gate   *GateSpec   `yaml:"gate,omitempty"`
	Watch  *WatchSpec  `yaml:"watch,omitempty"`

	// Step configuration
	With        map[string]any    `yaml:"with,omitempty"`
	Runner      *RunnerRef        `yaml:"runner,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	Timeout     string            `yaml:"timeout,omitempty"`
	Lifecycle   *Lifecycle        `yaml:"lifecycle,omitempty"`
	Cache       *CacheSpec        `yaml:"cache,omitempty"`
	Artifacts   *ArtifactSpec     `yaml:"artifacts,omitempty"`
	Services    map[string]Service `yaml:"services,omitempty"`
	Summary     *SummarySpec      `yaml:"summary,omitempty"`
	Post        *PostSpec         `yaml:"post,omitempty"`
	Environment *EnvironmentSpec  `yaml:"environment,omitempty"`
	Matrix      map[string][]any  `yaml:"matrix,omitempty"`
	SecretsRef  map[string]string `yaml:"secrets,omitempty"`
}

// ExecType returns which execution type is set for this step.
func (s *Step) ExecType() string {
	switch {
	case s.Run != "":
		return "run"
	case len(s.Do) > 0:
		return "do"
	case s.Use != "":
		return "use"
	case s.Invoke != "":
		return "invoke"
	case s.Gate != nil:
		return "gate"
	case s.Watch != nil:
		return "watch"
	default:
		return ""
	}
}

// DoTask represents a named task inside a do: list.
// YAML form: - taskName: { run: "..." }
type DoTask struct {
	Name string `yaml:"-"`
	Run  string `yaml:"run"`
}

// GateSpec defines an approval checkpoint.
type GateSpec struct {
	Message   string            `yaml:"message"`
	Approvers []string          `yaml:"approvers,omitempty"`
	Timeout   string            `yaml:"timeout,omitempty"`
	Form      map[string]FormField `yaml:"form,omitempty"`
}

type FormField struct {
	Type     string `yaml:"type"`
	Label    string `yaml:"label,omitempty"`
	Pattern  string `yaml:"pattern,omitempty"`
	Required bool   `yaml:"required,omitempty"`
}

// WatchSpec defines a condition to wait for.
type WatchSpec struct {
	Target    string      `yaml:"target,omitempty"`
	Condition string      `yaml:"condition,omitempty"`
	Namespace string      `yaml:"namespace,omitempty"`
	HTTP      string      `yaml:"http,omitempty"`
	Expect    *WatchExpect `yaml:"expect,omitempty"`
	Interval  string      `yaml:"interval,omitempty"`
	Timeout   string      `yaml:"timeout,omitempty"`
}

type WatchExpect struct {
	Status   int    `yaml:"status,omitempty"`
	Body     *WatchBody `yaml:"body,omitempty"`
}

type WatchBody struct {
	JSONPath string `yaml:"jsonpath,omitempty"`
	Equals   string `yaml:"equals,omitempty"`
}

// RunnerRef can be a simple string name or a structured spec.
// Custom unmarshalling handles both forms.
type RunnerRef struct {
	Name   string `yaml:"name,omitempty"`
	Size   string `yaml:"size,omitempty"`
	Arch   string `yaml:"arch,omitempty"`
	GPU    string `yaml:"gpu,omitempty"`
	CPU    string `yaml:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty"`
	Spot   *bool  `yaml:"spot,omitempty"`
}

type Lifecycle struct {
	OnFailure string     `yaml:"onFailure,omitempty"` // fail | continue | skip-dependents
	Always    bool       `yaml:"always,omitempty"`
	Retry     *RetrySpec `yaml:"retry,omitempty"`
}

type RetrySpec struct {
	Attempts        int      `yaml:"attempts,omitempty"`
	Backoff         string   `yaml:"backoff,omitempty"` // fixed | exponential | linear
	Interval        string   `yaml:"interval,omitempty"`
	InitialInterval string   `yaml:"initialInterval,omitempty"`
	MaxInterval     string   `yaml:"maxInterval,omitempty"`
	RetryOn         *RetryOn `yaml:"retryOn,omitempty"`
}

type RetryOn struct {
	ExitCodes []int `yaml:"exitCodes,omitempty"`
	Not       []int `yaml:"not,omitempty"`
}

// CacheSpec can be either named cache definitions or a restore reference.
// Custom unmarshalling handles both forms.
type CacheSpec struct {
	Entries map[string]CacheEntry `yaml:"-"`
	Restore []string              `yaml:"-"`
}

type CacheEntry struct {
	Key      string   `yaml:"key"`
	Paths    []string `yaml:"paths"`
	Fallback []string `yaml:"fallback,omitempty"`
}

type ArtifactSpec struct {
	Upload   []ArtifactUpload   `yaml:"upload,omitempty"`
	Download []ArtifactDownload `yaml:"download,omitempty"`
}

type ArtifactUpload struct {
	Name      string        `yaml:"name"`
	Path      string        `yaml:"path"`
	Retention time.Duration `yaml:"-"`
	RetentionRaw string     `yaml:"retention,omitempty"`
}

type ArtifactDownload struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
	From string `yaml:"from,omitempty"`
}

type Service struct {
	Image       string            `yaml:"image"`
	Env         map[string]string `yaml:"env,omitempty"`
	HealthCheck *HealthCheck      `yaml:"healthCheck,omitempty"`
}

type HealthCheck struct {
	Test        string `yaml:"test"`
	Interval    string `yaml:"interval,omitempty"`
	Timeout     string `yaml:"timeout,omitempty"`
	Retries     int    `yaml:"retries,omitempty"`
	StartPeriod string `yaml:"startPeriod,omitempty"`
}

type SummarySpec struct {
	From   string `yaml:"from"`
	Format string `yaml:"format,omitempty"`
	Title  string `yaml:"title,omitempty"`
}

type PostSpec struct {
	Run string `yaml:"run"`
}

type EnvironmentSpec struct {
	Name      string        `yaml:"name"`
	Namespace string        `yaml:"namespace,omitempty"`
	URL       string        `yaml:"url,omitempty"`
	Window    *DeployWindow `yaml:"window,omitempty"`
}

type DeployWindow struct {
	Days     []string `yaml:"days,omitempty"`
	Hours    string   `yaml:"hours,omitempty"`
	Timezone string   `yaml:"timezone,omitempty"`
}
