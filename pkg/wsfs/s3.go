package wsfs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3FS is an S3-backed FS implementation used for cross-run cache and as
// an optional workspace sync backend. S3 has no real directory concept;
// paths are used directly as object keys (prefixed when configured).
// MkdirAll is a no-op and ReadDir uses a list-objects call with a key prefix.
//
// S3FS is safe for concurrent use.
type S3FS struct {
	client *s3.Client
	bucket string
	prefix string // optional key prefix (e.g. "workspace/runs/{runID}/")
}

// NewS3 creates an S3FS using the default AWS credential chain.
func NewS3(bucket, region string) (*S3FS, error) {
	return NewS3WithPrefix(bucket, region, "")
}

// NewS3WithPrefix creates an S3FS scoped to a key prefix. All operations
// prepend the prefix to object keys. Use this to scope workspace storage
// per run: NewS3WithPrefix(bucket, region, "workspace/runs/{runID}/").
func NewS3WithPrefix(bucket, region, prefix string) (*S3FS, error) {
	if bucket == "" {
		return nil, fmt.Errorf("wsfs/s3: bucket must not be empty")
	}
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("wsfs/s3: load AWS config: %w", err)
	}
	return &S3FS{
		client: s3.NewFromConfig(cfg),
		bucket: bucket,
		prefix: prefix,
	}, nil
}

// key prepends the configured prefix to a path.
func (s *S3FS) key(name string) string {
	return s.prefix + name
}

// Open implements FS. Returns ErrNotExist if the key does not exist.
func (s *S3FS) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    aws.String(s.key(name)),
	})
	if err != nil {
		if isS3NotFound(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotExist, name)
		}
		return nil, fmt.Errorf("wsfs/s3: get %s: %w", name, err)
	}
	return resp.Body, nil
}

// Create implements FS. The write is buffered in memory and committed as a
// single PutObject call on Close. For large objects, prefer the workspace
// agent (RemoteFS) which streams in-cluster.
func (s *S3FS) Create(_ context.Context, name string) (io.WriteCloser, error) {
	return &s3Writer{fs: s, key: s.key(name)}, nil
}

// s3Writer buffers writes and flushes to S3 on Close.
type s3Writer struct {
	fs  *S3FS
	key string
	buf bytes.Buffer
}

func (w *s3Writer) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *s3Writer) Close() error {
	ctx := context.Background()
	_, err := w.fs.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        &w.fs.bucket,
		Key:           aws.String(w.key),
		Body:          bytes.NewReader(w.buf.Bytes()),
		ContentLength: aws.Int64(int64(w.buf.Len())),
	})
	if err != nil {
		return fmt.Errorf("wsfs/s3: put %s: %w", w.key, err)
	}
	return nil
}

// Stat implements FS. Uses HeadObject to retrieve metadata without
// downloading the body. Returns ErrNotExist if the key does not exist.
func (s *S3FS) Stat(ctx context.Context, name string) (FileInfo, error) {
	resp, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &s.bucket,
		Key:    aws.String(s.key(name)),
	})
	if err != nil {
		if isS3NotFound(err) {
			return FileInfo{}, fmt.Errorf("%w: %s", ErrNotExist, name)
		}
		return FileInfo{}, fmt.Errorf("wsfs/s3: head %s: %w", name, err)
	}
	var size int64
	if resp.ContentLength != nil {
		size = *resp.ContentLength
	}
	var modTime time.Time
	if resp.LastModified != nil {
		modTime = *resp.LastModified
	}
	// Derive the base name from the key.
	base := name
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		base = name[idx+1:]
	}
	return FileInfo{
		Name:    base,
		Size:    size,
		ModTime: modTime,
		IsDir:   false,
	}, nil
}

