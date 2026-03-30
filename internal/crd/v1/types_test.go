package v1_test

import (
	"testing"

	v1 "github.com/NerdMeNot/flint/internal/crd/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestSchemeRegistration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error: %v", err)
	}

	// Verify all types are registered.
	gvk := v1.SchemeGroupVersion.WithKind("ForgeConnection")
	obj, err := scheme.New(gvk)
	if err != nil {
		t.Fatalf("scheme.New(ForgeConnection) error: %v", err)
	}
	if _, ok := obj.(*v1.ForgeConnection); !ok {
		t.Errorf("expected *ForgeConnection, got %T", obj)
	}

	gvk = v1.SchemeGroupVersion.WithKind("Pipeline")
	obj, err = scheme.New(gvk)
	if err != nil {
		t.Fatalf("scheme.New(Pipeline) error: %v", err)
	}
	if _, ok := obj.(*v1.Pipeline); !ok {
		t.Errorf("expected *Pipeline, got %T", obj)
	}

	gvk = v1.SchemeGroupVersion.WithKind("RunnerPool")
	obj, err = scheme.New(gvk)
	if err != nil {
		t.Fatalf("scheme.New(RunnerPool) error: %v", err)
	}
	if _, ok := obj.(*v1.RunnerPool); !ok {
		t.Errorf("expected *RunnerPool, got %T", obj)
	}
}

func TestDeepCopy(t *testing.T) {
	fc := &v1.ForgeConnection{
		Spec: v1.ForgeConnectionSpec{
			Type: "github",
			GitHub: &v1.GitHubConnectionSpec{
				AppID:          "123",
				InstallationID: "456",
				PrivateKeyRef:  v1.SecretKeyRef{Name: "secret", Key: "key"},
				WebhookSecretRef: v1.SecretKeyRef{Name: "secret", Key: "webhook"},
			},
		},
	}

	copy := fc.DeepCopyObject().(*v1.ForgeConnection)

	// Modify original — copy should be unaffected.
	fc.Spec.GitHub.AppID = "changed"

	if copy.Spec.GitHub.AppID != "123" {
		t.Error("DeepCopy is not independent — modifying original affected copy")
	}
}

func TestRunnerPoolDeepCopy(t *testing.T) {
	rp := &v1.RunnerPool{
		Spec: v1.RunnerPoolSpec{
			Description: "GPU pool",
			Profile: v1.ResourceProfileSpec{
				CPU:    "8",
				Memory: "32Gi",
				GPU:    &v1.GPUSpec{Vendor: "nvidia", Model: "t4", Count: 1},
			},
			Scheduling: &v1.SchedulingSpec{
				NodeSelector: map[string]string{"instance-type": "p3.2xlarge"},
			},
		},
	}

	copy := rp.DeepCopyObject().(*v1.RunnerPool)

	rp.Spec.Scheduling.NodeSelector["instance-type"] = "changed"

	if copy.Spec.Scheduling.NodeSelector["instance-type"] != "p3.2xlarge" {
		t.Error("DeepCopy is not independent for RunnerPool")
	}
}
