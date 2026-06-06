// Package artifact provides explicit artifact storage for passing named files
// between pipeline steps. Artifacts are separate from workspace sync — they
// represent explicitly declared outputs that downstream steps consume by name.
//
// Artifacts are stored as tar.zst archives in S3 with structured keys:
//
//	artifacts/{orgID}/{runID}/{stepName}/{name}.tar.zst
//
// The key structure prevents collisions between steps, runs, and orgs.
package artifact

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

// Ref identifies an artifact by its origin and name.
type Ref struct {
	OrgID    string
	RunID    string
	StepName string
	Name     string // artifact name declared in the pipeline
}

// Input describes an artifact to download before a step runs.
type Input struct {
	From string `json:"from"` // source step name
	Name string `json:"name"` // artifact name
	Path string `json:"path"` // local path to extract to
}

// Output describes an artifact to upload after a step succeeds.
type Output struct {
	Name string `json:"name"` // artifact name
	Path string `json:"path"` // local path to compress
}

// Store is the interface for artifact persistence. Implementations handle
// compression, key derivation, and storage.
type Store interface {
	Upload(ctx context.Context, ref Ref, localPath string) error
	Download(ctx context.Context, ref Ref, extractPath string) error
}

// S3Store is an S3-backed artifact store.
type S3Store struct {
	bucket string
	region string
	newS3  func(bucket, region string) (s3Client, error) // injectable for testing
}

// s3Client is the minimal interface needed from wsfs.S3FS.
type s3Client interface {
	Create(ctx context.Context, name string) (io.WriteCloser, error)
	Open(ctx context.Context, name string) (io.ReadCloser, error)
}

// NewS3Store creates an S3-backed artifact store.
func NewS3Store(bucket, region string) *S3Store {
	return &S3Store{bucket: bucket, region: region}
}

func (s *S3Store) Upload(ctx context.Context, ref Ref, localPath string) error {
	key := artifactKey(ref)

	var buf bytes.Buffer
	if err := compress(localPath, &buf); err != nil {
		return fmt.Errorf("artifact: compress %s: %w", localPath, err)
	}

	client, err := s.getClient()
	if err != nil {
		return err
	}

	w, err := client.Create(ctx, key)
	if err != nil {
		return fmt.Errorf("artifact: create %s: %w", key, err)
	}
	if _, err := buf.WriteTo(w); err != nil {
		_ = w.Close()
		return fmt.Errorf("artifact: write %s: %w", key, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("artifact: close %s: %w", key, err)
	}

	log.Info().Str("key", key).Int("bytes", buf.Len()).Msg("artifact: uploaded")
	return nil
}

func (s *S3Store) Download(ctx context.Context, ref Ref, extractPath string) error {
	key := artifactKey(ref)

	client, err := s.getClient()
	if err != nil {
		return err
	}

	r, err := client.Open(ctx, key)
	if err != nil {
		return fmt.Errorf("artifact: open %s: %w", key, err)
	}
	defer r.Close()

	if err := extract(r, extractPath); err != nil {
		return fmt.Errorf("artifact: extract %s: %w", key, err)
	}

	log.Info().Str("key", key).Str("extractPath", extractPath).Msg("artifact: downloaded")
	return nil
}

func (s *S3Store) getClient() (s3Client, error) {
	if s.newS3 != nil {
		return s.newS3(s.bucket, s.region)
	}
	// Use wsfs.S3FS with an artifacts/ prefix to isolate from workspace data.
	wsfsS3, err := newWSFSS3(s.bucket, s.region)
	if err != nil {
		return nil, fmt.Errorf("artifact: init S3 client: %w", err)
	}
	return wsfsS3, nil
}

// artifactKey returns the storage key. Uses the artifact Name (not the file
// path) to prevent the collision bug where /foo/bar and foo/bar produce the
// same key.
func artifactKey(ref Ref) string {
	return fmt.Sprintf("artifacts/%s/%s/%s/%s.tar.zst", ref.OrgID, ref.RunID, ref.StepName, ref.Name)
}

// ─────────────────────────────────────────────────────────────
// tar.zst compression / extraction
// ─────────────────────────────────────────────────────────────

func compress(root string, w io.Writer) error {
	zw, err := zstd.NewWriter(w)
	if err != nil {
		return err
	}
	defer zw.Close()

	tw := tar.NewWriter(zw)
	defer tw.Close()

	root = filepath.Clean(root)
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Store paths relative to root for portable extraction.
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel

		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			header.Linkname = link
			header.Typeflag = tar.TypeSymlink
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
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

func extract(r io.Reader, destDir string) error {
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

		target := filepath.Join(destDir, filepath.Clean(hdr.Name))
		// Security: prevent path traversal.
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
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
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			_ = os.Remove(target)
			_ = os.Symlink(hdr.Linkname, target)
		}
	}
	return nil
}

// Bridge to wsfs.S3FS — avoids importing the whole wsfs package into the
// artifact package's public API.
func newWSFSS3(bucket, region string) (s3Client, error) {
	// Import wsfs at the implementation level, not the interface level.
	// This keeps the artifact package loosely coupled to the storage backend.
	return newWSFSS3Impl(bucket, region)
}
