package pipeline

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// inputPattern matches ${{ inputs.NAME }} expressions for template substitution.
var inputPattern = regexp.MustCompile(`\$\{\{\s*inputs\.(\S+?)\s*\}\}`)

// ---------------------------------------------------------------------------
// Template Resolver Interface
// ---------------------------------------------------------------------------

// TemplateResolver resolves use: references to their definitions.
// The pipeline package defines this interface — callers (engine, CLI) provide
// implementations that know how to fetch from K8s, git, or the local filesystem.
type TemplateResolver interface {
	// ResolveStep resolves a single-step template (StepTemplate CRD).
	// ref is a plain name like "ecr-login".
	ResolveStep(ctx context.Context, name string) (*ResolvedStepTemplate, error)

	// ResolveFile resolves a file-based template (local or cross-repo).
	// ref is a path like "./fragments/setup.yaml" or "acme/templates/go-build.yaml@v1".
	ResolveFile(ctx context.Context, ref string) (*ResolvedFileTemplate, error)
}

// ResolvedStepTemplate is a resolved StepTemplate CRD.
type ResolvedStepTemplate struct {
	Name   string
	Inputs []TemplateInput
	Image  string
	Run    string
}

// ResolvedFileTemplate is a resolved file containing steps.
type ResolvedFileTemplate struct {
	Source string // description for error messages
	Inputs []TemplateInput
	Steps  []Step
}

// TemplateInput defines an input parameter for a template.
type TemplateInput struct {
	Name        string
	Type        string // string, boolean, choice
	Required    bool
	Default     string
	Description string
	Options     []string // valid values for type: choice
}

// ---------------------------------------------------------------------------
// Use Reference Parsing
// ---------------------------------------------------------------------------

// UseRefKind classifies the type of a use: reference.
type UseRefKind int

const (
	UseRefCRD       UseRefKind = iota // plain name — StepTemplate CRD
	UseRefLocal                       // starts with ./ — local file
	UseRefCrossRepo                   // org/repo/path@ref — cross-repo
)

// UseRef is a parsed use: reference, classified by source type (CRD, local file, or cross-repo).
type UseRef struct {
	Kind UseRefKind
	Raw  string // original string

	// For CRD references:
	Name string

	// For local file references:
	Path string

	// For cross-repo references:
	Org  string
	Repo string
	File string
	Ref  string // git tag, branch, or SHA
}

// ParseUseRef classifies and parses a use: reference string.
func ParseUseRef(ref string) UseRef {
	ref = strings.TrimSpace(ref)

	// Local file: starts with ./
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") {
		return UseRef{
			Kind: UseRefLocal,
			Raw:  ref,
			Path: ref,
		}
	}

	// Cross-repo: contains @ and at least two path segments (org/repo/path@ref)
	if atIdx := strings.LastIndex(ref, "@"); atIdx > 0 {
		path := ref[:atIdx]
		gitRef := ref[atIdx+1:]

		parts := strings.SplitN(path, "/", 3)
		if len(parts) >= 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" && gitRef != "" {
			return UseRef{
				Kind: UseRefCrossRepo,
				Raw:  ref,
				Org:  parts[0],
				Repo: parts[1],
				File: parts[2],
				Ref:  gitRef,
			}
		}
	}

	// Default: CRD template name
	return UseRef{
		Kind: UseRefCRD,
		Raw:  ref,
		Name: ref,
	}
}

// malformedCrossRepoRef reports a diagnostic message if ref looks like a
// cross-repo reference (it contains '@') but is missing required parts. It
// mirrors ParseUseRef's cross-repo rule so that a ref ParseUseRef silently
// downgrades to a CRD-name lookup (e.g. "//path@", "org/@v1") is surfaced as an
// error instead. Returns "" for valid or non-cross-repo refs.
func malformedCrossRepoRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") {
		return "" // local file
	}
	atIdx := strings.LastIndex(ref, "@")
	if atIdx <= 0 {
		return "" // no '@' → plain CRD name, not a cross-repo ref
	}
	path := ref[:atIdx]
	gitRef := ref[atIdx+1:]
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" || gitRef == "" {
		return fmt.Sprintf("malformed cross-repo reference %q — expected org/repo/path@ref with all parts non-empty", ref)
	}
	return ""
}

// ---------------------------------------------------------------------------
// Template Resolution
// ---------------------------------------------------------------------------

// maxResolveDepth limits how deeply templates can be nested to prevent
// infinite recursion from circular template references.
const maxResolveDepth = 10 // Prevent circular template references

// ResolveTemplates walks all use: references in the pipeline and resolves them
// using the provided resolver. It returns a new pipeline with templates inlined.
// This does not modify the original pipeline.
//
// For CRD step templates, the use: step is replaced with a run: step using
// the template's image and command, with inputs substituted.
//
// For file-based templates, the use: step is replaced with the file's steps
// inlined as nested sub-steps (making it a nested step group).
func ResolveTemplates(ctx context.Context, p *Pipeline, resolver TemplateResolver) (*Pipeline, error) {
	if resolver == nil {
		return p, nil
	}

	resolved := *p
	resolved.Steps = make([]Step, len(p.Steps))
	copy(resolved.Steps, p.Steps)

	for i := range resolved.Steps {
		step, err := resolveStep(ctx, &resolved.Steps[i], resolver, 0)
		if err != nil {
			return nil, err
		}
		resolved.Steps[i] = *step
	}

	return &resolved, nil
}

