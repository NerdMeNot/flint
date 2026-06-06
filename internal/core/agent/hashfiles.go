package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// WorkspaceHasher implements pipeline.FileHasher by hashing files
// relative to a workspace directory.
type WorkspaceHasher struct {
	Workspace string
}

// HashFiles globs for files matching the pattern in the workspace,
// computes a SHA-256 hash over their sorted contents, and returns
// a truncated hex string.
func (h *WorkspaceHasher) HashFiles(pattern string) (string, error) {
	fullPattern := filepath.Join(h.Workspace, pattern)
	matches, err := filepath.Glob(fullPattern)
	if err != nil {
		return "", fmt.Errorf("hashFiles glob %s: %w", pattern, err)
	}

	// Filter to regular files only and sort for determinism.
	var files []string
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil || info.IsDir() {
			continue
		}
		files = append(files, m)
	}
	sort.Strings(files)

	if len(files) == 0 {
		return "empty", nil
	}

	// Hash each file's contents, then hash the concatenated per-file hashes.
	combined := sha256.New()
	for _, f := range files {
		h, err := hashFile(f)
		if err != nil {
			return "", err
		}
		combined.Write(h)
	}

	return hex.EncodeToString(combined.Sum(nil))[:16], nil
}

func hashFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("hashFiles open %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("hashFiles read %s: %w", path, err)
	}
	return h.Sum(nil), nil
}