// ReadDir implements FS. Lists objects whose keys begin with dir/ and returns
// them as a flat list of DirEntries. Common prefixes (simulated directories)
// are included as directory entries.
func (s *S3FS) ReadDir(ctx context.Context, dir string) ([]DirEntry, error) {
	prefix := s.key(dir)
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket:    &s.bucket,
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
	})
	var entries []DirEntry
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("wsfs/s3: list %s: %w", dir, err)
		}
		// Common prefixes are simulated subdirectories.
		for _, cp := range page.CommonPrefixes {
			if cp.Prefix == nil {
				continue
			}
			name := strings.TrimSuffix(strings.TrimPrefix(*cp.Prefix, prefix), "/")
			if name != "" {
				entries = append(entries, DirEntry{Name: name, IsDir: true})
			}
		}
		// Objects are files.
		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			name := strings.TrimPrefix(*obj.Key, prefix)
			if name == "" {
				continue
			}
			var size int64
			if obj.Size != nil {
				size = *obj.Size
			}
			entries = append(entries, DirEntry{Name: name, IsDir: false, Size: size})
		}
	}
	return entries, nil
}

// MkdirAll implements FS. S3 has no directory concept; this is a no-op.
func (s *S3FS) MkdirAll(_ context.Context, _ string) error { return nil }

// Remove implements FS. Returns ErrNotExist if the key does not exist.
func (s *S3FS) Remove(ctx context.Context, name string) error {
	// Verify the object exists first so we can return ErrNotExist correctly.
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &s.bucket,
		Key:    aws.String(s.key(name)),
	})
	if err != nil {
		if isS3NotFound(err) {
			return fmt.Errorf("%w: %s", ErrNotExist, name)
		}
		return fmt.Errorf("wsfs/s3: head before remove %s: %w", name, err)
	}
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    aws.String(s.key(name)),
	})
	if err != nil {
		return fmt.Errorf("wsfs/s3: delete %s: %w", name, err)
	}
	return nil
}

// isS3NotFound returns true for S3 404 / NoSuchKey responses.
func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	var nsk *s3types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *s3types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "NoSuchKey") ||
		strings.Contains(msg, "NotFound") ||
		strings.Contains(msg, "404")
}

// ─────────────────────────────────────────────────────────────
// ManifestFS implementation
// ─────────────────────────────────────────────────────────────

// manifestKey is the S3 object key used to store the workspace manifest.
const manifestKey = ".flint-manifest.json"

// s3Manifest is the JSON structure stored in S3.
type s3Manifest struct {
	Version int64              `json:"version"`
	Entries map[string]s3Entry `json:"entries"`
}

type s3Entry struct {
	Hash  string `json:"hash"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

// GetManifest implements ManifestFS. Reads the manifest JSON from S3.
// Returns an empty manifest with version 0 if the manifest doesn't exist yet
// (first step in the run).
func (s *S3FS) GetManifest(ctx context.Context) (Manifest, int64, error) {
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    aws.String(s.key(manifestKey)),
	})
	if err != nil {
		if isS3NotFound(err) {
			return make(Manifest), 0, nil
		}
		return nil, 0, fmt.Errorf("wsfs/s3: get manifest: %w", err)
	}
	defer resp.Body.Close()

	var sm s3Manifest
	if err := json.NewDecoder(resp.Body).Decode(&sm); err != nil {
		return nil, 0, fmt.Errorf("wsfs/s3: decode manifest: %w", err)
	}

	m := make(Manifest, len(sm.Entries))
	for path, e := range sm.Entries {
		m[path] = ManifestEntry(e)
	}
	return m, sm.Version, nil
}

// PutManifest writes the manifest JSON to S3. Called by SyncOut after
// uploading changed files.
func (s *S3FS) PutManifest(ctx context.Context, m Manifest, version int64) error {
	sm := s3Manifest{
		Version: version,
		Entries: make(map[string]s3Entry, len(m)),
	}
	for path, e := range m {
		sm.Entries[path] = s3Entry(e)
	}

	data, err := json.Marshal(sm)
	if err != nil {
		return fmt.Errorf("wsfs/s3: marshal manifest: %w", err)
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         aws.String(s.key(manifestKey)),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("wsfs/s3: put manifest: %w", err)
	}
	return nil
}

// compile-time interface checks
var _ FS = (*S3FS)(nil)
var _ ManifestFS = (*S3FS)(nil)
