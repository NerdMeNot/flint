package controller

import (
	"context"
	"encoding/json"
	"fmt"

	flintv1 "github.com/NerdMeNot/flint/internal/crd/v1"
	"github.com/NerdMeNot/flint/internal/db"
	"github.com/NerdMeNot/flint/internal/observe"
	"github.com/NerdMeNot/flint/pkg/secret"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const forgeFinalizer = "flint.dev/forge-cleanup"

// ForgeConnectionReconciler watches ForgeConnection CRDs, reads credentials
// from referenced K8s Secrets, encrypts them, and stores in forge_connections table.
type ForgeConnectionReconciler struct {
	client.Client
	Q         *db.Queries
	MasterKey []byte // 32-byte AES key for envelope encryption
}

func (r *ForgeConnectionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := observe.Logger(ctx)

	var fc flintv1.ForgeConnection
	if err := r.Get(ctx, req.NamespacedName, &fc); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching ForgeConnection: %w", err)
	}

	// Handle deletion.
	if !fc.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&fc, forgeFinalizer) {
			log.Info().Str("name", fc.Name).Msg("forge connection being deleted")

			if fc.Status.ConnectionID != "" {
				if err := r.Q.DeleteForgeConnectionByID(ctx, fc.Status.ConnectionID); err != nil {
					log.Error().Err(err).Msg("failed to delete forge connection from DB")
				}
			}

			controllerutil.RemoveFinalizer(&fc, forgeFinalizer)
			if err := r.Update(ctx, &fc); err != nil {
				return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer.
	if !controllerutil.ContainsFinalizer(&fc, forgeFinalizer) {
		controllerutil.AddFinalizer(&fc, forgeFinalizer)
		if err := r.Update(ctx, &fc); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
	}

	log.Info().Str("name", fc.Name).Str("type", fc.Spec.Type).Msg("reconciling forge connection")

	// Read credentials from K8s Secrets.
	creds, webhookSecret, err := r.readCredentials(ctx, &fc)
	if err != nil {
		r.setCondition(&fc, "Ready", metav1.ConditionFalse, "CredentialReadFailed", err.Error())
		_ = r.Status().Update(ctx, &fc)
		return ctrl.Result{}, fmt.Errorf("reading credentials: %w", err)
	}

	// Encrypt credentials.
	encryptedCreds, err := secret.Encrypt(creds, r.MasterKey, 1)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("encrypting credentials: %w", err)
	}

	// Upsert to database.
	displayName := fc.Spec.DisplayName
	if displayName == "" {
		displayName = fc.Name
	}

	connID, err := r.Q.InsertForgeConnection(ctx, db.InsertForgeConnectionParams{
		ForgeType:      fc.Spec.Type,
		DisplayName:    displayName,
		WebhookSecret:  webhookSecret,
		CredentialsEnc: encryptedCreds,
	})

	if err != nil {
		// Try update if insert fails (e.g., connection already exists).
		connID, err = r.Q.UpdateForgeConnectionByName(ctx, db.UpdateForgeConnectionByNameParams{
			ForgeType:      fc.Spec.Type,
			DisplayName:    displayName,
			WebhookSecret:  webhookSecret,
			CredentialsEnc: encryptedCreds,
			DisplayName_2:  displayName,
		})
		if err != nil {
			r.setCondition(&fc, "Ready", metav1.ConditionFalse, "SyncFailed", err.Error())
			_ = r.Status().Update(ctx, &fc)
			return ctrl.Result{}, fmt.Errorf("upserting forge connection: %w", err)
		}
	}

	// Update status.
	now := metav1.Now()
	fc.Status.ConnectionID = connID
	fc.Status.LastVerifiedAt = &now
	r.setCondition(&fc, "Ready", metav1.ConditionTrue, "Synced", "Credentials synced to database")

	if err := r.Status().Update(ctx, &fc); err != nil {
		log.Error().Err(err).Msg("failed to update ForgeConnection status")
	}

	return ctrl.Result{}, nil
}

func (r *ForgeConnectionReconciler) readCredentials(ctx context.Context, fc *flintv1.ForgeConnection) (creds []byte, webhookSecret string, err error) {
	switch fc.Spec.Type {
	case "github":
		if fc.Spec.GitHub == nil {
			return nil, "", fmt.Errorf("github config required when type is github")
		}

		privateKey, err := r.readSecretKey(ctx, fc.Namespace, fc.Spec.GitHub.PrivateKeyRef)
		if err != nil {
			return nil, "", fmt.Errorf("reading private key: %w", err)
		}

		webhookSecretVal, err := r.readSecretKey(ctx, fc.Namespace, fc.Spec.GitHub.WebhookSecretRef)
		if err != nil {
			return nil, "", fmt.Errorf("reading webhook secret: %w", err)
		}

		creds, _ = json.Marshal(map[string]string{
			"appId":          fc.Spec.GitHub.AppID,
			"installationId": fc.Spec.GitHub.InstallationID,
			"privateKey":     privateKey,
		})

		return creds, webhookSecretVal, nil

	case "gitlab":
		if fc.Spec.GitLab == nil {
			return nil, "", fmt.Errorf("gitlab config required when type is gitlab")
		}
		oauthCreds, err := r.readSecret(ctx, fc.Namespace, fc.Spec.GitLab.CredentialsRef.Name)
		if err != nil {
			return nil, "", err
		}
		creds, _ = json.Marshal(oauthCreds)
		return creds, "", nil

	case "bitbucket":
		if fc.Spec.Bitbucket == nil {
			return nil, "", fmt.Errorf("bitbucket config required when type is bitbucket")
		}
		oauthCreds, err := r.readSecret(ctx, fc.Namespace, fc.Spec.Bitbucket.CredentialsRef.Name)
		if err != nil {
			return nil, "", err
		}
		creds, _ = json.Marshal(oauthCreds)
		return creds, "", nil

	default:
		return nil, "", fmt.Errorf("unsupported forge type: %s", fc.Spec.Type)
	}
}

func (r *ForgeConnectionReconciler) readSecretKey(ctx context.Context, namespace string, ref flintv1.SecretKeyRef) (string, error) {
	var k8sSecret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, &k8sSecret); err != nil {
		return "", fmt.Errorf("k8s secret %q not found: %w", ref.Name, err)
	}

	val, ok := k8sSecret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("key %q not found in k8s secret %q", ref.Key, ref.Name)
	}

	return string(val), nil
}

func (r *ForgeConnectionReconciler) readSecret(ctx context.Context, namespace, name string) (map[string]string, error) {
	var k8sSecret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &k8sSecret); err != nil {
		return nil, fmt.Errorf("k8s secret %q not found: %w", name, err)
	}

	result := make(map[string]string, len(k8sSecret.Data))
	for k, v := range k8sSecret.Data {
		result[k] = string(v)
	}
	return result, nil
}

func (r *ForgeConnectionReconciler) setCondition(fc *flintv1.ForgeConnection, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&fc.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

func (r *ForgeConnectionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&flintv1.ForgeConnection{}).
		Complete(r)
}
