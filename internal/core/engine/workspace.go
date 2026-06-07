package engine

import (
	"context"
	"fmt"

	"github.com/NerdMeNot/flint/internal/core/runner"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

const (
	// workspacePort is the gRPC port the workspace agent listens on.
	workspacePort = 7700

	// workspaceRoot is the directory inside the workspace pod that holds run
	// data. It is backed by an emptyDir volume.
	workspaceRoot = "/workspace"

	// workspaceActiveDeadline is the maximum lifetime of a workspace pod.
	// It acts as a safety net in case TeardownWorkspace is never called
	// (e.g., worker crash). 4 hours comfortably covers the longest pipelines.
	workspaceActiveDeadline = int64(4 * 60 * 60)

	// workspaceTerminationGracePeriod gives in-flight RPCs time to drain
	// before the kernel SIGKILL fires.
	workspaceTerminationGracePeriod = int64(30)
)

// WorkspaceAddr returns the stable in-cluster DNS address of the workspace
// agent pod for the given run. This address is injected into step pods as
// FLINT_WS_ADDR.
func WorkspaceAddr(runID, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d",
		workspaceName(runID), namespace, workspacePort)
}

// EnsureWorkspace creates the workspace Pod and Service for runID if they do
// not already exist. It is idempotent and safe to call on every step dispatch.
// Returns the workspace gRPC address on success.
func EnsureWorkspace(
	ctx context.Context,
	k8s kubernetes.Interface,
	runID, orgID, agentImage, namespace string,
) (string, error) {
	name := workspaceName(runID)
	addr := WorkspaceAddr(runID, namespace)

	// Check whether the pod already exists. If so, we're done.
	_, err := k8s.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return addr, nil
	}
	if !k8serrors.IsNotFound(err) {
		return "", fmt.Errorf("engine: check workspace pod %q: %w", name, err)
	}

	// Create the Service first so that its ClusterIP is stable when the pod
	// starts and when step pods begin dialling.
	if err := createWorkspaceService(ctx, k8s, name, runID, orgID, namespace); err != nil {
		return "", err
	}

	// Create the Pod.
	if err := createWorkspacePod(ctx, k8s, name, runID, orgID, agentImage, namespace); err != nil {
		// Best-effort Service cleanup to avoid orphans.
		_ = k8s.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
		return "", err
	}

	log.Info().
		Str("name", name).
		Str("runID", runID).
		Str("addr", addr).
		Msg("engine: workspace created")

	return addr, nil
}

// TeardownWorkspace deletes all workspace resources for runID — agent pod +
// service (agent mode) and/or PVC (pvc mode). Idempotent; not-found is ignored.
func TeardownWorkspace(ctx context.Context, k8s kubernetes.Interface, runID, namespace string) error {
	name := workspaceName(runID)
	propagation := metav1.DeletePropagationBackground

	// Agent mode resources (pod + service).
	podErr := k8s.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
	svcErr := k8s.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})

	if podErr != nil && !k8serrors.IsNotFound(podErr) {
		return fmt.Errorf("engine: delete workspace pod %q: %w", name, podErr)
	}
	if svcErr != nil && !k8serrors.IsNotFound(svcErr) {
		return fmt.Errorf("engine: delete workspace service %q: %w", name, svcErr)
	}

	// PVC mode resource (shared volume).
	pvcName := workspacePVCName(runID)
	pvcErr := k8s.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, pvcName, metav1.DeleteOptions{})
	if pvcErr != nil && !k8serrors.IsNotFound(pvcErr) {
		return fmt.Errorf("engine: delete workspace PVC %q: %w", pvcName, pvcErr)
	}

	log.Info().Str("name", name).Str("runID", runID).Msg("engine: workspace torn down")
	return nil
}

// workspaceName derives a deterministic, K8s-safe name for the workspace
// resources from the run ID.
func workspaceName(runID string) string {
	id := runID
	if len(id) > 12 {
		id = id[:12]
	}
	return "flint-ws-" + sanitizeK8sName(id)
}

