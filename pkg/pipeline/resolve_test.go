package pipeline_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

func TestParseUseRef_CRD(t *testing.T) {
	ref := pipeline.ParseUseRef("ecr-login")
	assert.Equal(t, pipeline.UseRefCRD, ref.Kind)
	assert.Equal(t, "ecr-login", ref.Name)
}

func TestParseUseRef_Local(t *testing.T) {
	ref := pipeline.ParseUseRef("./fragments/setup.yaml")
	assert.Equal(t, pipeline.UseRefLocal, ref.Kind)
	assert.Equal(t, "./fragments/setup.yaml", ref.Path)
}

func TestParseUseRef_CrossRepo(t *testing.T) {
	ref := pipeline.ParseUseRef("acme/pipeline-templates/steps/go-build.yaml@v1")
	assert.Equal(t, pipeline.UseRefCrossRepo, ref.Kind)
	assert.Equal(t, "acme", ref.Org)
	assert.Equal(t, "pipeline-templates", ref.Repo)
	assert.Equal(t, "steps/go-build.yaml", ref.File)
	assert.Equal(t, "v1", ref.Ref)
}

func TestParseUseRef_CrossRepoWithSHA(t *testing.T) {
	ref := pipeline.ParseUseRef("acme/templates/deploy.yaml@abc1234")
	assert.Equal(t, pipeline.UseRefCrossRepo, ref.Kind)
	assert.Equal(t, "acme", ref.Org)
	assert.Equal(t, "templates", ref.Repo)
	assert.Equal(t, "deploy.yaml", ref.File)
	assert.Equal(t, "abc1234", ref.Ref)
}

func TestParseUseRef_ParentPath(t *testing.T) {
	ref := pipeline.ParseUseRef("../shared/setup.yaml")
	assert.Equal(t, pipeline.UseRefLocal, ref.Kind)
	assert.Equal(t, "../shared/setup.yaml", ref.Path)
}

// mockResolver implements TemplateResolver for testing.
type mockResolver struct {
	steps map[string]*pipeline.ResolvedStepTemplate
	files map[string]*pipeline.ResolvedFileTemplate
}

func (m *mockResolver) ResolveStep(_ context.Context, name string) (*pipeline.ResolvedStepTemplate, error) {
	if tmpl, ok := m.steps[name]; ok {
		return tmpl, nil
	}
	return nil, fmt.Errorf("template %q not found", name)
}

func (m *mockResolver) ResolveFile(_ context.Context, ref string) (*pipeline.ResolvedFileTemplate, error) {
	if tmpl, ok := m.files[ref]; ok {
		return tmpl, nil
	}
	return nil, fmt.Errorf("file %q not found", ref)
}

func TestResolveTemplates_CRDWithInputSubstitution(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps: []pipeline.Step{
			{
				Name: "login",
				Use:  "ecr-login",
				With: map[string]string{
					"registry": "123456789.dkr.ecr.us-east-1.amazonaws.com",
					"region":   "us-west-2",
				},
			},
			{
				Name:      "build",
				Run:       "make build",
				DependsOn: []string{"login"},
			},
		},
	}

	resolver := &mockResolver{
		steps: map[string]*pipeline.ResolvedStepTemplate{
			"ecr-login": {
				Name: "ecr-login",
				Inputs: []pipeline.TemplateInput{
					{Name: "registry", Type: "string", Required: true},
					{Name: "region", Type: "string", Default: "us-east-1"},
				},
				Image: "amazon/aws-cli:2",
				Run:   `aws ecr get-login-password --region ${{ inputs.region }} | docker login --username AWS --password-stdin ${{ inputs.registry }}`,
			},
		},
	}

	resolved, err := pipeline.ResolveTemplates(context.Background(), p, resolver)
	require.NoError(t, err)

	// Step should now be a run step with inputs substituted.
	login := resolved.Steps[0]
	assert.Equal(t, "", login.Use) // use cleared
	assert.Nil(t, login.With)      // with cleared
	assert.Equal(t, "amazon/aws-cli:2", login.Image)
	assert.Contains(t, login.Run, "us-west-2")
	assert.Contains(t, login.Run, "123456789.dkr.ecr.us-east-1.amazonaws.com")
	assert.NotContains(t, login.Run, "${{ inputs.")

	// Build step should be untouched.
	assert.Equal(t, "make build", resolved.Steps[1].Run)
}

