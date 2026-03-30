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

const authProviderFinalizer = "flint.dev/auth-cleanup"

// AuthProviderReconciler watches AuthProvider CRDs, reads credentials
// from referenced K8s Secrets, validates the provider config, encrypts
// it, and stores it in the auth_provider_config table.
type AuthProviderReconciler struct {
	client.Client
	Q         *db.Queries
	MasterKey []byte // 32-byte AES key for envelope encryption
}

func (r *AuthProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := observe.Logger(ctx)

	var ap flintv1.AuthProvider
	if err := r.Get(ctx, req.NamespacedName, &ap); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching AuthProvider: %w", err)
	}

	// Handle deletion.
	if !ap.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&ap, authProviderFinalizer) {
			log.Info().Str("name", ap.Name).Msg("auth provider being deleted")

			if ap.Status.ProviderID != "" {
				if _, err := r.Q.DeleteAuthProviderConfig(ctx, ap.Spec.Type); err != nil {
					log.Error().Err(err).Msg("failed to delete auth provider config from DB")
				}
			}

			controllerutil.RemoveFinalizer(&ap, authProviderFinalizer)
			if err := r.Update(ctx, &ap); err != nil {
				return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer.
	if !controllerutil.ContainsFinalizer(&ap, authProviderFinalizer) {
		controllerutil.AddFinalizer(&ap, authProviderFinalizer)
		if err := r.Update(ctx, &ap); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
	}

	log.Info().Str("name", ap.Name).Str("type", ap.Spec.Type).Msg("reconciling auth provider")

	// Read credentials from K8s Secrets.
	configJSON, err := r.buildConfigJSON(ctx, &ap)
	if err != nil {
		r.setCondition(&ap, "Ready", metav1.ConditionFalse, "CredentialReadFailed", err.Error())
		_ = r.Status().Update(ctx, &ap)
		return ctrl.Result{}, fmt.Errorf("reading credentials: %w", err)
	}

	// Encrypt the config.
	encryptedConfig, err := secret.Encrypt(configJSON, r.MasterKey, 1)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("encrypting config: %w", err)
	}

	// Upsert to database.
	displayName := ap.Name
	providerID, err := r.Q.UpsertAuthProviderConfig(ctx, db.UpsertAuthProviderConfigParams{
		ProviderType: ap.Spec.Type,
		DisplayName:  displayName,
		ConfigEnc:    encryptedConfig,
	})
	if err != nil {
		r.setCondition(&ap, "Ready", metav1.ConditionFalse, "SyncFailed", err.Error())
		_ = r.Status().Update(ctx, &ap)
		return ctrl.Result{}, fmt.Errorf("upserting auth provider config: %w", err)
	}

	// Update status.
	now := metav1.Now()
	ap.Status.ProviderID = providerID
	ap.Status.LastVerifiedAt = &now
	r.setCondition(&ap, "Ready", metav1.ConditionTrue, "Synced", "Auth provider config synced to database")

	if err := r.Status().Update(ctx, &ap); err != nil {
		log.Error().Err(err).Msg("failed to update AuthProvider status")
	}

	return ctrl.Result{}, nil
}

func (r *AuthProviderReconciler) buildConfigJSON(ctx context.Context, ap *flintv1.AuthProvider) ([]byte, error) {
	switch ap.Spec.Type {
	case "oidc":
		if ap.Spec.OIDC == nil {
			return nil, fmt.Errorf("oidc config required when type is oidc")
		}

		clientSecret, err := r.readSecretKey(ctx, ap.Namespace, ap.Spec.OIDC.ClientSecretRef)
		if err != nil {
			return nil, fmt.Errorf("reading client secret: %w", err)
		}

		config := map[string]any{
			"issuerUrl":    ap.Spec.OIDC.IssuerURL,
			"clientId":     ap.Spec.OIDC.ClientID,
			"clientSecret": clientSecret,
		}
		if len(ap.Spec.OIDC.Scopes) > 0 {
			config["scopes"] = ap.Spec.OIDC.Scopes
		}

		return json.Marshal(config)

	case "saml":
		if ap.Spec.SAML == nil {
			return nil, fmt.Errorf("saml config required when type is saml")
		}

		config := map[string]any{
			"metadataUrl": ap.Spec.SAML.MetadataURL,
			"entityId":    ap.Spec.SAML.EntityID,
		}

		if ap.Spec.SAML.CertificateRef != nil {
			cert, err := r.readSecretKey(ctx, ap.Namespace, *ap.Spec.SAML.CertificateRef)
			if err != nil {
				return nil, fmt.Errorf("reading SP certificate: %w", err)
			}
			config["certPEM"] = cert
		}

		if ap.Spec.SAML.PrivateKeyRef != nil {
			key, err := r.readSecretKey(ctx, ap.Namespace, *ap.Spec.SAML.PrivateKeyRef)
			if err != nil {
				return nil, fmt.Errorf("reading SP private key: %w", err)
			}
			config["keyPEM"] = key
		}

		return json.Marshal(config)

	default:
		return nil, fmt.Errorf("unsupported auth provider type: %s", ap.Spec.Type)
	}
}

func (r *AuthProviderReconciler) readSecretKey(ctx context.Context, namespace string, ref flintv1.SecretKeyRef) (string, error) {
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

func (r *AuthProviderReconciler) setCondition(ap *flintv1.AuthProvider, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&ap.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

func (r *AuthProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&flintv1.AuthProvider{}).
		Complete(r)
}
