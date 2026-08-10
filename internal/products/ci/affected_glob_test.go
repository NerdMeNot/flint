package ci

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// A malformed input glob never matches, and "no match" means the job is not
// affected — so a typo silently drops the job from the run and the pipeline
// still reports green. Validation is where the author can see it.
func TestValidate_RejectsAMalformedInputGlob(t *testing.T) {
	p := &Pipeline{
		Image:    "alpine:3.19",
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Jobs: map[string]Job{
			"build": {
				Steps:  []Step{{Name: "s", Run: pipeline.Cmd("echo hi")}},
				Inputs: &JobInputs{Files: []string{"src/[unclosed"}},
			},
		},
	}

	res := p.ValidateDetailed()

	require.False(t, res.Valid(), "an unparseable glob must be a validation error")
	var found bool
	for _, i := range res.Issues {
		if i.Field == "jobs.build.inputs.files[0]" {
			found = true
			assert.Contains(t, i.Message, "not a valid pattern")
		}
	}
	assert.True(t, found, "the error must point at the offending glob")
}

// A well-formed glob still validates.
func TestValidate_AcceptsAValidInputGlob(t *testing.T) {
	p := &Pipeline{
		Image:    "alpine:3.19",
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Jobs: map[string]Job{
			"build": {
				Steps:  []Step{{Name: "s", Run: pipeline.Cmd("echo hi")}},
				Inputs: &JobInputs{Files: []string{"src/**/*.go"}},
			},
		},
	}

	for _, i := range p.ValidateDetailed().Issues {
		assert.NotEqual(t, "jobs.build.inputs.files[0]", i.Field,
			"a valid doublestar pattern must not be flagged: %s", i.Message)
	}
}

// Affected-job selection is conservative: a job whose glob matches a changed
// file is included, along with its dependents.
func TestAffectedJobs_SelectsByGlobAndPullsInDependents(t *testing.T) {
	jobs := map[string]Job{
		"build": {Inputs: &JobInputs{Files: []string{"src/**/*.go"}}},
		"test":  {Needs: []string{"build"}, Inputs: &JobInputs{Files: []string{"test/**"}}},
		"docs":  {Inputs: &JobInputs{Files: []string{"docs/**"}}},
	}

	affected := AffectedJobs(jobs, []string{"src/api/handler.go"})

	assert.True(t, affected["build"], "the job whose inputs matched")
	assert.True(t, affected["test"], "and everything downstream of it")
	assert.False(t, affected["docs"], "an unrelated branch stays out")
}

// A job declaring no inputs cannot be proven unaffected, so it always runs.
func TestAffectedJobs_IncludesJobsWithNoDeclaredInputs(t *testing.T) {
	jobs := map[string]Job{
		"undeclared": {},
		"docs":       {Inputs: &JobInputs{Files: []string{"docs/**"}}},
	}

	affected := AffectedJobs(jobs, []string{"src/main.go"})

	assert.True(t, affected["undeclared"], "opting into affected-mode is explicit")
	assert.False(t, affected["docs"])
}

// The runtime backstop: a glob that reached execution malformed must not match,
// and must not panic or crash the selection pass.
func TestMatchesAnyGlob_MalformedPatternIsNotAMatch(t *testing.T) {
	assert.False(t, matchesAnyGlob([]string{"src/[unclosed"}, "src/main.go"))
	assert.True(t, matchesAnyGlob([]string{"src/[unclosed", "src/**"}, "src/main.go"),
		"a bad pattern must not stop a later good one from matching")
}
