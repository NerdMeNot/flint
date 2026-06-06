package pipeline_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

func TestIsEnvironmentAware(t *testing.T) {
	// Plain CI — not env-aware.
	plain := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{PullRequest: &pipeline.PullRequestTrigger{Branches: []string{"main"}}},
		Steps:    []pipeline.Step{{Name: "test", Run: pipeline.Cmd("echo test")}},
	}
	assert.False(t, pipeline.IsEnvironmentAware(plain))

	// Has top-level environments.
	withTopLevel := &pipeline.Pipeline{
		Environments: []string{"staging"},
		Triggers:     pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps:        []pipeline.Step{{Name: "test", Run: pipeline.Cmd("echo test")}},
	}
	assert.True(t, pipeline.IsEnvironmentAware(withTopLevel))

	// Trigger with environments.
	withTriggerEnv := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}, Environments: []string{"staging"}}},
		Steps:    []pipeline.Step{{Name: "test", Run: pipeline.Cmd("echo test")}},
	}
	assert.True(t, pipeline.IsEnvironmentAware(withTriggerEnv))

	// Step with environments.
	withStepEnv := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps:    []pipeline.Step{{Name: "test", Run: pipeline.Cmd("echo test"), Environments: []string{"production"}}},
	}
	assert.True(t, pipeline.IsEnvironmentAware(withStepEnv))

	// Has promotion trigger.
	withPromo := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{
			Push:      &pipeline.PushTrigger{Branches: []string{"main"}},
			Promotion: []pipeline.PromotionTrigger{{From: "staging", Environments: []string{"production"}}},
		},
		Steps: []pipeline.Step{{Name: "test", Run: pipeline.Cmd("echo test")}},
	}
	assert.True(t, pipeline.IsEnvironmentAware(withPromo))
}

func TestSimulateEnv(t *testing.T) {
	p := &pipeline.Pipeline{
		Environments: []string{"staging", "production"},
		Triggers: pipeline.Triggers{
			PullRequest: &pipeline.PullRequestTrigger{Branches: []string{"main"}},
			Push:        &pipeline.PushTrigger{Branches: []string{"main"}, Environments: []string{"staging"}},
			Promotion:   []pipeline.PromotionTrigger{{From: "staging", Environments: []string{"production"}}},
			Manual:      &pipeline.ManualTrigger{Environments: []string{"staging", "production"}},
		},
		Steps: []pipeline.Step{
			{Name: "approve", Gate: &pipeline.Gate{Approvers: []string{"role:release-manager"}}, Environments: []string{"production"}},
			{Name: "test", Run: pipeline.Cmd("make test")},
			{Name: "security-scan", Run: pipeline.Cmd("make scan"), Environments: []string{"production"}, DependsOn: []string{"test"}},
			{Name: "deploy", Run: pipeline.Cmd("make deploy"), DependsOn: []string{"approve", "test"}},
		},
	}

	// Simulate staging.
	sim := pipeline.SimulateEnv(p, "staging")
	require.NotNil(t, sim)
	assert.Equal(t, "staging", sim.TargetEnv)

	// Triggers: push active, PR not active (plain CI), promotion not active, manual active.
	pushTrigger := findTriggerStatus(sim, "push")
	require.NotNil(t, pushTrigger)
	assert.True(t, pushTrigger.Active)

	promoTrigger := findTriggerStatus(sim, "promotion")
	require.NotNil(t, promoTrigger)
	assert.False(t, promoTrigger.Active) // production only

	// Steps: approve skipped, test active, security-scan skipped, deploy active.
	approveStatus := findStepStatus(sim, "approve")
	assert.False(t, approveStatus.Active)

	testStatus := findStepStatus(sim, "test")
	assert.True(t, testStatus.Active)

	scanStatus := findStepStatus(sim, "security-scan")
	assert.False(t, scanStatus.Active)

	deployStatus := findStepStatus(sim, "deploy")
	assert.True(t, deployStatus.Active)

	// Simulate production.
	sim = pipeline.SimulateEnv(p, "production")
	promoTrigger = findTriggerStatus(sim, "promotion")
	assert.True(t, promoTrigger.Active)

	approveStatus = findStepStatus(sim, "approve")
	assert.True(t, approveStatus.Active)

	scanStatus = findStepStatus(sim, "security-scan")
	assert.True(t, scanStatus.Active)
}

func TestSimulateEnv_MatrixStep(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps: []pipeline.Step{
			{Name: "test", Run: pipeline.Cmd("echo"), Matrix: map[string][]string{"node": {"18", "20", "22"}}},
		},
	}
	sim := pipeline.SimulateEnv(p, "staging")
	require.Len(t, sim.Steps, 1)
	assert.Equal(t, 3, sim.Steps[0].MatrixCombinations)
}

func TestIsEnvironmentAware_PlainCI(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{PullRequest: &pipeline.PullRequestTrigger{Branches: []string{"main"}}},
		Steps:    []pipeline.Step{{Name: "test", Run: pipeline.Cmd("echo")}},
	}
	assert.False(t, pipeline.IsEnvironmentAware(p))
}

func TestSimulateEnv_NoEnvironments(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps:    []pipeline.Step{{Name: "test", Run: pipeline.Cmd("echo")}},
	}
	sim := pipeline.SimulateEnv(p, "")
	require.NotNil(t, sim)
	require.Len(t, sim.Steps, 1)
	assert.True(t, sim.Steps[0].Active)
}

func TestSimulateEnv_WhenAlwaysFiltered(t *testing.T) {
	p := &pipeline.Pipeline{
		Environments: []string{"staging", "production"},
		Triggers:     pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}, Environments: []string{"staging"}}},
		Steps: []pipeline.Step{
			{Name: "notify", Run: pipeline.Cmd("echo"), When: "always", Environments: []string{"production"}},
			{Name: "test", Run: pipeline.Cmd("echo")},
		},
	}
	sim := pipeline.SimulateEnv(p, "staging")
	notify := findStepStatus(sim, "notify")
	require.NotNil(t, notify)
	assert.False(t, notify.Active, "env-filtered step stays inactive even with when: always")
}
