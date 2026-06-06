package v1_test

import (
	"testing"

	v1 "github.com/NerdMeNot/flint/internal/core/crd/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestSchemeRegistration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error: %v", err)
	}

	// Verify all types are registered.
	gvk := v1.SchemeGroupVersion.WithKind("StepTemplate")
	obj, err := scheme.New(gvk)
	if err != nil {
		t.Fatalf("scheme.New(StepTemplate) error: %v", err)
	}
	if _, ok := obj.(*v1.StepTemplate); !ok {
		t.Errorf("expected *StepTemplate, got %T", obj)
	}

	gvk = v1.SchemeGroupVersion.WithKind("Project")
	obj, err = scheme.New(gvk)
	if err != nil {
		t.Fatalf("scheme.New(Project) error: %v", err)
	}
	if _, ok := obj.(*v1.Project); !ok {
		t.Errorf("expected *Project, got %T", obj)
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
	p := &v1.Project{
		Spec: v1.ProjectSpec{
			Repo:           "acme/svc",
			Tags:           []string{"a", "b"},
			PipelineSource: &v1.PipelineSourceSpec{Type: "self", Path: ".flint/"},
		},
	}

	cp := p.DeepCopyObject().(*v1.Project)

	// Modify original — copy should be unaffected (slice + pointer independence).
	p.Spec.Tags[0] = "changed"
	p.Spec.PipelineSource.Path = "changed"

	if cp.Spec.Tags[0] != "a" {
		t.Error("DeepCopy Tags slice is not independent")
	}
	if cp.Spec.PipelineSource.Path != ".flint/" {
		t.Error("DeepCopy PipelineSource pointer is not independent")
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
