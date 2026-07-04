package runner_test

import (
	"testing"

	"github.com/NerdMeNot/flint/internal/core/flinterr"
	"github.com/NerdMeNot/flint/internal/core/runner"
)

func standardPool() runner.PoolSpec {
	return runner.PoolSpec{
		Name:        "standard",
		Description: "General purpose builds",
		Provider:    "static",
		Arch:        "amd64",
		Resources: runner.ResourceProfile{
			CPUMillis: 2000,
			MemoryMB:  4096,
		},
	}
}

func gpuPool() runner.PoolSpec {
	return runner.PoolSpec{
		Name:        "gpu",
		Description: "GPU workloads",
		Provider:    "aws-us-east-1",
		Arch:        "amd64",
		Resources: runner.ResourceProfile{
			CPUMillis: 8000,
			MemoryMB:  32768,
			GPU:       &runner.GPURequest{Vendor: "nvidia", Model: "t4", Count: 1},
		},
		InstanceTypes: []string{"g4dn.2xlarge"},
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
	if spec.Resources.CPUMillis != 8000 {
		t.Errorf("CPUMillis = %d, want 8000", spec.Resources.CPUMillis)
	}
	if spec.Resources.MemoryMB != 16384 {
		t.Errorf("MemoryMB = %d, want 16384", spec.Resources.MemoryMB)
	}
}

func TestRegistry_ResolveWithSize_DoesNotMutateRegistry(t *testing.T) {
	reg := runner.NewRegistry()
	reg.Register(standardPool())

	if _, err := reg.ResolveWithSize("standard", runner.SizeXL); err != nil {
		t.Fatalf("ResolveWithSize() error: %v", err)
	}

	spec, err := reg.Resolve("standard")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if spec.Resources.CPUMillis != 2000 {
		t.Errorf("registry pool mutated by size override: CPUMillis = %d, want 2000", spec.Resources.CPUMillis)
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

func TestRegistry_ReplaceAll(t *testing.T) {
	reg := runner.NewRegistry()
	reg.Register(standardPool())
	reg.Register(gpuPool())

	reg.ReplaceAll([]runner.PoolSpec{standardPool()})

	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("len(List()) = %d after ReplaceAll, want 1", len(list))
	}
	if _, err := reg.Resolve("gpu"); err == nil {
		t.Error("gpu pool should be gone after ReplaceAll")
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

		if profile.CPUMillis == 0 {
			t.Errorf("size %q has zero CPU", size)
		}
		if profile.MemoryMB == 0 {
			t.Errorf("size %q has zero memory", size)
		}
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

func TestParseResources(t *testing.T) {
	p, err := runner.ParseResources("500m", "2Gi")
	if err != nil {
		t.Fatalf("ParseResources error: %v", err)
	}
	if p.CPUMillis != 500 || p.MemoryMB != 2048 {
		t.Errorf("got %+v, want {500 2048}", p)
	}

	// Blank strings mean "unset" — zero fields, no error.
	p, err = runner.ParseResources("", "")
	if err != nil {
		t.Fatalf("ParseResources blank error: %v", err)
	}
	if p.CPUMillis != 0 || p.MemoryMB != 0 {
		t.Errorf("blank quantities should be zero, got %+v", p)
	}

	if _, err := runner.ParseResources("nope", ""); err == nil {
		t.Error("expected error for invalid cpu")
	}
}