func TestResolveTemplates_MissingRequiredInput(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps: []pipeline.Step{
			{
				Name: "login",
				Use:  "ecr-login",
				With: map[string]string{}, // missing required "registry"
			},
		},
	}

	resolver := &mockResolver{
		steps: map[string]*pipeline.ResolvedStepTemplate{
			"ecr-login": {
				Name: "ecr-login",
				Inputs: []pipeline.TemplateInput{
					{Name: "registry", Type: "string", Required: true},
				},
				Run: "echo ${{ inputs.registry }}",
			},
		},
	}

	_, err := pipeline.ResolveTemplates(context.Background(), p, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
	assert.Contains(t, err.Error(), "registry")
}

func TestResolveTemplates_UnknownInput(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps: []pipeline.Step{
			{
				Name: "login",
				Use:  "ecr-login",
				With: map[string]string{"registry": "foo", "bogus": "bar"},
			},
		},
	}

	resolver := &mockResolver{
		steps: map[string]*pipeline.ResolvedStepTemplate{
			"ecr-login": {
				Name: "ecr-login",
				Inputs: []pipeline.TemplateInput{
					{Name: "registry", Type: "string", Required: true},
				},
				Run: "echo ${{ inputs.registry }}",
			},
		},
	}

	_, err := pipeline.ResolveTemplates(context.Background(), p, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
	assert.Contains(t, err.Error(), "bogus")
}

func TestResolveTemplates_DefaultInput(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps: []pipeline.Step{
			{
				Name: "login",
				Use:  "ecr-login",
				With: map[string]string{"registry": "my-registry"},
				// region not provided — should use default
			},
		},
	}

	resolver := &mockResolver{
		steps: map[string]*pipeline.ResolvedStepTemplate{
			"ecr-login": {
				Name: "ecr-login",
				Inputs: []pipeline.TemplateInput{
					{Name: "registry", Type: "string", Required: true},
					{Name: "region", Type: "string", Default: "us-east-1"},
				},
				Run: "aws --region ${{ inputs.region }} ecr get-login-password | docker login ${{ inputs.registry }}",
			},
		},
	}

	resolved, err := pipeline.ResolveTemplates(context.Background(), p, resolver)
	require.NoError(t, err)

	login := resolved.Steps[0]
	assert.Contains(t, login.Run, "us-east-1")   // default applied
	assert.Contains(t, login.Run, "my-registry") // provided value
}

func TestResolveTemplates_NestedUse(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps: []pipeline.Step{
			{
				Name: "push",
				Steps: []pipeline.Step{
					{
						Name: "login",
						Use:  "ecr-login",
						With: map[string]string{"registry": "my-registry"},
					},
					{
						Name: "push-image",
						Run:  "docker push my-registry/app:latest",
					},
				},
			},
		},
	}

	resolver := &mockResolver{
		steps: map[string]*pipeline.ResolvedStepTemplate{
			"ecr-login": {
				Name: "ecr-login",
				Inputs: []pipeline.TemplateInput{
					{Name: "registry", Type: "string", Required: true},
				},
				Run: "docker login ${{ inputs.registry }}",
			},
		},
	}

	resolved, err := pipeline.ResolveTemplates(context.Background(), p, resolver)
	require.NoError(t, err)

	// The nested use: should be resolved.
	push := resolved.Steps[0]
	assert.True(t, push.IsNested())
	login := push.Steps[0]
	assert.Equal(t, "", login.Use)
	assert.Contains(t, login.Run, "my-registry")
	assert.NotContains(t, login.Run, "${{ inputs.")
}
