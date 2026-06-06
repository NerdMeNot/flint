package controller

import (
	"context"
	"encoding/json"
	"fmt"

	flintv1 "github.com/NerdMeNot/flint/internal/crd/v1"
	"github.com/NerdMeNot/flint/internal/db"
	"github.com/NerdMeNot/flint/internal/observe"
	"github.com/NerdMeNot/flint/internal/runner"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const runnerPoolFinalizer = "flint.dev/runnerpool-cleanup"

// RunnerPoolReconciler watches RunnerPool CRDs, persists them to the
// runner_pools table (for server/UI reads), and registers them in the
// in-memory runner.Registry (for fast worker lookups at Job creation time).
type RunnerPoolReconciler struct {
	client.Client
	K8sClient kubernetes.Interface
	Q         *db.Queries
	Registry  *runner.Registry
}

func (r *RunnerPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := observe.Logger(ctx)

	var pool flintv1.RunnerPool
	if err := r.Get(ctx, req.NamespacedName, &pool); err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Info().Str("pool", req.Name).Msg("runner pool deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching RunnerPool: %w", err)
	}

	// Handle deletion — remove from DB.
	if !pool.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&pool, runnerPoolFinalizer) {
			log.Info().Str("pool", pool.Name).Msg("runner pool being deleted")

			if err := r.Q.DeleteRunnerPool(ctx, pool.Name); err != nil {
				log.Error().Err(err).Msg("failed to delete runner pool from DB")
			}

			controllerutil.RemoveFinalizer(&pool, runnerPoolFinalizer)
			if err := r.Update(ctx, &pool); err != nil {
				return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer.
	if !controllerutil.ContainsFinalizer(&pool, runnerPoolFinalizer) {
		controllerutil.AddFinalizer(&pool, runnerPoolFinalizer)
		if err := r.Update(ctx, &pool); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
	}

	log.Info().Str("pool", pool.Name).Msg("reconciling runner pool")

	// Build internal PoolSpec.
	spec := runner.PoolSpec{
		Name:        pool.Name,
		Description: pool.Spec.Description,
		Resources: runner.ResourceProfile{
			CPU:    resource.MustParse(pool.Spec.Profile.CPU),
			Memory: resource.MustParse(pool.Spec.Profile.Memory),
		},
	}

	if pool.Spec.Profile.GPU != nil {
		spec.Resources.GPU = &runner.GPURequest{
			Vendor:       pool.Spec.Profile.GPU.Vendor,
			Model:        pool.Spec.Profile.GPU.Model,
			Count:        pool.Spec.Profile.GPU.Count,
			ResourceName: pool.Spec.Profile.GPU.ResourceName,
		}
	}

	if pool.Spec.Scheduling != nil {
		spec.NodeSelector = pool.Spec.Scheduling.NodeSelector
		spec.Tolerations = pool.Spec.Scheduling.Tolerations
	}

	if pool.Spec.Spot != nil {
		spec.Spot = pool.Spec.Spot.Preferred
	}

	spec.ServiceAccountName = pool.Spec.ServiceAccountName

	// Workspace configuration.
	if pool.Spec.Workspace != nil {
		switch pool.Spec.Workspace.Mode {
		case "pvc":
			if pool.Spec.Workspace.StorageClass == "" {
				r.setCondition(&pool, "Ready", metav1.ConditionFalse, "InvalidWorkspace",
					"workspace mode=pvc requires storageClass")
				_ = r.Status().Update(ctx, &pool)
				return ctrl.Result{}, fmt.Errorf("workspace mode=pvc requires storageClass")
			}
			if err := r.validateStorageClassRWX(ctx, pool.Spec.Workspace.StorageClass); err != nil {
				r.setCondition(&pool, "Ready", metav1.ConditionFalse, "InvalidStorageClass", err.Error())
				_ = r.Status().Update(ctx, &pool)
				return ctrl.Result{}, err
			}
			size := pool.Spec.Workspace.Size
			if size == "" {
				size = "10Gi"
			}
			spec.Workspace = runner.WorkspaceConfig{
				Mode:         runner.WorkspaceModePVC,
				StorageClass: pool.Spec.Workspace.StorageClass,
				Size:         size,
			}

		case "s3":
			if pool.Spec.Workspace.Bucket == "" {
				r.setCondition(&pool, "Ready", metav1.ConditionFalse, "InvalidWorkspace",
					"workspace mode=s3 requires bucket")
				_ = r.Status().Update(ctx, &pool)
				return ctrl.Result{}, fmt.Errorf("workspace mode=s3 requires bucket")
			}
			spec.Workspace = runner.WorkspaceConfig{
				Mode: runner.WorkspaceModeS3,
			}
		}
	}

	// Persist to Postgres (for server/UI reads).
	if err := r.upsertToDB(ctx, &pool); err != nil {
		log.Error().Err(err).Msg("failed to persist runner pool to DB")
		r.setCondition(&pool, "Ready", metav1.ConditionFalse, "SyncFailed", err.Error())
		_ = r.Status().Update(ctx, &pool)
		return ctrl.Result{}, fmt.Errorf("persisting runner pool: %w", err)
	}

	// Register in memory (for worker fast path).
	r.Registry.Register(spec)

	// Update status.
	r.setCondition(&pool, "Ready", metav1.ConditionTrue, "Registered", "Pool synced to database and registered")
	pool.Status.Ready = true

	if err := r.Status().Update(ctx, &pool); err != nil {
		log.Error().Err(err).Msg("failed to update RunnerPool status")
	}

	return ctrl.Result{}, nil
}

func (r *RunnerPoolReconciler) upsertToDB(ctx context.Context, pool *flintv1.RunnerPool) error {
	var gpuVendor, gpuModel *string
	var gpuCount pgtype.Int4
	if pool.Spec.Profile.GPU != nil {
		gpuVendor = &pool.Spec.Profile.GPU.Vendor
		gpuModel = &pool.Spec.Profile.GPU.Model
		gpuCount = pgtype.Int4{Int32: int32(pool.Spec.Profile.GPU.Count), Valid: true}
	}

	var nodeSelectorJSON, tolerationsJSON []byte
	if pool.Spec.Scheduling != nil {
		if pool.Spec.Scheduling.NodeSelector != nil {
			nodeSelectorJSON, _ = json.Marshal(pool.Spec.Scheduling.NodeSelector)
		}
		if pool.Spec.Scheduling.Tolerations != nil {
			tolerationsJSON, _ = json.Marshal(pool.Spec.Scheduling.Tolerations)
		}
	}

	spotPreferred := false
	spotFallback := "on-demand"
	if pool.Spec.Spot != nil {
		spotPreferred = pool.Spec.Spot.Preferred
		if pool.Spec.Spot.Fallback != "" {
			spotFallback = pool.Spec.Spot.Fallback
		}
	}

	arch := pool.Spec.Profile.Arch
	if arch == "" {
		arch = "amd64"
	}

	return r.Q.UpsertRunnerPool(ctx, db.UpsertRunnerPoolParams{
		Name:           pool.Name,
		Description:    &pool.Spec.Description,
		Cpu:            pool.Spec.Profile.CPU,
		Memory:         pool.Spec.Profile.Memory,
		GpuVendor:      gpuVendor,
		GpuModel:       gpuModel,
		GpuCount:       gpuCount,
		Arch:           arch,
		NodeSelector:   nodeSelectorJSON,
		Tolerations:    tolerationsJSON,
		SpotPreferred:  spotPreferred,
		SpotFallback:   spotFallback,
		DefaultTimeout: &pool.Spec.DefaultTimeout,
	})
}

func (r *RunnerPoolReconciler) setCondition(pool *flintv1.RunnerPool, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&pool.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

// rwxProvisioners lists storage class provisioners known to support
// ReadWriteMany. This is not exhaustive — unknown provisioners get a warning
// condition rather than a hard block, since custom CSI drivers may support RWX.
var rwxProvisioners = map[string]bool{
	"efs.csi.aws.com":                 true, // AWS EFS
	"fsx.csi.aws.com":                 true, // AWS FSx Lustre
	"file.csi.azure.com":              true, // Azure Files
	"filestore.csi.storage.gke.io":    true, // GCP Filestore
	"nfs.csi.k8s.io":                  true, // NFS CSI
	"cephfs.csi.ceph.com":             true, // CephFS
	"rook-ceph.cephfs.csi.ceph.com":   true, // Rook CephFS
	"cluster.local/nfs-provisioner":   true, // NFS subdir provisioner
	"nfs-subdir-external-provisioner": true,
	"lustre.csi.aws.com":              true, // FSx Lustre (alternate)
}

// rwoDenyList lists provisioners that definitely do NOT support ReadWriteMany.
// These get a hard rejection with a clear error message.
var rwoDenyList = map[string]string{
	"ebs.csi.aws.com":          "AWS EBS (gp2/gp3/io2) is ReadWriteOnce only — use efs.csi.aws.com for shared workspaces",
	"disk.csi.azure.com":       "Azure Managed Disks are ReadWriteOnce only — use file.csi.azure.com for shared workspaces",
	"pd.csi.storage.gke.io":    "GCP Persistent Disk is ReadWriteOnce only — use filestore.csi.storage.gke.io for shared workspaces",
	"kubernetes.io/aws-ebs":    "AWS EBS (legacy in-tree) is ReadWriteOnce only — use efs.csi.aws.com",
	"kubernetes.io/gce-pd":     "GCP PD (legacy in-tree) is ReadWriteOnce only — use filestore.csi.storage.gke.io",
	"kubernetes.io/azure-disk": "Azure Disk (legacy in-tree) is ReadWriteOnce only — use file.csi.azure.com",
}

// validateStorageClassRWX checks that the named StorageClass uses a provisioner
// known to support ReadWriteMany. Rejects known-RWO provisioners with a clear
// error. Warns on unknown provisioners (may still work).
func (r *RunnerPoolReconciler) validateStorageClassRWX(ctx context.Context, scName string) error {
	sc, err := r.K8sClient.StorageV1().StorageClasses().Get(ctx, scName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("storage class %q not found: %w", scName, err)
	}

	provisioner := sc.Provisioner

	// Hard reject known-RWO provisioners.
	if msg, blocked := rwoDenyList[provisioner]; blocked {
		return fmt.Errorf("storage class %q cannot be used for workspace PVC: %s", scName, msg)
	}

	// Known-good provisioners pass immediately.
	if rwxProvisioners[provisioner] {
		return nil
	}

	// Unknown provisioner — allow with warning.
	log.Warn().
		Str("storageClass", scName).
		Str("provisioner", provisioner).
		Msg("workspace: storage class provisioner not in known RWX list — it may not support ReadWriteMany")
	return nil
}

func (r *RunnerPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&flintv1.RunnerPool{}).
		Complete(r)
}
