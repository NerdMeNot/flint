package config_test

import (
	"testing"

	"github.com/NerdMeNot/flint/internal/platform/config"
	"github.com/stretchr/testify/assert"
)

func TestProvisioningConfig_Configured(t *testing.T) {
	full := config.ProvisioningConfig{
		Role:                  "KarpenterNodeRole",
		SubnetSelector:        []string{"karpenter.sh/discovery=flint"},
		SecurityGroupSelector: []string{"karpenter.sh/discovery=flint"},
	}
	assert.True(t, full.Configured(), "complete profile is configured")

	cases := map[string]config.ProvisioningConfig{
		"empty":      {},
		"no role":    {SubnetSelector: []string{"k=v"}, SecurityGroupSelector: []string{"k=v"}},
		"no subnet":  {Role: "r", SecurityGroupSelector: []string{"k=v"}},
		"no sg":      {Role: "r", SubnetSelector: []string{"k=v"}},
		"blank pair": {Role: "r", SubnetSelector: []string{"no-equals"}, SecurityGroupSelector: []string{"no-equals"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.False(t, c.Configured(), "incomplete profile must not be configured")
		})
	}
}
