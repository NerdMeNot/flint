package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/NerdMeNot/flint/internal/core/runner"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/rs/zerolog/log"
	"github.com/zeebo/xxh3"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// k8sExecutor runs steps as Kubernetes Jobs. It is the default StepExecutor and
// the only execution model for CI: an init container (flint-agent init), the
// step container, and a flint-agent sidecar, sharing a workspace volume.
type k8sExecutor struct {
	k8s           kubernetes.Interface
	reg           *runner.Registry
	agentImage    string
	jobNamespace  string
	serverURL     string
	internalToken string
	s3Bucket      string // object storage for artifacts/cache/S3-workspace ("" = disabled)
	s3Region      string
	s3Endpoint    string // optional S3-compatible endpoint (MinIO, R2)
}

// K8sExecutorConfig configures the Kubernetes Job executor.
type K8sExecutorConfig struct {
	AgentImage    string
	JobNamespace  string
	ServerURL     string
	InternalToken string
	// S3Bucket/S3Region/S3Endpoint enable the agent's artifact, cache, and
	// S3-workspace paths. When the bucket is empty those features are off and
	// the agent skips them.
	S3Bucket   string
	S3Region   string
	S3Endpoint string
}

// NewK8sExecutor builds the Kubernetes Job executor.
func NewK8sExecutor(k8s kubernetes.Interface, reg *runner.Registry, cfg K8sExecutorConfig) *k8sExecutor {
	return &k8sExecutor{
		k8s:           k8s,
		reg:           reg,
		agentImage:    cfg.AgentImage,
		jobNamespace:  cfg.JobNamespace,
		serverURL:     cfg.ServerURL,
		internalToken: cfg.InternalToken,
		s3Bucket:      cfg.S3Bucket,
		s3Region:      cfg.S3Region,
		s3Endpoint:    cfg.S3Endpoint,
	}
}

func (e *k8sExecutor) Kind() string { return "k8s" }

// CleanupRun tears down the workspace pod and any leftover Jobs for a finished
// run. Completed Jobs are reaped by TTLSecondsAfterFinished; this also catches
// cancelled runs whose step containers may still be running.
func (e *k8sExecutor) CleanupRun(ctx context.Context, runID string) error {
	if err := TeardownWorkspace(ctx, e.k8s, runID, e.jobNamespace); err != nil {
		log.Warn().Err(err).Str("runID", runID).Msg("engine: workspace teardown failed")
	}
	if err := deleteRunJobs(ctx, e.k8s, runID, e.jobNamespace); err != nil {
		log.Warn().Err(err).Str("runID", runID).Msg("engine: job cleanup failed")
	}
	return nil
}

