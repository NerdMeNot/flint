// Package cache provides cross-run dependency caching for pipeline steps.
//
// Cache entries are stored as tar.zst archives in S3 with structured keys:
//
//	cache/{orgID}/{projectID}/{key}.tar.zst
//
// The cache key is an expression evaluated at runtime, typically including
// a file hash: npm-${{ hashFiles('package-lock.json') }}
//
// IMPORTANT: Cache key evaluation must happen AFTER checkout — not before.
// Evaluating hashFiles on an empty workspace always returns "empty", causing
// every run to miss the cache.
package cache

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/NerdMeNot/flint/pkg/wsfs"
	"github.com/klauspost/compress/zstd"
	"github.com/rs/zerolog/log"
)

// Cache manages cross-run dependency caching.
type Cache interface {
	// Restore downloads and extracts a cached archive into the workspace.
	// Returns (true, nil) on cache hit, (false, nil) on miss.
	Restore(ctx context.Context, key string, paths []string) (bool, error)

	// RestoreWithFallback tries the exact key, then each restore key as a
	// PREFIX (newest — lexicographically last — match wins). Returns the key
	// that hit, or "" on a full miss.
	RestoreWithFallback(ctx context.Context, key string, restoreKeys []string, paths []string) (string, error)

	// Save compresses the given paths and stores them under the key.
	Save(ctx context.Context, key string, paths []string) error
}

// S3Cache is an S3-backed cache store.
type S3Cache struct {
	orgID     string
	projectID string
	bucket    string
	region    string
}

// NewS3 creates an S3-backed cache store scoped to an org and project.
func NewS3(orgID, projectID, bucket, region string) *S3Cache {
	return &S3Cache{
		orgID:     orgID,
		projectID: projectID,
		bucket:    bucket,
		region:    region,
	}
}

func (c *S3Cache) Restore(ctx context.Context, key string, paths []string) (bool, error) {
	if key == "" || len(paths) == 0 {
		return false, nil
	}

	fsKey := c.cacheKey(key)
	client, err := c.getClient()
	if err != nil {
		return false, err
	}

	r, err := client.Open(ctx, fsKey)
	if err != nil {
		// Cache miss is normal, not an error.
		log.Info().Str("key", key).Msg("cache: miss")
		return false, nil
	}
	defer r.Close()

	if err := extract(r); err != nil {
		return false, fmt.Errorf("cache: extract %s: %w", key, err)
	}

	log.Info().Str("key", key).Msg("cache: restored")
	return true, nil
}

// RestoreWithFallback tries the exact key first, then each restore key as a
// prefix over the project's cache entries. A prefix hit restores stale-but-
// close dependencies so the build only pays the delta.
func (c *S3Cache) RestoreWithFallback(ctx context.Context, key string, restoreKeys []string, paths []string) (string, error) {
	if hit, err := c.Restore(ctx, key, paths); err != nil || hit {
		return key, err
	}
	if len(restoreKeys) == 0 {
		return "", nil
	}

	client, err := c.getClient()
	if err != nil {
		return "", err
	}
	entries, err := client.ReadDir(ctx, fmt.Sprintf("cache/%s/%s", c.orgID, c.projectID))
	if err != nil {
		log.Warn().Err(err).Msg("cache: restore-keys listing failed (treating as miss)")
		return "", nil
	}

	for _, prefix := range restoreKeys {
		best := ""
		for _, e := range entries {
			name := strings.TrimSuffix(e.Name, ".tar.zst")
			if e.IsDir || !strings.HasPrefix(name, prefix) {
				continue
			}
			// Without object mtimes in the listing, the lexicographically
			// last match is the deterministic tie-break.
			if name > best {
				best = name
			}
		}
		if best == "" {
			continue
		}
		hit, err := c.Restore(ctx, best, paths)
		if err != nil {
			return "", err
		}
		if hit {
			log.Info().Str("prefix", prefix).Str("key", best).Msg("cache: restore-key fallback hit")
			return best, nil
		}
	}
	return "", nil
}

func (c *S3Cache) Save(ctx context.Context, key string, paths []string) error {
	if key == "" || len(paths) == 0 {
		return nil
	}

	fsKey := c.cacheKey(key)
	client, err := c.getClient()
	if err != nil {
		return err
	}

	// Content-addressed keys (hashFiles) mean an existing entry is identical —
	// skip the re-compress + re-upload entirely. This turns the common
	// warm-cache case from "tar+upload every run" into one HEAD request.
	if _, err := client.Stat(ctx, fsKey); err == nil {
		log.Info().Str("key", key).Msg("cache: entry exists, skipping save")
		return nil
	}

	w, err := client.Create(ctx, fsKey)
	if err != nil {
		return fmt.Errorf("cache: create %s: %w", fsKey, err)
	}
	// Stream the archive into the writer — no full in-memory staging buffer
	// (the sidecar runs with a small memory request).
	if err := compress(paths, w); err != nil {
		_ = w.Close()
		return fmt.Errorf("cache: compress: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("cache: close %s: %w", fsKey, err)
	}

	log.Info().Str("key", key).Msg("cache: saved")
	return nil
}

func (c *S3Cache) cacheKey(evaluatedKey string) string {
	return fmt.Sprintf("cache/%s/%s/%s.tar.zst", c.orgID, c.projectID, evaluatedKey)
}

func (c *S3Cache) getClient() (s3Client, error) {
	return newS3Client(c.bucket, c.region)
}

// s3Client is the minimal interface for S3 operations.
type s3Client interface {
	Create(ctx context.Context, name string) (io.WriteCloser, error)
	Open(ctx context.Context, name string) (io.ReadCloser, error)
	Stat(ctx context.Context, name string) (wsfs.FileInfo, error)
	ReadDir(ctx context.Context, dir string) ([]wsfs.DirEntry, error)
}

// ─────────────────────────────────────────────────────────────
// tar.zst helpers (self-contained, no dependency on agent/tarutil)
// ─────────────────────────────────────────────────────────────

func compress(paths []string, w io.Writer) error {
	zw, err := zstd.NewWriter(w)
	if err != nil {
		return err
	}
	defer zw.Close()

	tw := tar.NewWriter(zw)
	defer tw.Close()

	for _, p := range paths {
		p = filepath.Clean(p)
		if err := addToTar(tw, p); err != nil {
			return err
		}
	}
	return nil
}

func extract(r io.Reader) error {
	zr, err := zstd.NewReader(r)
	if err != nil {
		return err
	}
	defer zr.Close()

	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Clean(hdr.Name)
		if strings.Contains(target, "..") {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(target, os.FileMode(hdr.Mode))
		case tar.TypeReg:
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, tr)
			f.Close()
			if copyErr != nil {
				return copyErr
			}
		}
	}
	return nil
}

func addToTar(tw *tar.Writer, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = path
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
}
