package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// chainGraph builds a linear dependency chain of n steps: s0 → s1 → … → s(n-1).
// A chain is the worst case for the old per-step DFS — every step must walk its
// entire upstream — and the shape a long sequential pipeline actually has.
func chainGraph(n int, failFirst bool) ([][]string, map[string]stepRow) {
	waves := make([][]string, 0, n)
	steps := make(map[string]stepRow, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("s%d", i)
		var deps []string
		if i > 0 {
			deps = []string{fmt.Sprintf("s%d", i-1)}
		}
		status := stepSucceeded
		if i == 0 && failFirst {
			status = stepFailed
		}
		steps[name] = stepRow{name: name, status: status, dependsOn: deps}
		waves = append(waves, []string{name})
	}
	return waves, steps
}

// foldAncestors mirrors the fold advanceWorkflow performs, so the benchmark and
// the assertion below exercise the same logic the engine uses.
func foldAncestors(waves [][]string, steps map[string]stepRow) map[string]bool {
	failedAncestor := make(map[string]bool, len(steps))
	folded := make(map[string]bool, len(steps))
	for _, wave := range waves {
		for _, name := range wave {
			s, ok := steps[name]
			if !ok {
				continue
			}
			upstreamFailed := false
			for _, dep := range s.dependsOn {
				d, ok := steps[dep]
				if !ok {
					continue
				}
				if !folded[dep] {
					upstreamFailed = ancestorFailed(name, steps)
					break
				}
				if failedAncestor[dep] || (d.status == stepFailed && d.onFailure != "continue") {
					upstreamFailed = true
					break
				}
			}
			failedAncestor[name] = upstreamFailed
			folded[name] = true
		}
	}
	return failedAncestor
}

// The fold must agree with the full walk it replaced, for every step — that is
// the only thing making the optimisation safe.
func TestAncestorFold_MatchesFullWalk(t *testing.T) {
	for _, n := range []int{1, 2, 5, 25} {
		for _, failFirst := range []bool{false, true} {
			waves, steps := chainGraph(n, failFirst)
			folded := foldAncestors(waves, steps)
			for name := range steps {
				assert.Equal(t, ancestorFailed(name, steps), folded[name],
					"n=%d failFirst=%v step=%s", n, failFirst, name)
			}
		}
	}
}

// A diamond: d depends on b and c, both of which depend on a. A failure at a
// must reach d exactly once, and a failure confined to one arm must still reach
// d, while an unrelated branch stays clean.
func TestAncestorFold_Diamond(t *testing.T) {
	steps := map[string]stepRow{
		"a":         {name: "a", status: stepFailed},
		"b":         {name: "b", status: stepSkipped, dependsOn: []string{"a"}},
		"c":         {name: "c", status: stepSkipped, dependsOn: []string{"a"}},
		"d":         {name: "d", status: stepPending, dependsOn: []string{"b", "c"}},
		"unrelated": {name: "unrelated", status: stepPending},
	}
	waves := [][]string{{"a"}, {"b", "c"}, {"d"}, {"unrelated"}}

	folded := foldAncestors(waves, steps)
	assert.True(t, folded["d"], "the failure at the diamond's head must reach its tail")
	assert.False(t, folded["unrelated"], "an unrelated branch must stay unaffected")
	assert.False(t, folded["a"], "the failed step is not its own failed ancestor")
}

// continueOnError contains a failure: it must not propagate as an unrecoverable
// ancestor failure.
func TestAncestorFold_ContinueOnErrorDoesNotPropagate(t *testing.T) {
	steps := map[string]stepRow{
		"a": {name: "a", status: stepFailed, onFailure: "continue"},
		"b": {name: "b", status: stepPending, dependsOn: []string{"a"}},
	}
	folded := foldAncestors([][]string{{"a"}, {"b"}}, steps)
	assert.False(t, folded["b"], "a tolerated failure must not gate downstream steps")
}

// The cost difference the fold exists for. The old shape recomputed the full
// ancestor walk per step (quadratic on a chain); the fold carries each verdict
// forward once.
func BenchmarkAncestors_PerStepWalk(b *testing.B) {
	waves, steps := chainGraph(300, true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, wave := range waves {
			for _, name := range wave {
				_ = ancestorFailed(name, steps)
			}
		}
	}
}

func BenchmarkAncestors_Fold(b *testing.B) {
	waves, steps := chainGraph(300, true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = foldAncestors(waves, steps)
	}
}
