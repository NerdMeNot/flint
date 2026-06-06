package workspace

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/zeebo/xxh3"
)

// ManifestEntry describes a single file in a workspace manifest.
type ManifestEntry struct {
	Hash  string `json:"hash"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

// Manifest maps relative file paths (forward-slash separated) to their entry.
type Manifest map[string]ManifestEntry

// defaultExcludes lists patterns that are never synced.
var defaultExcludes = []string{
	".git/**",
	".git",
	".flint-step.log",
	".flint-exit",
	".flint-emit",
}

// ComputeManifest walks root and builds a Manifest for all non-excluded files.
// Paths in the returned Manifest are relative to root, using forward slashes.
func ComputeManifest(root string) (Manifest, error) {
	excludes := append(defaultExcludes, loadFlintignore(root)...)
	m := make(Manifest)

	err := filepath.WalkDir(root, func(absPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, absPath)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		if rel == "." {
			return nil
		}

		if excluded(rel, excludes) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		hash, err := hashFile(absPath)
		if err != nil {
			return fmt.Errorf("hash %q: %w", rel, err)
		}

		m[rel] = ManifestEntry{
			Hash:  hash,
			Size:  info.Size(),
			Mtime: info.ModTime().Unix(),
		}
		return nil
	})

	return m, err
}

// Diff computes what changed between a local and remote manifest.
// Returns files to upload (new/changed), files to download (missing locally),
// and files to remove (deleted locally but still remote).
func Diff(local, remote Manifest) (upload, download []string, remove []string) {
	// Files to upload: in local but not in remote, or hash differs.
	for path, localEntry := range local {
		remoteEntry, exists := remote[path]
		if !exists || remoteEntry.Hash != localEntry.Hash {
			upload = append(upload, path)
		}
	}

	// Files to download: in remote but not in local, or hash differs.
	for path, remoteEntry := range remote {
		localEntry, exists := local[path]
		if !exists || localEntry.Hash != remoteEntry.Hash {
			download = append(download, path)
		}
	}

	// Files to remove: in remote but not in local.
	for path := range remote {
		if _, exists := local[path]; !exists {
			remove = append(remove, path)
		}
	}

	return upload, download, remove
}

// hashFile computes the xxh3 hex digest of the file at path.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := xxh3.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum64()), nil
}

// excluded reports whether rel matches any of the given glob patterns.
func excluded(rel string, patterns []string) bool {
	for _, pat := range patterns {
		ok, _ := doublestar.Match(pat, rel)
		if ok {
			return true
		}
	}
	return false
}

// loadFlintignore reads .flintignore from the workspace root.
func loadFlintignore(root string) []string {
	path := filepath.Join(root, ".flintignore")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}
