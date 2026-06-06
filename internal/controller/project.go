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

// ProjectReconciler watches Project CRDs and syncs them to the projects table.
type ProjectReconciler struct {
	client.Client
	Q         *db.Queries
	Forge     forge.ForgeProvider // for webhook creation/deletion
	ServerURL string              // webhook target URL (e.g., "https://flint.example.com/webhooks/github")
}

func (r *ProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := observe.Logger(ctx)

	var project flintv1.Project
	if err := r.Get(ctx, req.NamespacedName, &project); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching Project: %w", err)
	}

	// Handle deletion — clean up forge webhook, archive project.
	if !project.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&project, pipelineFinalizer) {
			log.Info().Str("repo", project.Spec.Repo).Msg("pipeline being deleted, cleaning up")

			if err := r.archiveProject(ctx, project.Status.ProjectID); err != nil {
				log.Error().Err(err).Msg("failed to archive project during cleanup")
			}

			// Delete forge webhook.
			if r.Forge != nil && project.Status.WebhookID != "" {
				if err := r.Forge.DeleteWebhook(ctx, project.Spec.Repo, project.Status.WebhookID); err != nil {
					log.Error().Err(err).Str("webhookID", project.Status.WebhookID).Msg("failed to delete forge webhook")
				}
			}

			controllerutil.RemoveFinalizer(&project, pipelineFinalizer)
			if err := r.Update(ctx, &project); err != nil {
				return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present.
	if !controllerutil.ContainsFinalizer(&project, pipelineFinalizer) {
		controllerutil.AddFinalizer(&project, pipelineFinalizer)
		if err := r.Update(ctx, &project); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
	}

	log.Info().Str("repo", project.Spec.Repo).Msg("reconciling pipeline")

	// Upsert project in database.
	projectID, err := r.upsertProject(ctx, &project)
	if err != nil {
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               "Registered",
			Status:             metav1.ConditionFalse,
			Reason:             "SyncFailed",
			Message:            err.Error(),
			LastTransitionTime: metav1.Now(),
		})
		_ = r.Status().Update(ctx, &project)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, fmt.Errorf("upserting project: %w", err)
	}

	// Create forge webhook if not already configured.
	if r.Forge != nil && r.ServerURL != "" && project.Status.WebhookID == "" {
		webhookURL := fmt.Sprintf("%s/webhooks/%s", r.ServerURL, r.Forge.Type())

		// Read webhook secret from the forge connection.
		webhookSecret, _ := r.Q.GetWebhookSecretByName(ctx, project.Spec.ForgeRef)

		if webhookSecret != "" {
			whID, whErr := r.Forge.CreateWebhook(ctx, project.Spec.Repo, webhookURL, webhookSecret,
				[]string{"push", "pull_request"})
			if whErr != nil {
				log.Error().Err(whErr).Str("repo", project.Spec.Repo).Msg("failed to create forge webhook")
			} else {
				project.Status.WebhookID = whID
				log.Info().Str("webhookID", whID).Str("repo", project.Spec.Repo).Msg("forge webhook created")
			}
		}
	}

	// Update status.
	now := metav1.Now()
	project.Status.ProjectID = projectID
	project.Status.LastSyncTime = &now
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               "Registered",
		Status:             metav1.ConditionTrue,
		Reason:             "Synced",
		Message:            "Project synced to database",
		LastTransitionTime: now,
	})

	if err := r.Status().Update(ctx, &project); err != nil {
		log.Error().Err(err).Msg("failed to update Project status")
	}

	return ctrl.Result{}, nil
}

func (r *ProjectReconciler) upsertProject(ctx context.Context, p *flintv1.Project) (string, error) {
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

func (r *ProjectReconciler) archiveProject(ctx context.Context, projectID string) error {
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

func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&flintv1.Project{}).
		Complete(r)
}
