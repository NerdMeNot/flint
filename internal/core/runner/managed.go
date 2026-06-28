package runner

import corev1 "k8s.io/api/core/v1"

// Pool isolation labels/taints. A managed pool's Karpenter NodePool labels its
// nodes with PoolLabel=<name> and taints them CITaintKey=<name>:NoSchedule, so
// only Flint step pods for that pool land there (and the pool can scale to zero
// cleanly). Flint auto-sets the matching nodeSelector + toleration on the pool,
// so pipeline authors never see this.
const (
	PoolLabel  = "flint.dev/pool"
	CITaintKey = "flint.dev/ci"
	// ArchLabel is the well-known node label a reference pool pins to its arch so
	// pods schedule onto matching-arch nodes. Derived from the pool's arch at load
	// (SpecFromRow) and stored on save by the editor.
	ArchLabel = "kubernetes.io/arch"
)

// ManagedSpec is the intent for a managed pool — what Karpenter may provision.
// Stored as managed_spec jsonb. The dev-facing profile (cpu/memory/gpu/arch) is
// kept in the pool's normal columns.
type ManagedSpec struct {
	// CapacityType: "spot" (spot-only), "on-demand", or "spot-preferred"
	// (spot with on-demand fallback — the default for cost-efficient CI).
	CapacityType string `json:"capacityType,omitempty"`
	// InstanceFamilies optionally restricts Karpenter to these families
	// (e.g. ["c", "m", "r"]); empty lets Karpenter choose any that fits.
	InstanceFamilies []string `json:"instanceFamilies,omitempty"`
	// CPULimit / GPULimit are pool-wide caps → Karpenter NodePool.spec.limits.
	CPULimit int `json:"cpuLimit,omitempty"`
	GPULimit int `json:"gpuLimit,omitempty"`
	// ConsolidateAfter is how long an empty node lingers before removal.
	ConsolidateAfter string `json:"consolidateAfter,omitempty"`
	// ScaleToZero (default true) → consolidationPolicy WhenEmpty.
	ScaleToZero bool `json:"scaleToZero"`
	// DiskGiB is the node root volume size (EC2NodeClass blockDeviceMappings).
	DiskGiB int `json:"diskGiB,omitempty"`
	// AMIFamily overrides the provisioning-profile default AMI family.
	AMIFamily string `json:"amiFamily,omitempty"`
}

// WithDefaults fills CI-friendly defaults: spot-preferred, aggressive scale-to-zero.
func (m ManagedSpec) WithDefaults() ManagedSpec {
	if m.CapacityType == "" {
		m.CapacityType = "spot-preferred"
	}
	if m.ConsolidateAfter == "" {
		m.ConsolidateAfter = "30s"
	}
	if m.DiskGiB == 0 {
		m.DiskGiB = 50
	}
	return m
}

// ProvisioningProfile is the account-level infra a Karpenter EC2NodeClass needs.
// Provided once by the platform team (config), not per pool.
type ProvisioningProfile struct {
	Role                  string
	SubnetSelector        map[string]string
	SecurityGroupSelector map[string]string
	AMIFamily             string
}

// IsConfigured reports whether enough is set to render a NodeClass.
func (p ProvisioningProfile) IsConfigured() bool {
	return p.Role != "" && len(p.SubnetSelector) > 0 && len(p.SecurityGroupSelector) > 0
}

// PoolNodeSelector / PoolToleration are the scheduling fragments a managed pool
// stores so the engine targets its nodes — identical in shape to a reference
// pool, so dispatch never branches on mode.
func PoolNodeSelector(name string) map[string]string {
	return map[string]string{PoolLabel: name}
}

func PoolToleration(name string) corev1.Toleration {
	return corev1.Toleration{
		Key:      CITaintKey,
		Operator: corev1.TolerationOpEqual,
		Value:    name,
		Effect:   corev1.TaintEffectNoSchedule,
	}
}
