package engine

import (
	"context"
	"fmt"
	"path"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"gopkg.in/yaml.v3"
)

// FileGetter fetches file bytes from a source repository at a ref. The engine
// depends only on this to load pipeline definitions and file-based templates —
// not on the full forge.ForgeProvider (webhooks, clone URLs, commit status, …).
// forge.ForgeProvider satisfies it, so the CI product passes its forge directly.
type FileGetter interface {
	GetFile(ctx context.Context, repo, ref, path string) ([]byte, error)
}

// fileResolver implements pipeline.TemplateResolver using a FileGetter to fetch
// file-based templates from source repositories.
type fileResolver struct {
	files        FileGetter
	repo         string // owner/repo for the current pipeline
	ref          string // commit SHA or branch
	pipelinePath string // e.g. ".flint/"
}

// ResolveStep resolves a CRD step template by name.
// Not yet supported — the pipeline_modules table lacks the command/image
// fields needed for full CRD resolution.
func (r *fileResolver) ResolveStep(_ context.Context, name string) (*pipeline.ResolvedStepTemplate, error) {
	return nil, fmt.Errorf("CRD step template %q: not yet supported (use file references instead)", name)
}

// ResolveFile fetches a file-based template from the repository and parses it
// into a ResolvedFileTemplate containing the template's steps.
func (r *fileResolver) ResolveFile(ctx context.Context, ref string) (*pipeline.ResolvedFileTemplate, error) {
	if r.files == nil {
		return nil, fmt.Errorf("file getter not configured")
	}

	parsed := pipeline.ParseUseRef(ref)

	var rawYAML []byte
	var err error

	switch parsed.Kind {
	case pipeline.UseRefLocal:
		// Local paths are relative to the pipeline directory.
		filePath := path.Join(r.pipelinePath, parsed.Path)
		rawYAML, err = r.files.GetFile(ctx, r.repo, r.ref, filePath)

	case pipeline.UseRefCrossRepo:
		repo := parsed.Org + "/" + parsed.Repo
		rawYAML, err = r.files.GetFile(ctx, repo, parsed.Ref, parsed.File)

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
