package runner_test

import (
	"testing"

	"github.com/NerdMeNot/flint/internal/flinterr"
	"github.com/NerdMeNot/flint/internal/runner"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func standardPool() runner.PoolSpec {
	return runner.PoolSpec{
		Name:        "standard",
		Description: "General purpose builds",
		Resources: runner.ResourceProfile{
			CPU:    resource.MustParse("2"),
			Memory: resource.MustParse("4Gi"),
		},
	}
}

func gpuPool() runner.PoolSpec {
	return runner.PoolSpec{
		Name:        "gpu",
		Description: "GPU workloads",
		Resources: runner.ResourceProfile{
			CPU:    resource.MustParse("8"),
			Memory: resource.MustParse("32Gi"),
			GPU:    &runner.GPURequest{Vendor: "nvidia", Model: "t4", Count: 1},
		},
		NodeSelector: map[string]string{
			"node.kubernetes.io/instance-type": "p3.2xlarge",
		},
		Tolerations: []corev1.Toleration{
			{Key: "nvidia.com/gpu", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
		},
	}
}

func TestRegistry_Resolve(t *testing.T) {
	reg := runner.NewRegistry()
	reg.Register(standardPool())
	reg.Register(gpuPool())

	spec, err := reg.Resolve("standard")
	if err != nil {
		t.Fatalf("Resolve(standard) error: %v", err)
	}
	if spec.Name != "standard" {
		t.Errorf("Name = %q", spec.Name)
	}

	spec, err = reg.Resolve("gpu")
	if err != nil {
		t.Fatalf("Resolve(gpu) error: %v", err)
	}
	if spec.Resources.GPU == nil {
		t.Error("gpu pool should have GPU resource")
	}
}

func TestRegistry_Resolve_Default(t *testing.T) {
	reg := runner.NewRegistry()
	reg.Register(standardPool())

	// Empty name should resolve to "standard".
	spec, err := reg.Resolve("")
	if err != nil {
		t.Fatalf("Resolve('') error: %v", err)
	}
	if spec.Name != "standard" {
		t.Errorf("Name = %q, want standard", spec.Name)
	}
}

func TestRegistry_Resolve_NotFound(t *testing.T) {
	reg := runner.NewRegistry()
	reg.Register(standardPool())

	_, err := reg.Resolve("gpu-h100")
	if err == nil {
		t.Fatal("expected error for unknown pool")
	}
	if !flinterr.IsNotFound(err) {
		t.Errorf("expected NotFound, got: %v", err)
	}
}

func TestRegistry_ResolveWithSize(t *testing.T) {
	reg := runner.NewRegistry()
	reg.Register(standardPool())

	spec, err := reg.ResolveWithSize("standard", runner.SizeXL)
	if err != nil {
		t.Fatalf("ResolveWithSize() error: %v", err)
	}

	// Size override should apply.
	if spec.Resources.CPU.String() != "8" {
		t.Errorf("CPU = %s, want 8", spec.Resources.CPU.String())
	}
	if spec.Resources.Memory.String() != "16Gi" {
		t.Errorf("Memory = %s, want 16Gi", spec.Resources.Memory.String())
	}
}

func TestRegistry_ResolveWithSize_InvalidSize(t *testing.T) {
	reg := runner.NewRegistry()
	reg.Register(standardPool())

	_, err := reg.ResolveWithSize("standard", "mega")
	if err == nil {
		t.Fatal("expected error for invalid size")
	}
}

func TestRegistry_List(t *testing.T) {
	reg := runner.NewRegistry()
	reg.Register(standardPool())
	reg.Register(gpuPool())

	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("len(List()) = %d, want 2", len(list))
	}
}

func TestMergeIntoJob(t *testing.T) {
	spec := gpuPool()

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job"},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "step", Image: "golang:1.23"},
					},
				},
			},
		},
	}

	runner.MergeIntoJob(&spec, job)

	container := job.Spec.Template.Spec.Containers[0]

	// CPU and memory.
	if cpu := container.Resources.Requests[corev1.ResourceCPU]; cpu.String() != "8" {
		t.Errorf("CPU request = %s, want 8", cpu.String())
	}
	if mem := container.Resources.Requests[corev1.ResourceMemory]; mem.String() != "32Gi" {
		t.Errorf("Memory request = %s, want 32Gi", mem.String())
	}

	// GPU.
	gpuRes := container.Resources.Requests[corev1.ResourceName("nvidia.com/gpu")]
	if gpuRes.String() != "1" {
		t.Errorf("GPU request = %s, want 1", gpuRes.String())
	}

	// Node selector.
	podSpec := job.Spec.Template.Spec
	if podSpec.NodeSelector["node.kubernetes.io/instance-type"] != "p3.2xlarge" {
		t.Errorf("NodeSelector = %v", podSpec.NodeSelector)
	}

	// Tolerations.
	if len(podSpec.Tolerations) != 1 {
		t.Fatalf("len(Tolerations) = %d, want 1", len(podSpec.Tolerations))
	}
	if podSpec.Tolerations[0].Key != "nvidia.com/gpu" {
		t.Errorf("Toleration key = %q", podSpec.Tolerations[0].Key)
	}
}