// createWorkspaceService creates a ClusterIP Service that routes to the
// workspace pod. The Service is created before the pod so that its virtual IP
// is immediately resolvable via cluster DNS.
func createWorkspaceService(
	ctx context.Context,
	k8s kubernetes.Interface,
	name, runID, orgID, namespace string,
) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    workspaceLabels(runID, orgID),
			Annotations: map[string]string{
				"flint.dev/run-id": runID,
			},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: workspaceLabels(runID, orgID),
			Ports: []corev1.ServicePort{
				{
					Name:       "grpc",
					Port:       workspacePort,
					TargetPort: intstr.FromInt(workspacePort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	_, err := k8s.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("engine: create workspace service %q: %w", name, err)
	}
	return nil
}

// createWorkspacePod creates the workspace agent pod. The pod runs
// `flint-agent workspace` backed by an emptyDir volume.
func createWorkspacePod(
	ctx context.Context,
	k8s kubernetes.Interface,
	name, runID, orgID, agentImage, namespace string,
) error {
	activeDeadline := workspaceActiveDeadline
	gracePeriod := workspaceTerminationGracePeriod
	port := int32(workspacePort)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    workspaceLabels(runID, orgID),
			Annotations: map[string]string{
				"flint.dev/run-id": runID,
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			ActiveDeadlineSeconds:         &activeDeadline,
			TerminationGracePeriodSeconds: &gracePeriod,
			Containers: []corev1.Container{
				{
					Name:    "workspace",
					Image:   agentImage,
					Command: []string{"/flint-agent", "workspace"},
					Env: []corev1.EnvVar{
						{Name: "FLINT_RUN_ID", Value: runID},
						{Name: "FLINT_WS_TOKEN", Value: runID}, // token == runID for scoped isolation
						{Name: "FLINT_WS_PORT", Value: fmt.Sprintf("%d", workspacePort)},
						{Name: "FLINT_WS_ROOT", Value: workspaceRoot},
					},
					Ports: []corev1.ContainerPort{
						{Name: "grpc", ContainerPort: port, Protocol: corev1.ProtocolTCP},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "workspace", MountPath: workspaceRoot},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
					// Readiness probe: verify the gRPC port is accepting connections.
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{
								Port: intstr.FromInt(workspacePort),
							},
						},
						InitialDelaySeconds: 2,
						PeriodSeconds:       5,
						FailureThreshold:    6,
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
	}

	_, err := k8s.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("engine: create workspace pod %q: %w", name, err)
	}
	return nil
}

// deleteRunJobs deletes all K8s Jobs for a given runID using the standard label
// selector. This is used during cancellation to stop running containers, and
// during normal cleanup to remove jobs that haven't been TTL-cleaned yet.
// Not-found and empty-list results are not errors.
func deleteRunJobs(ctx context.Context, k8s kubernetes.Interface, runID, namespace string) error {
	propagation := metav1.DeletePropagationBackground
	return k8s.BatchV1().Jobs(namespace).DeleteCollection(ctx,
		metav1.DeleteOptions{PropagationPolicy: &propagation},
		metav1.ListOptions{LabelSelector: "flint.dev/run-id=" + runID},
	)
}

// ─────────────────────────────────────────────────────────────
// PVC workspace mode
// ─────────────────────────────────────────────────────────────

// ensureWorkspacePVC creates a ReadWriteMany PVC for the run if it doesn't
// already exist. All step pods mount this PVC — no workspace agent needed.
func ensureWorkspacePVC(
	ctx context.Context,
	k8s kubernetes.Interface,
	runID string,
	pool *runner.PoolSpec,
	namespace string,
) error {
	name := workspacePVCName(runID)
	sc := pool.Workspace.StorageClass
	size := pool.Workspace.Size
	if size == "" {
		size = "10Gi"
	}
	qty := resource.MustParse(size)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"flint.dev/managed-by": "flint",
				"flint.dev/component":  "workspace",
				"flint.dev/run-id":     runID,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			StorageClassName: &sc,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: qty,
				},
			},
		},
	}

	_, err := k8s.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("engine: create workspace PVC %q: %w", name, err)
	}
	return nil
}

// workspacePVCName derives the PVC name from the run ID.
func workspacePVCName(runID string) string {
	id := runID
	if len(id) > 12 {
		id = id[:12]
	}
	return "flint-ws-" + sanitizeK8sName(id)
}

// workspaceVolume returns the appropriate Volume spec for the workspace.
func workspaceVolume(usePVC bool, runID string, pool *runner.PoolSpec, disk string) corev1.Volume {
	if usePVC {
		return corev1.Volume{
			Name: "workspace",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: workspacePVCName(runID),
				},
			},
		}
	}
	// Per-job scratch: a node-backed emptyDir sized to the job's `disk:` (if set).
	// The scheduler accounts for it via the step container's ephemeral-storage
	// request (set in dispatch). A sized emptyDir is the infra-light default;
	// block-backed generic ephemeral volumes are a future opt-in.
	emptyDir := &corev1.EmptyDirVolumeSource{}
	if disk != "" {
		if q, err := resource.ParseQuantity(disk); err == nil {
			emptyDir.SizeLimit = &q
		}
	}
	return corev1.Volume{
		Name:         "workspace",
		VolumeSource: corev1.VolumeSource{EmptyDir: emptyDir},
	}
}

// workspaceLabels returns the standard label set for workspace resources.
func workspaceLabels(runID, orgID string) map[string]string {
	return map[string]string{
		"flint.dev/managed-by": "flint",
		"flint.dev/component":  "workspace",
		"flint.dev/run-id":     runID,
		"flint.dev/org-id":     orgID,
	}
}
