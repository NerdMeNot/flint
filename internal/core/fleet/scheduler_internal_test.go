package fleet

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// B2: a new run (no holder) prefers a STABLE machine — its workspace should live
// on non-reclaimable capacity — and only overflows to interruptible when no
// stable machine has room. Affinity to an existing holder still wins outright.
func TestPickMachine_PrefersStableForNewRuns(t *testing.T) {
	req := db.ClaimPendingAssignmentsRow{RunID: "run-1", CpuMillis: 1000, MemoryMb: 1024}

	stable := &candidate{id: "stable", freeCPU: 4000, freeMem: 8192, interruptible: false}
	spot := &candidate{id: "spot", freeCPU: 4000, freeMem: 8192, interruptible: true}

	t.Run("stable preferred over interruptible with equal room", func(t *testing.T) {
		got := pickMachine([]*candidate{spot, stable}, req)
		assert.Equal(t, "stable", got.id)
	})

	t.Run("smaller stable wins on best-fit among stable", func(t *testing.T) {
		small := &candidate{id: "small", freeCPU: 1500, freeMem: 2048, interruptible: false}
		got := pickMachine([]*candidate{stable, small, spot}, req)
		assert.Equal(t, "small", got.id, "best-fit picks the tightest stable machine")
	})

	t.Run("overflows to interruptible only when no stable fits", func(t *testing.T) {
		fullStable := &candidate{id: "stable", freeCPU: 100, freeMem: 100, interruptible: false}
		got := pickMachine([]*candidate{fullStable, spot}, req)
		assert.Equal(t, "spot", got.id, "interruptible is the overflow when stable is full")
	})

	t.Run("no capacity anywhere → nil", func(t *testing.T) {
		full := &candidate{id: "x", freeCPU: 1, freeMem: 1, interruptible: false}
		assert.Nil(t, pickMachine([]*candidate{full}, req))
	})
}

// B2: run affinity is a hard guarantee and overrides the stable preference — a
// run already homed on an interruptible machine stays there (its workspace is
// only on that machine); we don't split a run across machines for reliability.
func TestPickMachine_AffinityBeatsClassPreference(t *testing.T) {
	req := db.ClaimPendingAssignmentsRow{RunID: "run-1", CpuMillis: 1000, MemoryMb: 1024}
	spotHolder := &candidate{id: "spot-holder", freeCPU: 4000, freeMem: 8192, interruptible: true, runIDs: []string{"run-1"}}
	freeStable := &candidate{id: "free-stable", freeCPU: 4000, freeMem: 8192, interruptible: false}

	got := pickMachine([]*candidate{freeStable, spotHolder}, req)
	assert.Equal(t, "spot-holder", got.id, "the run's existing holder wins even though it is interruptible")
}