func TestTShirtSizes(t *testing.T) {
	for _, size := range runner.AllSizes() {
		if !runner.ValidSize(size) {
			t.Errorf("ValidSize(%q) = false", size)
		}

		profile, ok := runner.ResourcesForSize(size)
		if !ok {
			t.Errorf("ResourcesForSize(%q) not found", size)
			continue
		}

		if profile.CPU.IsZero() {
			t.Errorf("size %q has zero CPU", size)
		}
		if profile.Memory.IsZero() {
			t.Errorf("size %q has zero memory", size)
		}
	}
}

func TestMergeIntoJob_CustomResourceName(t *testing.T) {
	spec := runner.PoolSpec{
		Name: "tpu",
		Resources: runner.ResourceProfile{
			CPU:    resource.MustParse("16"),
			Memory: resource.MustParse("64Gi"),
			GPU: &runner.GPURequest{
				Vendor:       "google",
				Model:        "tpu-v5",
				Count:        4,
				ResourceName: "google.com/tpu",
			},
		},
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "tpu-job"},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "step", Image: "python:3.12"},
					},
				},
			},
		},
	}

	runner.MergeIntoJob(&spec, job)

	container := job.Spec.Template.Spec.Containers[0]

	// Should use custom resource name, not "google.com/gpu".
	tpuRes := container.Resources.Requests[corev1.ResourceName("google.com/tpu")]
	if tpuRes.String() != "4" {
		t.Errorf("TPU request = %s, want 4", tpuRes.String())
	}

	// Should NOT have "google.com/gpu".
	gpuRes := container.Resources.Requests[corev1.ResourceName("google.com/gpu")]
	if !gpuRes.IsZero() {
		t.Errorf("should not have google.com/gpu resource, got %s", gpuRes.String())
	}
}

func TestGPURequest_K8sResourceName(t *testing.T) {
	tests := []struct {
		name string
		gpu  runner.GPURequest
		want string
	}{
		{"nvidia default", runner.GPURequest{Vendor: "nvidia", Model: "t4"}, "nvidia.com/gpu"},
		{"amd default", runner.GPURequest{Vendor: "amd", Model: "mi300"}, "amd.com/gpu"},
		{"custom tpu", runner.GPURequest{Vendor: "google", Model: "tpu-v5", ResourceName: "google.com/tpu"}, "google.com/tpu"},
		{"custom neuron", runner.GPURequest{Vendor: "aws", Model: "inf2", ResourceName: "aws.amazon.com/neuron"}, "aws.amazon.com/neuron"},
		{"custom gaudi", runner.GPURequest{Vendor: "habana", Model: "gaudi2", ResourceName: "habana.ai/gaudi"}, "habana.ai/gaudi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.gpu.K8sResourceName(); got != tt.want {
				t.Errorf("K8sResourceName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTShirtSizes_Invalid(t *testing.T) {
	if runner.ValidSize("mega") {
		t.Error("ValidSize(mega) = true, want false")
	}
	_, ok := runner.ResourcesForSize("mega")
	if ok {
		t.Error("ResourcesForSize(mega) should return false")
	}
}
