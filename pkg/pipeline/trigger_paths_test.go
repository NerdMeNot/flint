package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMatchTriggers_PathsFilter locks the paths: filter semantics: no filter →
// always; nil changed files → fail open; otherwise any file must match.
func TestMatchTriggers_PathsFilter(t *testing.T) {
	p := &Pipeline{Triggers: Triggers{
		Push: &PushTrigger{Branches: []string{"main"}, Paths: []string{"src/**", "go.mod"}},
	}}

	ev := func(files []string) TriggerEvent {
		return TriggerEvent{Kind: "push", Branch: "main", ChangedFiles: files}
	}

	assert.Len(t, MatchTriggers(p, ev([]string{"src/app/main.go"})), 1, "matching path runs")
	assert.Len(t, MatchTriggers(p, ev([]string{"go.mod"})), 1, "exact path runs")
	assert.Empty(t, MatchTriggers(p, ev([]string{"docs/readme.md"})), "non-matching path is filtered")
	assert.Empty(t, MatchTriggers(p, ev([]string{})), "no changed files is filtered")
	assert.Len(t, MatchTriggers(p, ev(nil)), 1, "unknown file list fails open")

	// No paths filter → files are irrelevant.
	plain := &Pipeline{Triggers: Triggers{Push: &PushTrigger{Branches: []string{"main"}}}}
	assert.Len(t, MatchTriggers(plain, ev([]string{"docs/readme.md"})), 1)
}
