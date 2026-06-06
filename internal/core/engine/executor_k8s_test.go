package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/NerdMeNot/flint/internal/core/runner"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// envMap flattens a container's env vars for easy assertions.
func envMap(vars []corev1.EnvVar) map[string]string {
	m := make(map[string]string, len(vars))
	for _, v := range vars {
		m[v.Name] = v.Value
	}
	return m
}

func containerByName(t *testing.T, cs []corev1.Container, name string) corev1.Container {
	t.Helper()
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("container %q not found", name)
	return corev1.Container{}
}

// TestK8sExecutor_Dispatch_BuildsJob exercises the Kubernetes dispatch path with
// a fake clientset — no cluster — and locks the produced Job spec so future
// executor refactors can't silently change it.
func TestK8sExecutor_Dispatch_BuildsJob(t *testing.T) {
	ctx := context.Background()
	k8s := fake.NewSimpleClientset()
	exec := NewK8sExecutor(k8s, runner.NewRegistry(), "flint-agent:v1", "flint-ns",
		"http://flint-server:5000", "internal-tok")

	def, err := json.Marshal(pipeline.Step{
		Name:  "build",
		Image: "golang:1.26",
		Run:   pipeline.Cmd("make build"),
	})
	require.NoError(t, err)

	step := claimedStep{
		name:      "build",
		execType:  "run",
		taskToken: "task-token",
		stepDef:   def,
		runID:     "run-abcdef12",
		orgID:     "org-1",
		projectID: "proj-1",
		repo:      "acme/app",
		ref:       "main",
		commitSHA: "deadbeef",
		env:       map[string]string{"FOO": "bar"},
	}

	handle, err := exec.Dispatch(ctx, step)
	require.NoError(t, err)
	require.NotEmpty(t, handle)

	job, err := k8s.BatchV1().Jobs("flint-ns").Get(ctx, handle, metav1.GetOptions{})
	require.NoError(t, err, "the Job should have been created under its handle")

	// Labels for informer correlation.
	assert.Equal(t, "flint", job.Labels["flint.dev/managed-by"])
	assert.Equal(t, "run-abcdef12", job.Labels["flint.dev/run-id"])
	assert.Equal(t, "build", job.Labels["flint.dev/step-name"])
	assert.Equal(t, "org-1", job.Labels["flint.dev/org-id"])

	// No automatic K8s retries — Flint handles retries in the DB.
	require.NotNil(t, job.Spec.BackoffLimit)
	assert.Equal(t, int32(0), *job.Spec.BackoffLimit)

	spec := job.Spec.Template.Spec

	// Init container runs `flint-agent init`.
	require.Len(t, spec.InitContainers, 1)
	initC := spec.InitContainers[0]
	assert.Equal(t, "flint-agent:v1", initC.Image)
	assert.Equal(t, []string{"/flint-agent", "init"}, initC.Command)

	// Step container: user image + wrapped command + user env, working dir /workspace.
	stepC := containerByName(t, spec.Containers, "step")
	assert.Equal(t, "golang:1.26", stepC.Image)
	assert.Equal(t, "/workspace", stepC.WorkingDir)
	assert.Equal(t, wrapStepCommand("make build"), stepC.Command)
	assert.Equal(t, "bar", envMap(stepC.Env)["FOO"])

	// Agent sidecar runs `flint-agent watch` and carries the task/git context.
	agentC := containerByName(t, spec.Containers, "flint-agent")
	assert.Equal(t, []string{"/flint-agent", "watch"}, agentC.Command)
	ae := envMap(agentC.Env)
	assert.Equal(t, "task-token", ae["FLINT_TASK_TOKEN"])
	assert.Equal(t, "http://flint-server:5000", ae["FLINT_SERVER_URL"])
	assert.Equal(t, "run-abcdef12", ae["FLINT_RUN_ID"])
	assert.Equal(t, "internal-tok", ae["FLINT_INTERNAL_TOKEN"])
	assert.Equal(t, "acme/app", ae["FLINT_GIT_REPO"])
	assert.Equal(t, "main", ae["FLINT_GIT_REF"])
	assert.Equal(t, "deadbeef", ae["FLINT_GIT_SHA"])

	// Workspace volume is mounted.
	var hasWorkspace bool
	for _, v := range spec.Volumes {
		if v.Name == "workspace" {
			hasWorkspace = true
		}
	}
	assert.True(t, hasWorkspace, "workspace volume should be present")

	// Agent mode (default pool) provisions a per-run workspace pod + service.
	_, err = k8s.CoreV1().Pods("flint-ns").Get(ctx, workspaceName("run-abcdef12"), metav1.GetOptions{})
	assert.NoError(t, err, "workspace agent pod should be created in agent mode")
}

// TestK8sExecutor_Dispatch_MissingImage fails fast when no image can be resolved.
func TestK8sExecutor_Dispatch_MissingImage(t *testing.T) {
	ctx := context.Background()
	exec := NewK8sExecutor(fake.NewSimpleClientset(), runner.NewRegistry(), "flint-agent:v1",
		"flint-ns", "http://flint-server:5000", "")

	def, _ := json.Marshal(pipeline.Step{Name: "noimg", Run: pipeline.Cmd("echo hi")})
	_, err := exec.Dispatch(ctx, claimedStep{
		name: "noimg", execType: "run", stepDef: def, runID: "run-abcdef12",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no container image")
}