// Dispatch creates a K8s Job for a run/use/steps step and returns the Job name.
func (e *k8sExecutor) Dispatch(ctx context.Context, step claimedStep) (string, error) {
	var stepDef pipeline.Step
	if err := json.Unmarshal(step.stepDef, &stepDef); err != nil {
		return "", fmt.Errorf("engine: unmarshal step def: %w", err)
	}

	// Resolve container image. Step image takes priority; fall back to the
	// pipeline-level default image stored on the workflow input.
	if stepDef.Image == "" {
		stepDef.Image = step.pipelineImage
	}
	if stepDef.Image == "" {
		return "", fmt.Errorf("engine: step %q has no container image (set image: on the step or at pipeline level)", step.name)
	}

	// Resolve runner pool — empty runner resolves to the registry's configured
	// default pool (worker.defaultRunnerPool).
	poolSpec, err := e.reg.Resolve(stepDef.Runner)
	if err != nil {
		log.Warn().Str("pool", stepDef.Runner).Msg("engine: runner pool not found, using defaults")
		poolSpec = &runner.PoolSpec{
			Name: "default",
			Resources: runner.ResourceProfile{
				CPU:    resource.MustParse("1"),
				Memory: resource.MustParse("2Gi"),
			},
		}
	}

	// Build the command. A group ("steps") step runs the flint-agent steps
	// driver — a static binary the init container copies into the shared
	// workspace — which executes the sub-steps sequentially inside the user's
	// image with per-sub-step if:/env/timeouts/outputs. Single "run"/"use"
	// steps keep the plain shell wrapper.
	isGroup := stepDef.ExecType() == "steps"
	var wrappedCmd []string
	if isGroup {
		wrappedCmd = []string{stepsDriverPath, "steps"}
	} else {
		wrappedCmd = wrapStepCommand(stepDef.Run.String())
	}

	// Workspace setup depends on the runner pool's workspace mode AND the
	// run's declared file flow.
	// PVC mode: all pods mount the same PVC — no workspace agent needed.
	// Agent mode (default): emptyDir per pod + gRPC workspace agent for sync —
	// but ONLY for runs that actually flow files through workspace sync.
	// Artifact-flow runs (the ci dialect) and single-container-step runs skip
	// the per-run pod + Service entirely: one fewer standing pod per run and
	// one fewer K8s API round-trip per step.
	usePVC := poolSpec.Workspace.Mode == runner.WorkspaceModePVC
	needsWorkspaceSync := step.workspaceFlow == "" || step.workspaceFlow == "sync"
	var wsAddr string

	switch {
	case usePVC:
		// Ensure the per-run PVC exists. Idempotent.
		if err := ensureWorkspacePVC(ctx, e.k8s, step.runID, poolSpec, e.jobNamespace); err != nil {
			return "", fmt.Errorf("engine: create workspace PVC: %w", err)
		}
	case needsWorkspaceSync:
		// Agent mode: ensure per-run workspace agent pod is running.
		var wsErr error
		wsAddr, wsErr = EnsureWorkspace(ctx, e.k8s, step.runID, step.orgID, step.wsToken, e.agentImage, e.jobNamespace)
		if wsErr != nil {
			log.Warn().Err(wsErr).Str("runID", step.runID).Msg("engine: workspace unavailable, falling back to S3 artifacts")
		}
	}

	// Build env vars.
	agentEnv := []corev1.EnvVar{
		{Name: "FLINT_TASK_TOKEN", Value: step.taskToken},
		{Name: "FLINT_SERVER_URL", Value: e.serverURL},
		{Name: "FLINT_RUN_ID", Value: step.runID},
		{Name: "FLINT_STEP_NAME", Value: step.name},
		{Name: "FLINT_ORG_ID", Value: step.orgID},
		{Name: "FLINT_PROJECT_ID", Value: step.projectID},
		{Name: "FLINT_ENVIRONMENT", Value: step.environment},
		{Name: "FLINT_WORKSPACE", Value: "/workspace"},
		{Name: "FLINT_GIT_REPO", Value: step.repo},
		{Name: "FLINT_GIT_REF", Value: step.ref},
		{Name: "FLINT_GIT_SHA", Value: step.commitSHA},
	}

	// Inject internal token for /internal endpoint auth.
	if e.internalToken != "" {
		agentEnv = append(agentEnv,
			corev1.EnvVar{Name: "FLINT_INTERNAL_TOKEN", Value: e.internalToken},
		)
	}

	// Object storage for artifacts, cache, and the S3 workspace mode. The
	// agent gates all three on FLINT_S3_BUCKET — with no bucket configured
	// they are skipped (workspace sync still works via the agent/PVC modes).
	if e.s3Bucket != "" {
		agentEnv = append(agentEnv,
			corev1.EnvVar{Name: "FLINT_S3_BUCKET", Value: e.s3Bucket},
			corev1.EnvVar{Name: "FLINT_S3_REGION", Value: e.s3Region},
		)
		if e.s3Endpoint != "" {
			// AWS_ENDPOINT_URL_S3 is honored by the SDK's default config
			// chain, so every S3 client in the agent (artifacts, cache,
			// workspace) picks it up without bespoke plumbing.
			agentEnv = append(agentEnv,
				corev1.EnvVar{Name: "AWS_ENDPOINT_URL_S3", Value: e.s3Endpoint},
			)
		}
	}

	// Per-step timeout for the sidecar's completion wait — mirrors the
	// engine-side timeout timer so the agent gives up (and reports failure)
	// at the same deadline the engine would.
	if stepDef.Timeout != "" {
		if d, err := time.ParseDuration(stepDef.Timeout); err == nil {
			agentEnv = append(agentEnv,
				corev1.EnvVar{Name: "FLINT_STEP_TIMEOUT", Value: d.String()},
			)
		}
	}

	// Inject workspace mode so the agent knows which sync backend to use.
	switch {
	case usePVC:
		agentEnv = append(agentEnv,
			corev1.EnvVar{Name: "FLINT_WS_MODE", Value: "pvc"},
		)
	case poolSpec.Workspace.Mode == runner.WorkspaceModeS3:
		agentEnv = append(agentEnv,
			corev1.EnvVar{Name: "FLINT_WS_MODE", Value: "s3"},
		)
		// Bucket/region for the S3 workspace come from the same storage
		// config injected above; the workspace uses a per-run key prefix.
	}

	// Inject workspace agent address when available.
	// The token is the run ID — shared across all steps in the run.
	if wsAddr != "" {
		agentEnv = append(agentEnv,
			corev1.EnvVar{Name: "FLINT_WS_ADDR", Value: wsAddr},
			corev1.EnvVar{Name: "FLINT_WS_TOKEN", Value: step.wsToken},
		)
	}

	// Secret mapping: tells agent which secrets to fetch and how to expose them.
	if len(step.secretMapping) > 0 {
		mappingJSON, _ := json.Marshal(step.secretMapping)
		agentEnv = append(agentEnv, corev1.EnvVar{
			Name: "FLINT_SECRET_MAPPING", Value: string(mappingJSON),
		})
	}

	// Pass step template inputs (with:) as env var for built-in templates
	// like use: checkout that read configuration from FLINT_CHECKOUT_INPUTS.
	if len(stepDef.With) > 0 {
		withJSON, _ := json.Marshal(stepDef.With)
		agentEnv = append(agentEnv,
			corev1.EnvVar{Name: "FLINT_CHECKOUT_INPUTS", Value: string(withJSON)},
		)
	}

	// Group steps: the init container copies the driver binary into the
	// workspace, and the driver (running as the step container) reads the
	// sub-step spec, job outputs, and needs context from its environment.
	if isGroup {
		agentEnv = append(agentEnv, corev1.EnvVar{Name: "FLINT_EXEC_TYPE", Value: "steps"})
	}

	userEnv := make([]corev1.EnvVar, 0, len(step.env)+8)
	for k, v := range step.env {
		userEnv = append(userEnv, corev1.EnvVar{Name: k, Value: v})
	}
	// $FLINT_OUTPUT is the canonical step-output channel: `echo "k=v" >>
	// "$FLINT_OUTPUT"`. The shell wrapper's emit() helper appends to the same
	// file; the sidecar reads it as the step result's outputs.
	userEnv = append(userEnv, corev1.EnvVar{Name: "FLINT_OUTPUT", Value: "/workspace/.flint-emit"})
	if isGroup {
		specJSON, err := json.Marshal(stepDef.Steps)
		if err != nil {
			return "", fmt.Errorf("engine: marshal sub-step spec: %w", err)
		}
		userEnv = append(userEnv,
			corev1.EnvVar{Name: "FLINT_STEPS_SPEC", Value: string(specJSON)},
			// Run/git context for in-pod expression evaluation — mirrors the
			// engine's git/run namespaces.
			corev1.EnvVar{Name: "FLINT_RUN_ID", Value: step.runID},
			corev1.EnvVar{Name: "FLINT_STEP_NAME", Value: step.name},
			corev1.EnvVar{Name: "FLINT_GIT_REPO", Value: step.repo},
			corev1.EnvVar{Name: "FLINT_GIT_REF", Value: step.ref},
			corev1.EnvVar{Name: "FLINT_GIT_SHA", Value: step.commitSHA},
			corev1.EnvVar{Name: "FLINT_TRIGGER_TYPE", Value: step.triggerType},
		)
		if len(stepDef.DeclaredOutputs) > 0 {
			outJSON, _ := json.Marshal(stepDef.DeclaredOutputs)
			userEnv = append(userEnv, corev1.EnvVar{Name: "FLINT_JOB_OUTPUTS", Value: string(outJSON)})
		}
		if len(step.needsOutputs) > 0 {
			needsJSON, _ := json.Marshal(step.needsOutputs)
			userEnv = append(userEnv, corev1.EnvVar{Name: "FLINT_NEEDS_OUTPUTS", Value: string(needsJSON)})
		}
	}

	// Labels for informer filtering.
	labels := map[string]string{
		"flint.dev/managed-by":  "flint",
		"flint.dev/org-id":      step.orgID,
		"flint.dev/run-id":      step.runID,
		"flint.dev/step-name":   step.name,
		"flint.dev/runner-pool": poolSpec.Name,
	}

	// Extract matrix key from expanded step name (e.g., "test[node=16]" → "node=16").
	matrixKey := extractMatrixKey(step.name)
	if matrixKey != "" {
		agentEnv = append(agentEnv, corev1.EnvVar{
			Name: "FLINT_MATRIX_KEY", Value: matrixKey,
		})
	}

	// Artifact config for agent.
	if len(stepDef.Inputs) > 0 {
		inputsJSON, _ := json.Marshal(stepDef.Inputs)
		agentEnv = append(agentEnv, corev1.EnvVar{
			Name: "FLINT_ARTIFACT_INPUTS", Value: string(inputsJSON),
		})
	}
	if len(stepDef.Outputs) > 0 {
		outputsJSON, _ := json.Marshal(stepDef.Outputs)
		agentEnv = append(agentEnv, corev1.EnvVar{
			Name: "FLINT_ARTIFACT_OUTPUTS", Value: string(outputsJSON),
		})
	}

	// Cache config for agent.
	if stepDef.Cache != nil {
		agentEnv = append(agentEnv, corev1.EnvVar{
			Name: "FLINT_CACHE_KEY", Value: stepDef.Cache.Key,
		})
		pathsJSON, _ := json.Marshal(stepDef.Cache.Paths)
		agentEnv = append(agentEnv, corev1.EnvVar{
			Name: "FLINT_CACHE_PATHS", Value: string(pathsJSON),
		})
		if len(stepDef.Cache.RestoreKeys) > 0 {
			rkJSON, _ := json.Marshal(stepDef.Cache.RestoreKeys)
			agentEnv = append(agentEnv, corev1.EnvVar{
				Name: "FLINT_CACHE_RESTORE_KEYS", Value: string(rkJSON),
			})
		}
	}

	jobName := fmt.Sprintf("flint-%s-%s", step.runID[:8], sanitizeK8sName(step.name))
	ttl := int32(3600)
	backoffLimit := int32(0)
	// ONE agent container per pod: a native sidecar (K8s >= 1.28, an init
	// container with restartPolicy Always) that prepares the workspace
	// (secrets, sync-in, artifacts, cache, driver install), then watches the
	// step (logs, sync-out, upload, completion). Its startup probe gates the
	// step container on the init-done marker, so preparation still strictly
	// precedes the step — with one fewer container and resource request than
	// the previous init + sidecar pair.
	sidecarRestart := corev1.ContainerRestartPolicyAlways

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: e.jobNamespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					InitContainers: []corev1.Container{
						{
							Name:          "flint-agent",
							Image:         e.agentImage,
							Command:       []string{"/flint-agent", "sidecar"},
							Env:           agentEnv,
							RestartPolicy: &sidecarRestart,
							VolumeMounts: []corev1.VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
							},
							// The step container starts only once workspace
							// preparation is done (init-done marker). Generous
							// threshold: cache restores can take minutes.
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"test", "-f", "/workspace/" + initDoneFile},
									},
								},
								PeriodSeconds:    2,
								FailureThreshold: 300,
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("96Mi"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:       "step",
							Image:      stepDef.Image,
							Command:    wrappedCmd,
							Env:        userEnv,
							WorkingDir: "/workspace",
							VolumeMounts: []corev1.VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
							},
							// Default resource limits — overridden by runner.MergeIntoJob below.
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("4Gi"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						workspaceVolume(usePVC, step.runID, poolSpec, stepDef.Disk),
					},
				},
			},
		},
	}

	// Add sidecar service containers.
	for _, svc := range stepDef.Services {
		svcEnv := make([]corev1.EnvVar, 0, len(svc.Env))
		for k, v := range svc.Env {
			svcEnv = append(svcEnv, corev1.EnvVar{Name: k, Value: v})
		}
		job.Spec.Template.Spec.Containers = append(job.Spec.Template.Spec.Containers, corev1.Container{
			Name:  "svc-" + sanitizeK8sName(svc.Name),
			Image: svc.Image,
			Env:   svcEnv,
		})
	}

	// Harden every container and the pod. Always drop ALL capabilities, forbid
	// privilege escalation, and apply the RuntimeDefault seccomp profile; only
	// force non-root when the pool opts in (build images often need root).
	job.Spec.Template.Spec.SecurityContext = podSecurityContext(poolSpec.RunAsNonRoot)
	for i := range job.Spec.Template.Spec.InitContainers {
		job.Spec.Template.Spec.InitContainers[i].SecurityContext = restrictedSecurityContext(poolSpec.RunAsNonRoot)
	}
	for i := range job.Spec.Template.Spec.Containers {
		job.Spec.Template.Spec.Containers[i].SecurityContext = restrictedSecurityContext(poolSpec.RunAsNonRoot)
	}

	// Apply runner pool scheduling (includes pool-level ServiceAccount).
	runner.MergeIntoJob(poolSpec, job)

	// Apply per-job resource requests/limits to the step container, overriding the
	// pool defaults (the pool sets the bounds; enforcement is a follow-up).
	applyStepResources(job, stepDef.Resources)

	// Override service account: step > pipeline > runner pool.
	// MergeIntoJob already set the runner pool SA; override if a more specific
	// scope is configured.
	if sa := resolveServiceAccount(stepDef.ServiceAccount, step.pipelineServiceAccount); sa != "" {
		job.Spec.Template.Spec.ServiceAccountName = sa
	}

	created, err := e.k8s.BatchV1().Jobs(e.jobNamespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("engine: create K8s Job: %w", err)
	}

	log.Info().
		Str("job", created.Name).
		Str("step", step.name).
		Str("image", stepDef.Image).
		Msg("engine: K8s Job created")

	return created.Name, nil
}