func resolveStep(ctx context.Context, s *Step, resolver TemplateResolver, depth int) (*Step, error) {
	if depth > maxResolveDepth {
		return nil, &ParseError{
			Field:   s.Name,
			Message: fmt.Sprintf("template resolution exceeds maximum depth of %d — possible circular reference", maxResolveDepth),
			Err:     ErrInvalidPipeline,
		}
	}

	if s.Use == "" {
		// Resolve sub-steps if nested.
		if s.IsNested() {
			resolved := *s
			resolved.Steps = make([]Step, len(s.Steps))
			for i := range s.Steps {
				sub, err := resolveStep(ctx, &s.Steps[i], resolver, depth+1)
				if err != nil {
					return nil, err
				}
				resolved.Steps[i] = *sub
			}
			return &resolved, nil
		}
		return s, nil
	}

	// Built-in templates — resolved without a CRD or file lookup.
	if resolved, ok := resolveBuiltin(s); ok {
		return resolved, nil
	}

	ref := ParseUseRef(s.Use)

	switch ref.Kind {
	case UseRefCRD:
		return resolveCRDTemplate(ctx, s, ref, resolver)
	case UseRefLocal, UseRefCrossRepo:
		return resolveFileTemplate(ctx, s, ref, resolver, depth)
	default:
		return s, nil
	}
}

func resolveCRDTemplate(ctx context.Context, s *Step, ref UseRef, resolver TemplateResolver) (*Step, error) {
	tmpl, err := resolver.ResolveStep(ctx, ref.Name)
	if err != nil {
		return nil, &ParseError{
			Field:   s.Name + ".use",
			Message: "failed to resolve step template " + ref.Name,
			Err:     err,
		}
	}

	// Validate inputs: check required inputs are provided, apply defaults.
	inputValues, err := resolveInputs(s.Name, s.With, tmpl.Inputs)
	if err != nil {
		return nil, err
	}

	// Build a run step from the template, substituting input expressions.
	resolved := *s
	resolved.Use = ""
	resolved.With = nil

	// Substitute ${{ inputs.NAME }} in the template's fields. A StepTemplate CRD
	// only exposes Run and Image, so those are the only template-provided fields
	// that can reference inputs (the use-step's own fields are not substituted —
	// template inputs are internal to the template).
	resolved.Run = Cmd(substituteInputs(tmpl.Run, inputValues))

	if tmpl.Image != "" && resolved.Image == "" {
		resolved.Image = substituteInputs(tmpl.Image, inputValues)
	}

	return &resolved, nil
}

// resolveBuiltin handles built-in step templates that don't require a CRD or
// file lookup. Returns (resolved, true) if the use: ref matches a built-in,
// or (nil, false) otherwise.
//
// Built-in templates:
//   - "checkout" — clones the repository (replaces implicit init-container clone)
func resolveBuiltin(s *Step) (*Step, bool) {
	switch s.Use {
	case "checkout":
		resolved := Step{
			Name: s.Name,
			// The engine dispatches this as a run step with the agent image.
			// The command runs `flint-agent checkout` with step inputs passed
			// as FLINT_CHECKOUT_INPUTS env var.
			Run:  Cmd("flint-agent checkout"),
			With: s.With,
			// Preserve step-level overrides.
			Image:          s.Image,
			ServiceAccount: s.ServiceAccount,
			Runner:         s.Runner,
			Timeout:        s.Timeout,
			If:             s.If,
			When:           s.When,
			Env:            s.Env,
		}
		if resolved.Name == "" {
			resolved.Name = "checkout"
		}
		return &resolved, true
	default:
		return nil, false
	}
}

func resolveFileTemplate(ctx context.Context, s *Step, ref UseRef, resolver TemplateResolver, depth int) (*Step, error) {
	tmpl, err := resolver.ResolveFile(ctx, ref.Raw)
	if err != nil {
		return nil, &ParseError{
			Field:   s.Name + ".use",
			Message: "failed to resolve file template " + ref.Raw,
			Err:     err,
		}
	}

	// Validate the template has steps.
	if len(tmpl.Steps) == 0 {
		return nil, &ParseError{
			Field:   s.Name + ".use",
			Message: "file template " + ref.Raw + " contains no steps",
			Err:     ErrInvalidPipeline,
		}
	}

	// Validate inputs if the file template declares them.
	inputValues, err := resolveInputs(s.Name, s.With, tmpl.Inputs)
	if err != nil {
		return nil, err
	}

	// Substitute inputs across all string fields in each sub-step.
	resolvedSteps := make([]Step, len(tmpl.Steps))
	copy(resolvedSteps, tmpl.Steps)
	for i := range resolvedSteps {
		substituteStepInputs(&resolvedSteps[i], inputValues)
	}

	// Re-resolve sub-steps so nested use: references inside the template are
	// expanded too. depth+1 bounds mutual template references against
	// maxResolveDepth (otherwise a template that references itself loops forever).
	for i := range resolvedSteps {
		sub, err := resolveStep(ctx, &resolvedSteps[i], resolver, depth+1)
		if err != nil {
			return nil, err
		}
		resolvedSteps[i] = *sub
	}

	// Convert to a nested step — the file's steps become sub-steps.
	resolved := *s
	resolved.Use = ""
	resolved.With = nil
	resolved.Steps = resolvedSteps

	return &resolved, nil
}

