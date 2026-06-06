package engine

import (
	"context"
	"fmt"
	"path"

	"github.com/NerdMeNot/flint/pkg/forge"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	"gopkg.in/yaml.v3"
)

// forgeResolver implements pipeline.TemplateResolver using a forge provider
// to fetch file-based templates from Git repositories.
type forgeResolver struct {
	forge        forge.ForgeProvider
	repo         string // owner/repo for the current pipeline
	ref          string // commit SHA or branch
	pipelinePath string // e.g. ".flint/"
}

// ResolveStep resolves a CRD step template by name.
// Not yet supported — the pipeline_modules table lacks the command/image
// fields needed for full CRD resolution.
func (r *forgeResolver) ResolveStep(_ context.Context, name string) (*pipeline.ResolvedStepTemplate, error) {
	return nil, fmt.Errorf("CRD step template %q: not yet supported (use file references instead)", name)
}

// ResolveFile fetches a file-based template from the repository and parses it
// into a ResolvedFileTemplate containing the template's steps.
func (r *forgeResolver) ResolveFile(ctx context.Context, ref string) (*pipeline.ResolvedFileTemplate, error) {
	if r.forge == nil {
		return nil, fmt.Errorf("forge provider not configured")
	}

	parsed := pipeline.ParseUseRef(ref)

	var rawYAML []byte
	var err error

	switch parsed.Kind {
	case pipeline.UseRefLocal:
		// Local paths are relative to the pipeline directory.
		filePath := path.Join(r.pipelinePath, parsed.Path)
		rawYAML, err = r.forge.GetFile(ctx, r.repo, r.ref, filePath)

	case pipeline.UseRefCrossRepo:
		repo := parsed.Org + "/" + parsed.Repo
		rawYAML, err = r.forge.GetFile(ctx, repo, parsed.Ref, parsed.File)

	default:
		return nil, fmt.Errorf("unsupported use ref: %s", ref)
	}

	if err != nil {
		return nil, fmt.Errorf("fetch template %s: %w", ref, err)
	}

	// Template files are step fragments — they don't have triggers.
	// Parse as a partial pipeline (just the steps array).
	var tmpl struct {
		Steps  []pipeline.Step          `yaml:"steps"`
		Inputs []pipeline.TemplateInput `yaml:"inputs"`
	}
	if err := yaml.Unmarshal(rawYAML, &tmpl); err != nil {
		return nil, fmt.Errorf("parse template %s: %w", ref, err)
	}

	return &pipeline.ResolvedFileTemplate{
		Source: ref,
		Inputs: tmpl.Inputs,
		Steps:  tmpl.Steps,
	}, nil
}
