package ci

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func affectedList(m map[string]bool) []string {
	var out []string
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// D2: a change to a job's declared input files marks it affected, and the
// affected set is closed under the needs graph (dependents come along).
func TestAffectedJobs(t *testing.T) {
	jobs := map[string]Job{
		"api": {
			Inputs: &JobInputs{Files: []string{"services/api/**"}},
		},
		"web": {
			Inputs: &JobInputs{Files: []string{"services/web/**"}},
		},
		"e2e": { // depends on both; declares no file inputs
			Needs: []string{"api", "web"},
		},
	}

	t.Run("a change under api affects api + its dependent e2e, not web", func(t *testing.T) {
		got := AffectedJobs(jobs, []string{"services/api/handler.go"})
		assert.Equal(t, []string{"api", "e2e"}, affectedList(got))
	})

	t.Run("a change under web affects web + e2e", func(t *testing.T) {
		got := AffectedJobs(jobs, []string{"services/web/app.tsx"})
		assert.Equal(t, []string{"e2e", "web"}, affectedList(got))
	})

	t.Run("an unrelated change affects only no-declared-input jobs", func(t *testing.T) {
		// e2e has no declared file inputs → always affected; api/web are provably
		// unaffected. e2e depends on nothing that changed, but conservatism keeps it.
		got := AffectedJobs(jobs, []string{"README.md"})
		assert.Equal(t, []string{"e2e"}, affectedList(got))
	})

	t.Run("a change to both affects everything", func(t *testing.T) {
		got := AffectedJobs(jobs, []string{"services/api/x.go", "services/web/y.tsx"})
		assert.Equal(t, []string{"api", "e2e", "web"}, affectedList(got))
	})
}

// D2: a job with no declared inputs is conservatively always affected (you opt
// INTO skippability by declaring inputs).
func TestAffectedJobs_NoInputsAlwaysAffected(t *testing.T) {
	jobs := map[string]Job{
		"build": {}, // no inputs declared
	}
	got := AffectedJobs(jobs, []string{"anything.txt"})
	assert.Equal(t, []string{"build"}, affectedList(got))

	// even with an empty changed-file set.
	got = AffectedJobs(jobs, nil)
	assert.Equal(t, []string{"build"}, affectedList(got))
}

// D2: the closure is transitive across multiple hops.
func TestAffectedJobs_TransitiveChain(t *testing.T) {
	jobs := map[string]Job{
		"a": {Inputs: &JobInputs{Files: []string{"pkg/a/**"}}},
		"b": {Needs: []string{"a"}, Inputs: &JobInputs{Files: []string{"pkg/b/**"}}},
		"c": {Needs: []string{"b"}, Inputs: &JobInputs{Files: []string{"pkg/c/**"}}},
	}
	// change only a's files → a, b, c all affected (chain).
	got := AffectedJobs(jobs, []string{"pkg/a/main.go"})
	assert.Equal(t, []string{"a", "b", "c"}, affectedList(got))

	// change only c's files → only c.
	got = AffectedJobs(jobs, []string{"pkg/c/main.go"})
	assert.Equal(t, []string{"c"}, affectedList(got))
}
