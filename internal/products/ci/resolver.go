package ci

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NerdMeNot/flint/pkg/forge"
	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// osReadFile reads a repo-relative file, refusing path escapes.
func osReadFile(root, rel string) ([]byte, error) {
	full := filepath.Join(root, filepath.Clean(rel))
	if !strings.HasPrefix(full, filepath.Clean(root)+string(filepath.Separator)) {
		return nil, fmt.Errorf("path escapes repository root")
	}
	return os.ReadFile(full)
}

// resolver.go — the production ModuleResolver. Reference forms:
//
//	checkout                  — built-in (no fetch)
//	./ci/modules/go-test.yaml — a file in the SAME repo at the run's commit
//	org/repo/path@ref         — a file in another repo at an immutable ref
//
// Anything else is an unknown module — a loud error, per the policy that a
// reference must resolve or refuse.

// builtinModules are step templates that ship with Flint and need no fetch.
// checkout clones the repository using the flint-agent binary the init
// container installed into the workspace (static binary, runs in any image;
// clone uses go-git so the image needs no git).
var builtinModules = map[string]*Module{
	"checkout": {
		Name: "checkout",
		Kind: "steps",
		Inputs: map[string]Input{
			"ref":   {Type: "string"},
			"path":  {Type: "string"},
			"token": {Type: "string"},
			"depth": {Type: "string"},
		},
		Steps: []Step{{
			Name: "checkout",
			Run: pipeline.Cmd(`FLINT_CHECKOUT_INPUTS='{"ref":"${{ inputs.ref }}","path":"${{ inputs.path }}","token":"${{ inputs.token }}","depth":"${{ inputs.depth }}"}' ` +
				`/workspace/.flint-bin/flint-agent checkout`),
		}},
	},
}

// ForgeResolver resolves module references from repositories via the forge.
type ForgeResolver struct {
	forge forge.ForgeProvider
	// repo/sha anchor local ./ references to the consuming pipeline's commit,
	// so a pipeline and its in-repo modules always version together.
	repo string
	sha  string
}

// NewForgeResolver builds a resolver for a pipeline fetched from repo at sha.
func NewForgeResolver(fg forge.ForgeProvider, repo, sha string) *ForgeResolver {
	return &ForgeResolver{forge: fg, repo: repo, sha: sha}
}

// Resolve fetches and parses the referenced module.
func (r *ForgeResolver) Resolve(ctx context.Context, ref string) (*Module, error) {
	if m, ok := builtinModules[ref]; ok {
		return m, nil
	}
	if r.forge == nil {
		return nil, fmt.Errorf("ci: module %q cannot be resolved (no forge configured)", ref)
	}

	repo, gitRef, path, err := splitModuleRef(ref, r.repo, r.sha)
	if err != nil {
		return nil, err
	}
	raw, err := r.forge.GetFile(ctx, repo, gitRef, path)
	if err != nil {
		return nil, fmt.Errorf("ci: fetch module %q: %w", ref, err)
	}
	m, err := ParseModule(raw)
	if err != nil {
		return nil, fmt.Errorf("ci: module %q: %w", ref, err)
	}
	return m, nil
}

// splitModuleRef parses a module reference into (repo, gitRef, path).
//
//	./path/mod.yaml   → (consumerRepo, consumerSHA, path/mod.yaml)
//	org/repo/path@ref → (org/repo, ref, path)
func splitModuleRef(ref, consumerRepo, consumerSHA string) (repo, gitRef, path string, err error) {
	if strings.HasPrefix(ref, "./") {
		return consumerRepo, consumerSHA, strings.TrimPrefix(ref, "./"), nil
	}
	raw, gitRef, found := strings.Cut(ref, "@")
	if !found || gitRef == "" {
		return "", "", "", fmt.Errorf("ci: module ref %q — use a built-in name, ./local/path.yaml, or org/repo/path@ref", ref)
	}
	parts := strings.SplitN(raw, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("ci: malformed cross-repo module ref %q — expected org/repo/path@ref with all parts non-empty", ref)
	}
	return parts[0] + "/" + parts[1], gitRef, parts[2], nil
}

// FileResolver resolves built-ins and in-repo ./ references from the local
// filesystem — the CLI's offline resolver for `flint validate`. Cross-repo
// references cannot be resolved offline and error with a pointer.
type FileResolver struct {
	// Root is the repository root local ./ references are relative to.
	Root string
}

// Resolve reads the referenced module from disk.
func (r *FileResolver) Resolve(_ context.Context, ref string) (*Module, error) {
	if m, ok := builtinModules[ref]; ok {
		return m, nil
	}
	if strings.HasPrefix(ref, "./") {
		raw, err := osReadFile(r.Root, strings.TrimPrefix(ref, "./"))
		if err != nil {
			return nil, fmt.Errorf("ci: module %q: %w", ref, err)
		}
		m, err := ParseModule(raw)
		if err != nil {
			return nil, fmt.Errorf("ci: module %q: %w", ref, err)
		}
		return m, nil
	}
	if strings.Contains(ref, "@") {
		return nil, fmt.Errorf("ci: module %q is a cross-repo reference — it resolves at run time, not offline (validate on the server or vendor the module locally)", ref)
	}
	return nil, fmt.Errorf("ci: unknown module %q (built-ins: checkout; local: ./path.yaml; cross-repo: org/repo/path@ref)", ref)
}

// hasModuleRefs reports whether the pipeline references any module (so the
// resolution pass can be skipped entirely for the common case).
func hasModuleRefs(p *Pipeline) bool {
	if p.Extends != "" {
		return true
	}
	for _, job := range p.Jobs {
		if job.Use != "" {
			return true
		}
		for _, s := range job.Steps {
			if s.Use != "" {
				return true
			}
		}
	}
	return false
}
