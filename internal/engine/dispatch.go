package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NerdMeNot/flint/internal/runner"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/rs/zerolog/log"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// dispatchStep handles a newly claimed step based on its exec type.
func dispatchStep(ctx context.Context, k8s kubernetes.Interface, reg *runner.Registry,
	step claimedStep, agentImage, jobNamespace, serverURL string) error {

	switch step.execType {
	case "run", "use", "steps":
		return dispatchRunStep(ctx, k8s, reg, step, agentImage, jobNamespace, serverURL)
	case "gate":
		// Gate steps transition to "waiting" — no K8s Job needed.
		// The timer (gate_timeout) was already created by the loop.
		log.Info().Str("step", step.name).Msg("engine: gate step waiting for approval")
		return nil
	default:
		return fmt.Errorf("engine: unknown step type %q", step.execType)
	}
}

// dispatchRunStep creates a K8s Job for a run/use/steps step.
func dispatchRunStep(ctx context.Context, k8s kubernetes.Interface, reg *runner.Registry,
	step claimedStep, agentImage, jobNamespace, serverURL string) error {

	var stepDef pipeline.Step
	if err := json.Unmarshal(step.stepDef, &stepDef); err != nil {
		return fmt.Errorf("engine: unmarshal step def: %w", err)
	}

	// Resolve runner pool.
	poolName := "standard"
	if stepDef.Runner != "" {
		poolName = stepDef.Runner
	}
	poolSpec, err := reg.Resolve(poolName)
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
	command := stepDef.Run
	wrappedCmd := wrapStepCommand(command)

	// Build env vars.
	agentEnv := []corev1.EnvVar{
		{Name: "FLINT_TASK_TOKEN", Value: step.taskToken},
		{Name: "FLINT_SERVER_URL", Value: serverURL},
		{Name: "FLINT_RUN_ID", Value: step.runID},
		{Name: "FLINT_STEP_NAME", Value: step.name},
		{Name: "FLINT_ORG_ID", Value: step.orgID},
		{Name: "FLINT_WORKSPACE", Value: "/workspace"},
		{Name: "FLINT_GIT_REPO", Value: step.repo},
		{Name: "FLINT_GIT_REF", Value: step.ref},
		{Name: "FLINT_GIT_SHA", Value: step.commitSHA},
	}

	userEnv := make([]corev1.EnvVar, 0, len(step.env))
	for k, v := range step.env {
		userEnv = append(userEnv, corev1.EnvVar{Name: k, Value: v})
	}

	// Labels for informer filtering.
	labels := map[string]string{
		"flint.dev/managed-by": "flint",
		"flint.dev/org-id":     step.orgID,
		"flint.dev/run-id":     step.runID,
		"flint.dev/step-name":  step.name,
		"flint.dev/runner-pool": poolSpec.Name,
	}

	jobName := fmt.Sprintf("flint-%s-%s", step.runID[:8], step.name)
	ttl := int32(3600)
	backoffLimit := int32(0)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: jobNamespace,
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
							Image:   agentImage,
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
							Image:   agentImage,
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
						{
							Name: "workspace",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	// Apply runner pool scheduling.
	runner.MergeIntoJob(poolSpec, job)

	created, err := k8s.BatchV1().Jobs(jobNamespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("engine: create K8s Job: %w", err)
	}

	log.Info().
		Str("job", created.Name).
		Str("step", step.name).
		Str("image", stepDef.Image).
		Msg("engine: K8s Job created")

	return nil
}

// claimedStep holds the data needed to dispatch a step after claiming.
type claimedStep struct {
	id         string
	workflowID string
	name       string
	execType   string
	taskToken  string
	stepDef    []byte
	runID      string
	orgID      string
	repo       string
	ref        string
	commitSHA  string
	env        map[string]string
}

func wrapStepCommand(userCommand string) []string {
	emitFn := `emit() { echo "$1=$2" >> /workspace/.flint-emit; }`
	wrapped := fmt.Sprintf(
		`%s; (%s) 2>&1 | tee /workspace/.flint-step.log; echo $? > /workspace/.flint-exit`,
		emitFn, userCommand,
	)
	return []string{"/bin/sh", "-c", wrapped}
}
