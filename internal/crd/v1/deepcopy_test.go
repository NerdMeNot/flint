package v1_test

import (
	"testing"

	v1 "github.com/NerdMeNot/flint/internal/crd/v1"
)

// TestStepTemplateDeepCopy_OptionsIsolated guards against a regression where the
// per-input Options slice was shallow-copied and shared between original and copy.
func TestStepTemplateDeepCopy_OptionsIsolated(t *testing.T) {
	orig := &v1.StepTemplate{
		Spec: v1.StepTemplateSpec{
			Inputs: []v1.StepTemplateInput{
				{Name: "env", Type: "choice", Options: []string{"staging", "production"}},
			},
		},
	}

	clone := &v1.StepTemplate{}
	orig.DeepCopyInto(clone)

	// Mutate the clone's Options; the original must be untouched.
	clone.Spec.Inputs[0].Options[0] = "MUTATED"

	if got := orig.Spec.Inputs[0].Options[0]; got != "staging" {
		t.Fatalf("deep copy shared the Options backing array: original mutated to %q", got)
	}
}
