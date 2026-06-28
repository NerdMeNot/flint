package render

import (
	"strings"
	"testing"

	"github.com/NerdMeNot/flint/internal/core/runner"
)

func prof() runner.ProvisioningProfile {
	return runner.ProvisioningProfile{
		Role:                  "KarpenterNodeRole",
		SubnetSelector:        map[string]string{"karpenter.sh/discovery": "flint"},
		SecurityGroupSelector: map[string]string{"karpenter.sh/discovery": "flint"},
		AMIFamily:             "AL2023",
	}
}

func TestRender_ArchAny_AllowsBothArchitectures(t *testing.T) {
	m, err := Render(Input{Name: "multi", Arch: "any", Prov: prof()})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(m.NodePool, "amd64") || !strings.Contains(m.NodePool, "arm64") {
		t.Errorf("expected both archs in NodePool requirements:\n%s", m.NodePool)
	}
}

func TestRender_ConcreteArch_ConstrainsToOne(t *testing.T) {
	m, err := Render(Input{Name: "arm", Arch: "arm64", Prov: prof()})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(m.NodePool, "amd64") {
		t.Errorf("arm64 pool should not allow amd64:\n%s", m.NodePool)
	}
	if !strings.Contains(m.NodePool, "arm64") {
		t.Errorf("expected arm64 in requirements:\n%s", m.NodePool)
	}
}
