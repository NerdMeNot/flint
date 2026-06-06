// Package wsfs defines the workspace filesystem abstraction used by Flint
// pipeline agents.
//
// The FS interface decouples agent code from the underlying storage transport.
// Three implementations are provided:
//
//   - LocalFS   — os-backed, used in local development and single-node mode
//   - RemoteFS  — gRPC client connecting to a per-run workspace agent pod
//   - S3FS      — S3-backed cold storage, used for cross-run cache
//
// Within a K8s pipeline run the workspace agent pod is the primary store:
// artifacts produced by one step are written via RemoteFS and read by
// downstream steps in the same run. S3FS is used alongside it for cache that
// must persist across runs.
//
// All path arguments must be relative and use forward slashes as separators.
// Implementations reject absolute paths and paths that escape the root.
package wsfs

import (
	"context"
	"errors"
	"io"
	"time"
)

// FS is the workspace filesystem interface. All paths are relative,
// forward-slash separated, and must not escape the storage root.
//
// All methods accept a context so that network-backed implementations can
// respect cancellation and deadlines. Implementations must be safe for
// concurrent use from multiple goroutines.
type FS interface {
	// Open opens the named file for reading.
	// Returns ErrNotExist if the file does not exist.
	Open(ctx context.Context, name string) (io.ReadCloser, error)

	// Create creates or truncates the named file for writing.
	// Parent directories are created as needed.
	// The caller must Close the returned WriteCloser to commit the write;
	// writes may be buffered until Close is called.
	Create(ctx context.Context, name string) (io.WriteCloser, error)

	// Stat returns metadata for the named file.
	// Returns ErrNotExist if the path does not exist.
	Stat(ctx context.Context, name string) (FileInfo, error)

	// ReadDir lists entries directly under dir.
	// Returns ErrNotExist if dir does not exist.
	ReadDir(ctx context.Context, dir string) ([]DirEntry, error)

	// MkdirAll creates dir and all required parents, analogous to os.MkdirAll.
	// It is not an error if the directory already exists.
	MkdirAll(ctx context.Context, dir string) error

	// Remove deletes the named file.
	// Returns ErrNotExist if the file does not exist.
	Remove(ctx context.Context, name string) error
}

// FileInfo holds metadata for a single workspace file or directory.
type FileInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
	IsDir   bool
}

// DirEntry is a single entry returned by FS.ReadDir.
type DirEntry struct {
	Name  string
	IsDir bool
	Size  int64
}

// Sentinel errors returned by FS implementations.
var (
	// ErrNotExist is returned when the requested path does not exist.
	ErrNotExist = errors.New("wsfs: file does not exist")

	// ErrPermission is returned when an operation is not permitted,
	// including path-traversal attempts.
	ErrPermission = errors.New("wsfs: permission denied")
)

// IsNotExist reports whether err is or wraps ErrNotExist.
func IsNotExist(err error) bool {
	return errors.Is(err, ErrNotExist)
}

// IsPermission reports whether err is or wraps ErrPermission.
func IsPermission(err error) bool {
	return errors.Is(err, ErrPermission)
}

// ─────────────────────────────────────────────────────────────
// ManifestFS — incremental sync extension
// ─────────────────────────────────────────────────────────────

// ManifestEntry describes a single file tracked in a workspace manifest.
// The hash is an xxh3 hex string computed from the full file content.
type ManifestEntry struct {
	Hash  string
	Size  int64
	Mtime int64 // Unix seconds
}

// Manifest maps relative file paths to their ManifestEntry.
type Manifest map[string]ManifestEntry

// ManifestFS extends FS with manifest-based incremental sync support.
// RemoteFS (workspace agent) and S3FS both implement this interface.
// Agents test for this interface and fall back to full-tar sync when absent.
type ManifestFS interface {
	FS

	// GetManifest returns the server's current file manifest and a monotonic
	// version counter. Agents cache the version to skip SyncIn when unchanged.
	GetManifest(ctx context.Context) (Manifest, int64, error)
}

// ManifestWriter extends ManifestFS with explicit manifest persistence.
// S3FS implements this — the manifest must be written explicitly after sync.
// RemoteFS does NOT — the workspace agent updates its manifest automatically
// on each Put/Remove.
type ManifestWriter interface {
	ManifestFS
	PutManifest(ctx context.Context, m Manifest, version int64) error
}
