package pipeline_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

func TestValidate_ValidPipeline(t *testing.T) {
	data := readTestdata(t, "minimal.yaml")
	result := pipeline.Validate(data, pipeline.ValidateOptions{})
	assert.True(t, result.Valid())
	assert.Empty(t, result.Errors())
}

func TestValidate_DeployPipeline(t *testing.T) {
	data := readTestdata(t, "deploy.yaml")
	result := pipeline.Validate(data, pipeline.ValidateOptions{})
	// deploy.yaml is structurally valid — may have warnings about env-aware triggers.
	assert.Empty(t, result.Errors())
}

func TestValidate_UnknownEnvironment(t *testing.T) {
	yaml := `
environments: [staging, production]
triggers:
  push:
    branches: [main]
    environments: [staging]
steps:
  - name: test
    run: echo
    environments: [prodction]
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{
		Environments: []string{"staging", "production"},
	})
	require.False(t, result.Valid())

	// Should have errors mentioning "prodction" — both unknown env and subset violation.
	errs := result.Errors()
	require.NotEmpty(t, errs)
	hasMention := false
	hasSuggestion := false
	for _, e := range errs {
		if e.Message != "" {
			hasMention = true
		}
		if e.Suggestion != "" {
			hasSuggestion = true
		}
	}
	assert.True(t, hasMention, "expected at least one error")
	assert.True(t, hasSuggestion, "expected a 'did you mean' suggestion")
}

func TestValidate_EnvironmentSubset(t *testing.T) {
	yaml := `
environments: [staging, production]
triggers:
  push:
    branches: [main]
    environments: [dev]
steps:
  - name: test
    run: echo
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "dev")
	assert.Contains(t, result.Errors()[0].Message, "not in the pipeline's top-level environments")
}

func TestValidate_InvalidWhen(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: test
    run: echo
    when: sometimes
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "sometimes")
}

func TestValidate_InvalidApprover(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: approve
    gate:
      approvers: [invalid-format]
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "invalid approver format")
}

func TestValidate_ImagePresetsOnly(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: build
    image: ubuntu:24.04
    run: echo
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{
		ImagePresets: []string{"node20", "node22", "golang122"},
		PresetsOnly:  true,
	})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "not a known preset")
}

func TestValidate_ManualInputsWithoutDefaults(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
  manual:
    inputs:
      - name: version
        type: string
        required: true
steps:
  - name: test
    run: echo
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "no default")
}

func TestValidate_PromotionSelfTarget(t *testing.T) {
	yaml := `
triggers:
  promotion:
    - from: production
      environments: [production]
steps:
  - name: test
    run: echo
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "cannot target the same environment")
}

func TestValidate_DependsOnTypo(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: build
    run: echo build
  - name: deploy
    run: echo deploy
    dependsOn: [bild]
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	errs := result.Errors()
	require.Len(t, errs, 1)
	// The parser catches the unknown ref. Validate converts it to a ValidationIssue.
	assert.Contains(t, errs[0].Message, "bild")
}

func TestValidate_InvalidTimeout(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: test
    timeout: "abc"
    run: echo
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "invalid duration")
}

func TestValidate_GateApproversBounds(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: approve
    gate:
      approvers: [role:admin]
      minApprovals: 5
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "exceeds")
}

func TestValidate_GateNoApprovers(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: approve
    gate:
      approvers: []
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "at least one approver")
}

func TestValidate_InvalidCron(t *testing.T) {
	yaml := `
triggers:
  schedule:
    cron: "invalid cron"
steps:
  - name: test
    run: echo
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "expected 5")
}

func TestValidate_ChoiceInputWithoutOptions(t *testing.T) {
	yaml := `
triggers:
  manual:
    inputs:
      - name: region
        type: choice
steps:
  - name: test
    run: echo
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "choice input must have")
}

func TestValidate_InvalidInputType(t *testing.T) {
	yaml := `
triggers:
  manual:
    inputs:
      - name: count
        type: integer
steps:
  - name: test
    run: echo
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "invalid input type")
}

func TestValidate_RetryAttemptsZero(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: test
    run: echo
    retry:
      attempts: 0
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "at least 1")
}

func TestValidate_InvalidShell(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: test
    shell: ruby
    run: puts 'hello'
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "ruby")
}

func TestValidate_AllTriggersCombined(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
  pull_request: [main]
  promotion:
    - from: staging
      environments: [production]
steps:
  - name: test
    run: echo
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	// Should have a warning about PR + promotion.
	warnings := result.Warnings()
	found := false
	for _, w := range warnings {
		if w.Code == pipeline.CodeTriggerConflict {
			found = true
		}
	}
	assert.True(t, found, "expected trigger conflict warning for PR + promotion")
}

func TestValidate_DuplicatePromotionFrom(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
    environments: [staging]
  promotion:
    - from: staging
      environments: [production]
    - from: staging
      environments: [canary]
steps:
  - name: test
    run: echo
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "duplicate promotion")
}

func TestValidate_EnvVarNameWarning(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: test
    run: echo
    env:
      valid_NAME: ok
      invalid-name: bad
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	warnings := result.Warnings()
	found := false
	for _, w := range warnings {
		if w.Field == "steps[0].env.invalid-name" {
			found = true
		}
	}
	assert.True(t, found, "expected warning for invalid env var name")
}

func TestValidate_ArtifactPathAbsolute(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: build
    run: echo
    outputs:
      - path: relative/path
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.Contains(t, result.Errors()[0].Message, "absolute")
}

func TestValidate_ErrorCodes(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: test
    run: echo
    dependsOn: [missing]
`
	result := pipeline.Validate([]byte(yaml), pipeline.ValidateOptions{})
	require.False(t, result.Valid())
	assert.NotEmpty(t, result.Errors()[0].Code)
}
