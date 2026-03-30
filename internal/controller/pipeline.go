package controller

import (
	"context"
	"fmt"
	"time"

	flintv1 "github.com/NerdMeNot/flint/internal/crd/v1"
	"github.com/NerdMeNot/flint/internal/db"
	"github.com/NerdMeNot/flint/internal/observe"
	"github.com/NerdMeNot/flint/pkg/forge"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const pipelineFinalizer = "flint.dev/pipeline-cleanup"

// PipelineReconciler watches Pipeline CRDs and syncs them to the projects table.
type PipelineReconciler struct {
	client.Client
	Q         *db.Queries
	Forge     forge.ForgeProvider // for webhook creation/deletion
	ServerURL string             // webhook target URL (e.g., "https://flint.example.com/webhooks/github")
}

func (r *PipelineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := observe.Logger(ctx)

	var pipeline flintv1.Pipeline
	if err := r.Get(ctx, req.NamespacedName, &pipeline); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching Pipeline: %w", err)
	}

	// Handle deletion — clean up forge webhook, archive project.
	if !pipeline.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&pipeline, pipelineFinalizer) {
			log.Info().Str("repo", pipeline.Spec.Repo).Msg("pipeline being deleted, cleaning up")

			if err := r.archiveProject(ctx, pipeline.Status.ProjectID); err != nil {
				log.Error().Err(err).Msg("failed to archive project during cleanup")
			}

			// Delete forge webhook.
			if r.Forge != nil && pipeline.Status.WebhookID != "" {
				if err := r.Forge.DeleteWebhook(ctx, pipeline.Spec.Repo, pipeline.Status.WebhookID); err != nil {
					log.Error().Err(err).Str("webhookID", pipeline.Status.WebhookID).Msg("failed to delete forge webhook")
				}
			}

			controllerutil.RemoveFinalizer(&pipeline, pipelineFinalizer)
			if err := r.Update(ctx, &pipeline); err != nil {
				return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present.
	if !controllerutil.ContainsFinalizer(&pipeline, pipelineFinalizer) {
		controllerutil.AddFinalizer(&pipeline, pipelineFinalizer)
		if err := r.Update(ctx, &pipeline); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
	}

	log.Info().Str("repo", pipeline.Spec.Repo).Msg("reconciling pipeline")

	// Upsert project in database.
	projectID, err := r.upsertProject(ctx, &pipeline)
	if err != nil {
		meta.SetStatusCondition(&pipeline.Status.Conditions, metav1.Condition{
			Type:               "Registered",
			Status:             metav1.ConditionFalse,
			Reason:             "SyncFailed",
			Message:            err.Error(),
			LastTransitionTime: metav1.Now(),
		})
		_ = r.Status().Update(ctx, &pipeline)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, fmt.Errorf("upserting project: %w", err)
	}

	// Create forge webhook if not already configured.
	if r.Forge != nil && r.ServerURL != "" && pipeline.Status.WebhookID == "" {
		webhookURL := fmt.Sprintf("%s/webhooks/%s", r.ServerURL, r.Forge.Type())

		// Read webhook secret from the forge connection.
		webhookSecret, _ := r.Q.GetWebhookSecretByName(ctx, pipeline.Spec.ForgeRef)

		if webhookSecret != "" {
			whID, whErr := r.Forge.CreateWebhook(ctx, pipeline.Spec.Repo, webhookURL, webhookSecret,
				[]string{"push", "pull_request"})
			if whErr != nil {
				log.Error().Err(whErr).Str("repo", pipeline.Spec.Repo).Msg("failed to create forge webhook")
			} else {
				pipeline.Status.WebhookID = whID
				log.Info().Str("webhookID", whID).Str("repo", pipeline.Spec.Repo).Msg("forge webhook created")
			}
		}
	}

	// Update status.
	now := metav1.Now()
	pipeline.Status.ProjectID = projectID
	pipeline.Status.LastSyncTime = &now
	meta.SetStatusCondition(&pipeline.Status.Conditions, metav1.Condition{
		Type:               "Registered",
		Status:             metav1.ConditionTrue,
		Reason:             "Synced",
		Message:            "Project synced to database",
		LastTransitionTime: now,
	})

	if err := r.Status().Update(ctx, &pipeline); err != nil {
		log.Error().Err(err).Msg("failed to update Pipeline status")
	}

	return ctrl.Result{}, nil
}

func (r *PipelineReconciler) upsertProject(ctx context.Context, p *flintv1.Pipeline) (string, error) {
	pipelineSource := `{"type":"self","path":".flint/"}`
	if p.Spec.PipelineSource != nil {
		if p.Spec.PipelineSource.Type == "external" {
			pipelineSource = fmt.Sprintf(`{"type":"external","repo":"%s","ref":"%s","path":"%s"}`,
				p.Spec.PipelineSource.Repo, p.Spec.PipelineSource.Ref, p.Spec.PipelineSource.Path)
		} else if p.Spec.PipelineSource.Path != "" {
			pipelineSource = fmt.Sprintf(`{"type":"self","path":"%s"}`, p.Spec.PipelineSource.Path)
		}
	}

	colour := p.Spec.Colour
	if colour == "" {
		colour = "#6366f1"
	}

	defaultBranch := p.Spec.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	return r.Q.UpsertProject(ctx, db.UpsertProjectParams{
		RepoPath:       p.Spec.Repo,
		RepoUrl:        fmt.Sprintf("https://github.com/%s", p.Spec.Repo),
		DisplayName:    nilIfEmpty(p.Spec.DisplayName),
		Description:    nilIfEmpty(p.Spec.Description),
		Colour:         colour,
		Icon:           nilIfEmpty(p.Spec.Icon),
		Tags:           p.Spec.Tags,
		DefaultBranch:  defaultBranch,
		PipelineSource: []byte(pipelineSource),
		ForgeRef:       p.Spec.ForgeRef,
	})
}

func (r *PipelineReconciler) archiveProject(ctx context.Context, projectID string) error {
	if projectID == "" {
		return nil
	}
	return r.Q.ArchiveProject(ctx, projectID)
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (r *PipelineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&flintv1.Pipeline{}).
		Complete(r)
}
