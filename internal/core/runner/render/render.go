// Package render turns a managed runner pool's intent into Karpenter manifests
// (NodePool + EC2NodeClass) for the platform's GitOps to apply — Flint is the
// single source of truth for the pool, render-to-GitOps means Flint never writes
// to the cluster directly. AWS/EC2NodeClass first; Azure (AKSNodeClass) is a swap
// of renderNodeClass behind the same Input/Manifests seam.
package render

import (
	"fmt"
	"strconv"

	"github.com/NerdMeNot/flint/internal/core/runner"
	"gopkg.in/yaml.v3"
)

// Input is everything needed to render a managed pool's manifests.
type Input struct {
	Name    string
	Arch    string // amd64 | arm64 (defaults amd64)
	GPU     *runner.GPURequest
	Managed runner.ManagedSpec
	Prov    runner.ProvisioningProfile
}

// Manifests are the two YAML documents the platform applies.
type Manifests struct {
	NodePool  string `json:"nodePool"`
	NodeClass string `json:"nodeClass"`
}

// Combined returns both manifests as one multi-document YAML string.
func (m Manifests) Combined() string {
	return m.NodePool + "---\n" + m.NodeClass
}

// Render produces the Karpenter NodePool + EC2NodeClass for a managed pool.
func Render(in Input) (Manifests, error) {
	if !in.Prov.IsConfigured() {
		return Manifests{}, fmt.Errorf("render: provisioning profile not configured (need role + subnet + security-group selectors)")
	}
	ms := in.Managed.WithDefaults()
	// "any" (or empty) lets Karpenter provision either architecture; a concrete
	// arch constrains the NodePool to it.
	archValues := []string{in.Arch}
	if in.Arch == "" || in.Arch == "any" {
		archValues = []string{"amd64", "arm64"}
	}
	nodeClassName := "flint-" + in.Name

	np, err := yamlDoc(buildNodePool(in.Name, archValues, in.GPU, ms, nodeClassName))
	if err != nil {
		return Manifests{}, err
	}
	nc, err := yamlDoc(buildNodeClass(nodeClassName, ms, in.Prov))
	if err != nil {
		return Manifests{}, err
	}
	return Manifests{NodePool: np, NodeClass: nc}, nil
}

func buildNodePool(name string, archValues []string, gpu *runner.GPURequest, ms runner.ManagedSpec, nodeClassName string) map[string]any {
	reqs := []map[string]any{
		{"key": "kubernetes.io/arch", "operator": "In", "values": archValues},
		{"key": "karpenter.sh/capacity-type", "operator": "In", "values": capacityTypes(ms.CapacityType)},
	}
	if len(ms.InstanceFamilies) > 0 {
		reqs = append(reqs, map[string]any{"key": "karpenter.k8s.aws/instance-family", "operator": "In", "values": ms.InstanceFamilies})
	}
	if gpu != nil {
		// Steer Karpenter to accelerated-instance categories for GPU pools.
		reqs = append(reqs, map[string]any{"key": "karpenter.k8s.aws/instance-category", "operator": "In", "values": []string{"g", "p"}})
	}

	consolidation := "WhenEmpty"
	if !ms.ScaleToZero {
		consolidation = "WhenEmptyOrUnderutilized"
	}

	limits := map[string]any{}
	if ms.CPULimit > 0 {
		limits["cpu"] = strconv.Itoa(ms.CPULimit)
	}
	if ms.GPULimit > 0 && gpu != nil {
		limits[gpu.K8sResourceName()] = strconv.Itoa(ms.GPULimit)
	}

	spec := map[string]any{
		"template": map[string]any{
			"metadata": map[string]any{
				"labels": map[string]string{runner.PoolLabel: name},
			},
			"spec": map[string]any{
				"requirements": reqs,
				"taints": []map[string]any{
					{"key": runner.CITaintKey, "value": name, "effect": "NoSchedule"},
				},
				"nodeClassRef": map[string]any{
					"group": "karpenter.k8s.aws",
					"kind":  "EC2NodeClass",
					"name":  nodeClassName,
				},
				"expireAfter": "720h",
			},
		},
		"disruption": map[string]any{
			"consolidationPolicy": consolidation,
			"consolidateAfter":    ms.ConsolidateAfter,
		},
	}
	if len(limits) > 0 {
		spec["limits"] = limits
	}

	return map[string]any{
		"apiVersion": "karpenter.sh/v1",
		"kind":       "NodePool",
		"metadata": map[string]any{
			"name":   "flint-" + name,
			"labels": map[string]string{"flint.dev/managed-by": name},
		},
		"spec": spec,
	}
}

func buildNodeClass(name string, ms runner.ManagedSpec, p runner.ProvisioningProfile) map[string]any {
	amiFamily := ms.AMIFamily
	if amiFamily == "" {
		amiFamily = p.AMIFamily
	}
	if amiFamily == "" {
		amiFamily = "AL2023"
	}
	return map[string]any{
		"apiVersion": "karpenter.k8s.aws/v1",
		"kind":       "EC2NodeClass",
		"metadata": map[string]any{
			"name":   name,
			"labels": map[string]string{"flint.dev/managed-by": name},
		},
		"spec": map[string]any{
			"amiFamily":                  amiFamily,
			"amiSelectorTerms":           []map[string]any{{"alias": amiAlias(amiFamily)}},
			"role":                       p.Role,
			"subnetSelectorTerms":        []map[string]any{{"tags": p.SubnetSelector}},
			"securityGroupSelectorTerms": []map[string]any{{"tags": p.SecurityGroupSelector}},
			"blockDeviceMappings": []map[string]any{
				{"deviceName": "/dev/xvda", "ebs": map[string]any{
					"volumeSize": fmt.Sprintf("%dGi", ms.DiskGiB),
					"volumeType": "gp3",
					"encrypted":  true,
				}},
			},
		},
	}
}

// capacityTypes maps the pool's capacity intent to Karpenter capacity-type values.
// spot-preferred = both allowed (Karpenter prefers spot, falls back to on-demand).
func capacityTypes(ct string) []string {
	switch ct {
	case "on-demand":
		return []string{"on-demand"}
	case "spot":
		return []string{"spot"}
	default: // spot-preferred
		return []string{"spot", "on-demand"}
	}
}

func amiAlias(family string) string {
	switch family {
	case "AL2023":
		return "al2023@latest"
	case "AL2":
		return "al2@latest"
	case "Bottlerocket":
		return "bottlerocket@latest"
	default:
		return "al2023@latest"
	}
}

func yamlDoc(v any) (string, error) {
	b, err := yaml.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("render: marshal: %w", err)
	}
	return string(b), nil
}
