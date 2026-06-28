package runner

import (
	"encoding/json"
	"testing"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpecFromRow_ArchNotAutoPinned(t *testing.T) {
	t.Run("reference pool arch is never auto-added to the selector", func(t *testing.T) {
		spec := SpecFromRow(db.ListRunnerPoolsRow{Name: "arm", Arch: "arm64", Mode: "reference", Cpu: "2", Memory: "4Gi"})
		_, has := spec.NodeSelector[ArchLabel]
		assert.False(t, has, "arch is a descriptor, not an auto-pinned selector")
	})

	t.Run("explicit arch selector is passed through untouched", func(t *testing.T) {
		ns, _ := json.Marshal(map[string]string{ArchLabel: "arm64", "karpenter.sh/nodepool": "ci"})
		spec := SpecFromRow(db.ListRunnerPoolsRow{Name: "cpu", Arch: "arm64", Mode: "reference", Cpu: "8", Memory: "16Gi", NodeSelector: ns})
		assert.Equal(t, "arm64", spec.NodeSelector[ArchLabel])
		assert.Equal(t, "ci", spec.NodeSelector["karpenter.sh/nodepool"])
	})

	t.Run("managed pool keeps only its derived pool selector", func(t *testing.T) {
		ns, _ := json.Marshal(PoolNodeSelector("spot"))
		spec := SpecFromRow(db.ListRunnerPoolsRow{Name: "spot", Arch: "amd64", Mode: "managed", Cpu: "4", Memory: "8Gi", NodeSelector: ns})
		require.NotNil(t, spec.NodeSelector)
		_, has := spec.NodeSelector[ArchLabel]
		assert.False(t, has)
		assert.Equal(t, "spot", spec.NodeSelector[PoolLabel])
	})
}

func TestSpecFromRow_OptionalResources(t *testing.T) {
	t.Run("blank cpu/memory leave zero quantities", func(t *testing.T) {
		spec := SpecFromRow(db.ListRunnerPoolsRow{Name: "p", Arch: "amd64", Mode: "reference", Cpu: "", Memory: ""})
		assert.True(t, spec.Resources.CPU.IsZero())
		assert.True(t, spec.Resources.Memory.IsZero())
	})
}
