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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Q        *db.Queries
	Registry *runner.Registry
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

func (r *RunnerPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&flintv1.RunnerPool{}).
		Complete(r)
}
