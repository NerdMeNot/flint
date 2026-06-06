// Package informer runs a K8s SharedInformerFactory that watches Jobs and PVCs
// created by Flint. When a Job completes, fails, or is deleted, the informer
// calls the engine to deliver step-result signals. Safety net for when the
// agent crashes without calling /internal/complete.
package informer

import (
	"context"
	"fmt"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Config configures the informer.
type Config struct {
	Namespace     string
	LabelSelector string
}

func (c *Config) labelSelector() string {
	if c.LabelSelector != "" {
		return c.LabelSelector
	}
	return "flint.dev/managed-by=flint"
}

// Watcher runs informers that watch Flint-managed K8s resources.
type Watcher struct {
	client kubernetes.Interface
	engine engine.Engine
	pool   *pgxpool.Pool
	config Config
}

// New creates a new informer Watcher.
func New(client kubernetes.Interface, eng engine.Engine, pool *pgxpool.Pool, cfg Config) *Watcher {
	return &Watcher{client: client, engine: eng, pool: pool, config: cfg}
}

// Run starts the informers and blocks until the context is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	if w.client == nil {
		log.Warn().Msg("informer: K8s client is nil, skipping")
		<-ctx.Done()
		return nil
	}

	log.Info().
		Str("namespace", w.config.Namespace).
		Str("labelSelector", w.config.labelSelector()).
		Msg("starting K8s informers")

	factory := informers.NewSharedInformerFactoryWithOptions(
		w.client,
		0,
		informers.WithNamespace(w.config.Namespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = w.config.labelSelector()
		}),
	)

	jobInformer := factory.Batch().V1().Jobs().Informer()
	jobInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: w.onJobUpdate,
		DeleteFunc: w.onJobDelete,
	})

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())
	log.Info().Msg("informer cache synced")

	<-ctx.Done()
	log.Info().Msg("K8s informers stopped")
	return nil
}

func (w *Watcher) onJobUpdate(oldObj, newObj any) {
	oldJob, ok := oldObj.(*batchv1.Job)
	if !ok {
		return
	}
	newJob, ok := newObj.(*batchv1.Job)
	if !ok {
		return
	}

	if isJobComplete(newJob) && !isJobComplete(oldJob) {
		w.handleJobEvent(newJob, true, "")
	} else if isJobFailed(newJob) && !isJobFailed(oldJob) {
		w.handleJobEvent(newJob, false, jobFailureReason(newJob))
	}
}

func (w *Watcher) onJobDelete(obj any) {
	job, ok := obj.(*batchv1.Job)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		job, ok = tombstone.Obj.(*batchv1.Job)
		if !ok {
			return
		}
	}

	if !isJobComplete(job) && !isJobFailed(job) {
		w.handleJobEvent(job, false, "job deleted externally")
	}
}

func (w *Watcher) handleJobEvent(job *batchv1.Job, success bool, reason string) {
	runID := job.Labels["flint.dev/run-id"]
	stepName := job.Labels["flint.dev/step-name"]
	if runID == "" || stepName == "" {
		return
	}
	if w.engine == nil || w.pool == nil {
		return
	}

	// Resolve workflowID from runID via the pipeline_runs table.
	ctx := context.Background()
	q := db.New(w.pool)
	wfID, err := q.GetRunWorkflowID(ctx, runID)
	if err != nil || wfID == nil {
		log.Warn().Err(err).Str("runID", runID).Msg("informer: cannot resolve workflow for run")
		return
	}

	if success {
		HandleJobCompleted(ctx, w.engine, *wfID, stepName, runID)
	} else {
		HandleJobFailed(ctx, w.engine, *wfID, stepName, runID, reason)
	}
}

func isJobComplete(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func isJobFailed(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func jobFailureReason(job *batchv1.Job) string {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			if c.Reason != "" {
				return fmt.Sprintf("%s: %s", c.Reason, c.Message)
			}
			return c.Message
		}
	}
	return ""
}
