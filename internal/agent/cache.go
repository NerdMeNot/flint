package agent

import (
	"fmt"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// EvaluateCacheKey interpolates the cache key expression with runtime context.
// The key typically includes hashFiles() which reads from the workspace.
// Must be called AFTER checkout so the files exist.
func EvaluateCacheKey(cfg *Config) (string, error) {
	hasher := &WorkspaceHasher{Workspace: cfg.Workspace}
	exprCtx := pipeline.BuildRuntimeContext(pipeline.RuntimeContextOpts{
		Branch:     cfg.GitRef,
		CommitSha:  cfg.GitSHA,
		FileHasher: hasher,
	})

	result, err := pipeline.Interpolate(cfg.CacheKey, exprCtx)
	if err != nil {
		return "", fmt.Errorf("evaluate cache key: %w", err)
	}
	return result, nil
}
