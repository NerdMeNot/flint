package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
}

// NewK8sExecutor builds the Kubernetes Job executor.
func NewK8sExecutor(k8s kubernetes.Interface, reg *runner.Registry, agentImage, jobNamespace, serverURL, internalToken string) *k8sExecutor {
	return &k8sExecutor{
		k8s:           k8s,
		reg:           reg,
		agentImage:    agentImage,
		jobNamespace:  jobNamespace,
		serverURL:     serverURL,
		internalToken: internalToken,
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
	if stepDef.Image == "" && stepDef.ExecType() != "steps" {
		return "", fmt.Errorf("engine: step %q has no container image (set image: on the step or at pipeline level)", step.name)
	}

	// Resolve runner pool.
	poolName := "standard"
	if stepDef.Runner != "" {
		poolName = stepDef.Runner
	}
	poolSpec, err := e.reg.Resolve(poolName)
	if err != nil {
		log.Warn().Str("pool", poolName).Msg("engine: runner pool not found, using defaults")
		poolSpec = &runner.PoolSpec{
			Name: "default",
			Resources: runner.ResourceProfile{
				CPU:    resource.MustParse("1"),
				Memory: resource.MustParse("2Gi"),
			},
		}
	}

	// Build the command.
	command := stepDef.Run.String()
	wrappedCmd := wrapStepCommand(command)

	// Workspace setup depends on the runner pool's workspace mode.
	// PVC mode: all pods mount the same PVC — no workspace agent needed.
	// Agent mode (default): emptyDir per pod + gRPC workspace agent for sync.
	usePVC := poolSpec.Workspace.Mode == runner.WorkspaceModePVC
	var wsAddr string

	if usePVC {
		// Ensure the per-run PVC exists. Idempotent.
		if err := ensureWorkspacePVC(ctx, e.k8s, step.runID, poolSpec, e.jobNamespace); err != nil {
			return "", fmt.Errorf("engine: create workspace PVC: %w", err)
		}
	} else {
		// Agent mode: ensure per-run workspace agent pod is running.
		var wsErr error
		wsAddr, wsErr = EnsureWorkspace(ctx, e.k8s, step.runID, step.orgID, e.agentImage, e.jobNamespace)
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
		// S3 bucket/region are injected from the pool config so the agent
		// can construct the S3FS with the correct per-run prefix.
		// Note: FLINT_S3_BUCKET/REGION may already be set for cache; the
		// workspace uses the same bucket with a different key prefix.
	}

	// Inject workspace agent address when available.
	// The token is the run ID — shared across all steps in the run.
	if wsAddr != "" {
		agentEnv = append(agentEnv,
			corev1.EnvVar{Name: "FLINT_WS_ADDR", Value: wsAddr},
			corev1.EnvVar{Name: "FLINT_WS_TOKEN", Value: step.runID},
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

	userEnv := make([]corev1.EnvVar, 0, len(step.env))
	for k, v := range step.env {
		userEnv = append(userEnv, corev1.EnvVar{Name: k, Value: v})
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
	}

	jobName := fmt.Sprintf("flint-%s-%s", step.runID[:8], sanitizeK8sName(step.name))
	ttl := int32(3600)
	backoffLimit := int32(0)

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
							Name:    "flint-init",
							Image:   e.agentImage,
							Command: []string{"/flint-agent", "init"},
							Env:     agentEnv,
							VolumeMounts: []corev1.VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
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
						{
							Name:    "flint-agent",
							Image:   e.agentImage,
							Command: []string{"/flint-agent", "watch"},
							Env:     agentEnv,
							VolumeMounts: []corev1.VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						workspaceVolume(usePVC, step.runID, poolSpec),
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

	// Apply runner pool scheduling (includes pool-level ServiceAccount).
	runner.MergeIntoJob(poolSpec, job)

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

// claimedStep holds the data needed to dispatch a step after claiming.
type claimedStep struct {
	id                     string
	workflowID             string
	name                   string
	execType               string
	taskToken              string
	stepDef                []byte
	runID                  string
	orgID                  string
	projectID              string
	repo                   string
	ref                    string
	commitSHA              string
	environment            string            // target environment for secret scoping
	pipelineImage          string            // pipeline-level default image (fallback)
	pipelineServiceAccount string            // pipeline-level default K8s SA (fallback)
	env                    map[string]string // merged env vars (org env_vars + step.env + input.Env)
	secretMapping          map[string]string // env var name → secret store name (from step YAML secrets:)
}

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