func boolPtr(b bool) *bool { return &b }

// restrictedSecurityContext is the always-on container hardening: drop every
// capability and forbid privilege escalation. runAsNonRoot is set only when the
// pool opts in — root-using build images would otherwise fail to start.
func restrictedSecurityContext(runAsNonRoot bool) *corev1.SecurityContext {
	sc := &corev1.SecurityContext{
		AllowPrivilegeEscalation: boolPtr(false),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
	if runAsNonRoot {
		sc.RunAsNonRoot = boolPtr(true)
	}
	return sc
}

// podSecurityContext applies the RuntimeDefault seccomp profile to the pod, plus
// pod-level runAsNonRoot when the pool opts in.
func podSecurityContext(runAsNonRoot bool) *corev1.PodSecurityContext {
	psc := &corev1.PodSecurityContext{
		SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
	if runAsNonRoot {
		psc.RunAsNonRoot = boolPtr(true)
	}
	return psc
}

// claimedStep holds the data needed to dispatch a step after claiming.
type claimedStep struct {
	id                     string
	workflowID             string
	name                   string
	execType               string
	taskToken              string
	wsToken                string // per-run workspace gRPC bearer token (HMAC-derived)
	stepDef                []byte
	runID                  string
	orgID                  string
	projectID              string
	repo                   string
	ref                    string
	commitSHA              string
	triggerType            string
	workspaceFlow          string            // ""/"sync" | "artifacts" | "none" (see StartWorkflowInput)
	environment            string            // target environment for secret scoping
	pipelineImage          string            // pipeline-level default image (fallback)
	pipelineServiceAccount string            // pipeline-level default K8s SA (fallback)
	env                    map[string]string // merged env vars (org env_vars + step.env + input.Env)
	secretMapping          map[string]string // env var name → secret store name (from step YAML secrets:)
	// needsOutputs carries the outputs of this step's direct dependencies
	// (base job name → outputs) for the in-pod needs.* expression context.
	needsOutputs map[string]map[string]string
}

// stepsDriverPath is where the sidecar places the flint-agent binary inside
// the shared workspace, so group steps can run the steps driver inside the
// user's image (the binary is static — CGO disabled — so it runs in any linux
// image of matching architecture).
const stepsDriverPath = "/workspace/.flint-bin/flint-agent"

// initDoneFile is the marker the sidecar writes when workspace preparation is
// complete — the step container's startup gate. Must match agent.InitDoneFile
// (not imported: the engine must not depend on the agent package).
const initDoneFile = ".flint-init-done"

// sanitizeK8sName converts a step name to a valid K8s DNS subdomain component.
// K8s names: lowercase, alphanumeric, hyphens only, max 63 chars.
//
// A 5-char xxh3 hash of the original name is appended as a suffix to prevent
// collisions when two different step names normalize to the same string
// (e.g. "my.build" and "my-build" would both become "my-build" without it).
func sanitizeK8sName(name string) string {
	// Compute hash before any normalization so it reflects the original name.
	hash := fmt.Sprintf("%05x", xxh3.HashString(name)&0xfffff)

	var b strings.Builder
	for _, c := range strings.ToLower(name) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else {
			b.WriteByte('-')
		}
	}
	s := b.String()

	// Trim leading/trailing hyphens and collapse runs.
	s = strings.Trim(s, "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}

	// Reserve 6 chars for "-HHHHH" suffix; cap sanitized part at 57 chars.
	if len(s) > 57 {
		s = strings.TrimRight(s[:57], "-")
	}
	return s + "-" + hash
}

// extractMatrixKey returns the matrix key portion from an expanded step name.
// "test[node=16,os=ubuntu]" → "node=16,os=ubuntu". Returns "" for non-matrix steps.
func extractMatrixKey(name string) string {
	start := strings.Index(name, "[")
	end := strings.LastIndex(name, "]")
	if start >= 0 && end > start {
		return name[start+1 : end]
	}
	return ""
}

// applyStepResources sets the step container's compute requests/limits from the
// job's resources. Quantities are parsed safely (invalid values are skipped, not
// fatal). The "step" container is the user container (the agent is a sidecar).
func applyStepResources(job *batchv1.Job, r *pipeline.StepResources) {
	if r == nil {
		return
	}
	for i := range job.Spec.Template.Spec.Containers {
		c := &job.Spec.Template.Spec.Containers[i]
		if c.Name != "step" {
			continue
		}
		if c.Resources.Requests == nil {
			c.Resources.Requests = corev1.ResourceList{}
		}
		if c.Resources.Limits == nil {
			c.Resources.Limits = corev1.ResourceList{}
		}
		setQuantity(c.Resources.Requests, corev1.ResourceCPU, r.CPU)
		setQuantity(c.Resources.Requests, corev1.ResourceMemory, r.Memory)
		if r.Limits != nil {
			setQuantity(c.Resources.Limits, corev1.ResourceCPU, r.Limits.CPU)
			setQuantity(c.Resources.Limits, corev1.ResourceMemory, r.Limits.Memory)
		}
		return
	}
}

func setQuantity(list corev1.ResourceList, name corev1.ResourceName, v string) {
	if v == "" {
		return
	}
	if q, err := resource.ParseQuantity(v); err == nil {
		list[name] = q
	}
}

// resolveServiceAccount returns the most specific service account override.
// Precedence: step > pipeline. Returns "" if neither is set (runner pool
// default from MergeIntoJob applies).
func resolveServiceAccount(stepSA, pipelineSA string) string {
	if stepSA != "" {
		return stepSA
	}
	return pipelineSA
}

func wrapStepCommand(userCommand string) []string {
	emitFn := `emit() { echo "$1=$2" >> /workspace/.flint-emit; }`
	wrapped := fmt.Sprintf(
		`%s; (%s) 2>&1 | tee /workspace/.flint-step.log; echo $? > /workspace/.flint-exit`,
		emitFn, userCommand,
	)
	return []string{"/bin/sh", "-c", wrapped}
}