// ---------------------------------------------------------------------------
// Input resolution and substitution
// ---------------------------------------------------------------------------

// resolveInputs validates provided inputs against the template's input spec,
// applies defaults for missing optional inputs, and returns the final map.
func resolveInputs(stepName string, provided map[string]string, spec []TemplateInput) (map[string]string, error) {
	result := make(map[string]string)

	// Build a set of declared input names for unknown input detection.
	declared := make(map[string]bool, len(spec))
	for _, input := range spec {
		declared[input.Name] = true
	}

	// Check for unknown inputs provided by the pipeline.
	for k := range provided {
		if !declared[k] {
			return nil, &ParseError{
				Field:   stepName + ".with." + k,
				Message: fmt.Sprintf("unknown template input %q", k),
				Err:     ErrInvalidPipeline,
			}
		}
	}

	// Resolve each declared input with type validation.
	for _, input := range spec {
		if val, ok := provided[input.Name]; ok {
			// Type validation.
			if err := validateInputValue(stepName, input, val); err != nil {
				return nil, err
			}
			result[input.Name] = val
		} else if input.Default != "" {
			result[input.Name] = input.Default
		} else if input.Required {
			return nil, &ParseError{
				Field:   stepName + ".with",
				Message: fmt.Sprintf("required template input %q is not provided", input.Name),
				Err:     ErrInvalidPipeline,
			}
		}
		// Optional inputs without a default and not provided are simply absent.
	}

	return result, nil
}

// validateInputValue checks that a provided value matches the declared input type.
func validateInputValue(stepName string, input TemplateInput, value string) error {
	switch input.Type {
	case "boolean":
		if value != "true" && value != "false" {
			return &ParseError{
				Field:   stepName + ".with." + input.Name,
				Message: fmt.Sprintf("boolean input %q must be \"true\" or \"false\", got %q", input.Name, value),
				Err:     ErrInvalidPipeline,
			}
		}
	case "choice":
		if len(input.Options) > 0 {
			found := false
			for _, opt := range input.Options {
				if opt == value {
					found = true
					break
				}
			}
			if !found {
				return &ParseError{
					Field:   stepName + ".with." + input.Name,
					Message: fmt.Sprintf("choice input %q value %q is not in options %v", input.Name, value, input.Options),
					Err:     ErrInvalidPipeline,
				}
			}
		}
	case "string", "":
		// No constraints on string values.
	}
	return nil
}

// substituteInputs replaces all ${{ inputs.NAME }} expressions in a string
// with the corresponding values from the input map.
func substituteInputs(s string, inputs map[string]string) string {
	if len(inputs) == 0 {
		return s
	}

	return inputPattern.ReplaceAllStringFunc(s, func(match string) string {
		sub := inputPattern.FindStringSubmatch(match)
		if len(sub) != 2 {
			return match
		}
		name := strings.TrimSpace(sub[1])
		if val, ok := inputs[name]; ok {
			return val
		}
		return match
	})
}

// substituteStepInputs replaces ${{ inputs.NAME }} across all string fields of a step.
func substituteStepInputs(s *Step, inputs map[string]string) {
	if len(inputs) == 0 {
		return
	}

	for i, cmd := range s.Run.Commands {
		s.Run.Commands[i] = substituteInputs(cmd, inputs)
	}
	s.Image = substituteInputs(s.Image, inputs)
	s.WorkingDir = substituteInputs(s.WorkingDir, inputs)
	s.Timeout = substituteInputs(s.Timeout, inputs)

	// Env values.
	if len(s.Env) > 0 {
		newEnv := make(map[string]string, len(s.Env))
		for k, v := range s.Env {
			newEnv[k] = substituteInputs(v, inputs)
		}
		s.Env = newEnv
	}

	// Services image and env.
	for i := range s.Services {
		s.Services[i].Image = substituteInputs(s.Services[i].Image, inputs)
		if len(s.Services[i].Env) > 0 {
			newEnv := make(map[string]string, len(s.Services[i].Env))
			for k, v := range s.Services[i].Env {
				newEnv[k] = substituteInputs(v, inputs)
			}
			s.Services[i].Env = newEnv
		}
	}

	// Cache key.
	if s.Cache != nil {
		s.Cache.Key = substituteInputs(s.Cache.Key, inputs)
	}

	// With values (for nested use: references).
	if len(s.With) > 0 {
		newWith := make(map[string]string, len(s.With))
		for k, v := range s.With {
			newWith[k] = substituteInputs(v, inputs)
		}
		s.With = newWith
	}
}
