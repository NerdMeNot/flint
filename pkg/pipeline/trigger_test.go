package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchGlob_ExactMatch(t *testing.T) {
	assert.True(t, matchGlob("main", "main"))
	assert.False(t, matchGlob("main", "develop"))
}

func TestMatchGlob_SingleStar(t *testing.T) {
	assert.True(t, matchGlob("feature/*", "feature/foo"))
	assert.False(t, matchGlob("feature/*", "feature/foo/bar"))
	assert.True(t, matchGlob("release-*", "release-v1"))
	assert.False(t, matchGlob("release-*", "release-v1/hotfix"))
}

func TestMatchGlob_DoubleStar(t *testing.T) {
	assert.True(t, matchGlob("feature/**", "feature/foo"))
	assert.True(t, matchGlob("feature/**", "feature/foo/bar"))
	assert.True(t, matchGlob("feature/**", "feature/foo/bar/baz"))
	assert.False(t, matchGlob("feature/**", "main"))
	assert.False(t, matchGlob("feature/**", "bugfix/foo"))
}

func TestMatchTriggers_PushExactBranch(t *testing.T) {
	p := &Pipeline{
		Triggers: Triggers{
			Push: &PushTrigger{Branches: []string{"main"}},
		},
	}

	matches := MatchTriggers(p, TriggerEvent{Kind: "push", Branch: "main"})
	assert.Len(t, matches, 1)
	assert.Equal(t, "push", matches[0].TriggerType)

	matches = MatchTriggers(p, TriggerEvent{Kind: "push", Branch: "develop"})
	assert.Empty(t, matches)
}

func TestMatchTriggers_PushWildcardBranch(t *testing.T) {
	p := &Pipeline{
		Triggers: Triggers{
			Push: &PushTrigger{Branches: []string{"main", "release/**"}},
		},
	}

	matches := MatchTriggers(p, TriggerEvent{Kind: "push", Branch: "release/v1"})
	assert.Len(t, matches, 1)

	matches = MatchTriggers(p, TriggerEvent{Kind: "push", Branch: "feature/foo"})
	assert.Empty(t, matches)
}

func TestMatchTriggers_PushWithEnvironments(t *testing.T) {
	p := &Pipeline{
		Triggers: Triggers{
			Push: &PushTrigger{
				Branches:     []string{"main"},
				Environments: []string{"staging", "production"},
			},
		},
	}

	matches := MatchTriggers(p, TriggerEvent{Kind: "push", Branch: "main"})
	assert.Len(t, matches, 1)
	assert.Equal(t, []string{"staging", "production"}, matches[0].Environments)
}

func TestMatchTriggers_PullRequest(t *testing.T) {
	p := &Pipeline{
		Triggers: Triggers{
			PullRequest: &PullRequestTrigger{Branches: []string{"main", "develop"}},
		},
	}

	matches := MatchTriggers(p, TriggerEvent{Kind: "pull_request", BaseBranch: "main"})
	assert.Len(t, matches, 1)
	assert.Equal(t, "pull_request", matches[0].TriggerType)
	assert.Nil(t, matches[0].Environments) // PRs never have environments

	matches = MatchTriggers(p, TriggerEvent{Kind: "pull_request", BaseBranch: "feature/foo"})
	assert.Empty(t, matches)
}

func TestMatchTriggers_Tag(t *testing.T) {
	p := &Pipeline{
		Triggers: Triggers{
			Tag: &TagTrigger{
				Patterns:     []string{"v*"},
				Environments: []string{"production"},
			},
		},
	}

	matches := MatchTriggers(p, TriggerEvent{Kind: "tag", Tag: "v1.0.0"})
	assert.Len(t, matches, 1)
	assert.Equal(t, []string{"production"}, matches[0].Environments)

	matches = MatchTriggers(p, TriggerEvent{Kind: "tag", Tag: "nightly-123"})
	assert.Empty(t, matches)
}

func TestMatchTriggers_NoMatchingTrigger(t *testing.T) {
	p := &Pipeline{
		Triggers: Triggers{
			Push: &PushTrigger{Branches: []string{"main"}},
		},
	}

	// Tag event, but pipeline only has push trigger.
	matches := MatchTriggers(p, TriggerEvent{Kind: "tag", Tag: "v1.0"})
	assert.Empty(t, matches)
}

func TestMatchTriggers_NilPipeline(t *testing.T) {
	assert.Nil(t, MatchTriggers(nil, TriggerEvent{Kind: "push", Branch: "main"}))
}

func TestCollectEnvironments_NoEnvs(t *testing.T) {
	matches := []TriggerMatch{
		{TriggerType: "push", Environments: nil},
	}
	envs := CollectEnvironments(matches)
	assert.Equal(t, []string{""}, envs)
}

func TestCollectEnvironments_WithEnvs(t *testing.T) {
	matches := []TriggerMatch{
		{TriggerType: "push", Environments: []string{"staging"}},
		{TriggerType: "tag", Environments: []string{"staging", "production"}},
	}
	envs := CollectEnvironments(matches)
	assert.ElementsMatch(t, []string{"staging", "production"}, envs)
}

func TestCollectEnvironments_Dedup(t *testing.T) {
	matches := []TriggerMatch{
		{TriggerType: "push", Environments: []string{"staging"}},
		{TriggerType: "push", Environments: []string{"staging"}},
	}
	envs := CollectEnvironments(matches)
	assert.Equal(t, []string{"staging"}, envs)
}
