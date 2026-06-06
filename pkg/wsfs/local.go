package wsfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// LocalFS is an os-backed FS implementation rooted at a single directory.
// Every path argument is resolved relative to the root and validated to
// prevent path traversal; any path that would escape the root is rejected
// with ErrPermission.
//
// LocalFS is safe for concurrent use; the underlying OS filesystem provides
// the necessary synchronisation.
type LocalFS struct {
	root string // absolute, cleaned path
}

// NewLocal creates a LocalFS rooted at dir. dir must already exist.
func NewLocal(dir string) (*LocalFS, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("wsfs/local: resolve root %q: %w", dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("wsfs/local: stat root %q: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("wsfs/local: root %q is not a directory", abs)
	}
	return &LocalFS{root: abs}, nil
}

// Root returns the absolute path of the filesystem root.
func (l *LocalFS) Root() string { return l.root }

// safePath resolves name relative to the root and verifies that the result
// is still under the root. Returns ErrPermission for invalid paths.
func (l *LocalFS) safePath(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("wsfs/local: empty path: %w", ErrPermission)
	}

	// Convert slashes, then clean. filepath.Clean removes ".." components.
	cleaned := filepath.Clean(filepath.FromSlash(name))

	// Reject absolute paths after cleaning — they can never be under root.
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("wsfs/local: absolute path not allowed %q: %w", name, ErrPermission)
	}

	full := filepath.Join(l.root, cleaned)

	// Ensure the resolved path is strictly under root.
	// The trailing separator check catches the case where full == l.root.
	if full != l.root && !strings.HasPrefix(full, l.root+string(filepath.Separator)) {
		return "", fmt.Errorf("wsfs/local: path %q escapes root: %w", name, ErrPermission)
	}

	return full, nil
}

// Open implements FS.
func (l *LocalFS) Open(_ context.Context, name string) (io.ReadCloser, error) {
	full, err := l.safePath(name)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, mapOSError(err)
	}
	return f, nil
}

// Create implements FS.
func (l *LocalFS) Create(_ context.Context, name string) (io.WriteCloser, error) {
	full, err := l.safePath(name)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, fmt.Errorf("wsfs/local: create parents for %q: %w", name, err)
	}
	f, err := os.Create(full)
	if err != nil {
		return nil, mapOSError(err)
	}
	return f, nil
}

// Stat implements FS.
func (l *LocalFS) Stat(_ context.Context, name string) (FileInfo, error) {
	full, err := l.safePath(name)
	if err != nil {
		return FileInfo{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return FileInfo{}, mapOSError(err)
	}
	return FileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}, nil
}

// ReadDir implements FS.
func (l *LocalFS) ReadDir(_ context.Context, dir string) ([]DirEntry, error) {
	full, err := l.safePath(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, mapOSError(err)
	}
	result := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		var size int64
		if !e.IsDir() {
			if info, ierr := e.Info(); ierr == nil {
				size = info.Size()
			}
		}
		result = append(result, DirEntry{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Size:  size,
		})
	}
	return result, nil
}

// MkdirAll implements FS.
func (l *LocalFS) MkdirAll(_ context.Context, dir string) error {
	full, err := l.safePath(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(full, 0o755); err != nil {
		return fmt.Errorf("wsfs/local: mkdirall %q: %w", dir, err)
	}
	return nil
}

// Remove implements FS.
func (l *LocalFS) Remove(_ context.Context, name string) error {
	full, err := l.safePath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil {
		return mapOSError(err)
	}
	return nil
}

// mapOSError converts os-layer errors to wsfs sentinel errors so callers can
// use IsNotExist / IsPermission without importing the os package.
func mapOSError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
		return fmt.Errorf("%w: %s", ErrNotExist, err.Error())
	}
	if errors.Is(err, fs.ErrPermission) || os.IsPermission(err) {
		return fmt.Errorf("%w: %s", ErrPermission, err.Error())
	}
	return err
}

// compile-time interface check
var _ FS = (*LocalFS)(nil)
