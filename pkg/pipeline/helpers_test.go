package pipeline_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return data
}

func findStep(p *pipeline.Pipeline, name string) *pipeline.Step {
	for i := range p.Steps {
		if p.Steps[i].Name == name {
			return &p.Steps[i]
		}
	}
	return nil
}

func findTriggerStatus(sim *pipeline.EnvSimulation, triggerType string) *pipeline.TriggerStatus {
	for i := range sim.ActiveTriggers {
		if sim.ActiveTriggers[i].Type == triggerType {
			return &sim.ActiveTriggers[i]
		}
	}
	return nil
}

func findStepStatus(sim *pipeline.EnvSimulation, name string) *pipeline.StepStatus {
	for i := range sim.Steps {
		if sim.Steps[i].Name == name {
			return &sim.Steps[i]
		}
	}
	return nil
}
