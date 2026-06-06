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
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/rs/zerolog/log"
)

// Cache manages cross-run dependency caching.
type Cache interface {
	// Restore downloads and extracts a cached archive into the workspace.
	// Returns (true, nil) on cache hit, (false, nil) on miss.
	Restore(ctx context.Context, key string, paths []string) (bool, error)

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

func (c *S3Cache) Save(ctx context.Context, key string, paths []string) error {
	if key == "" || len(paths) == 0 {
		return nil
	}

	var buf bytes.Buffer
	if err := compress(paths, &buf); err != nil {
		return fmt.Errorf("cache: compress: %w", err)
	}

	fsKey := c.cacheKey(key)
	client, err := c.getClient()
	if err != nil {
		return err
	}

	w, err := client.Create(ctx, fsKey)
	if err != nil {
		return fmt.Errorf("cache: create %s: %w", fsKey, err)
	}
	if _, err := buf.WriteTo(w); err != nil {
		_ = w.Close()
		return fmt.Errorf("cache: write %s: %w", fsKey, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("cache: close %s: %w", fsKey, err)
	}

	log.Info().Str("key", key).Int("bytes", buf.Len()).Msg("cache: saved")
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
