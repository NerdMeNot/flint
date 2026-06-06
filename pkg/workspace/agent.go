package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/NerdMeNot/flint/pkg/wsfs"
	"github.com/rs/zerolog/log"
)

// AgentWorkspace syncs workspace state via the per-run gRPC workspace agent.
// The workspace agent maintains an in-memory manifest that tracks all files
// stored in it. SyncIn/SyncOut diff local vs remote manifests and transfer
// only changed files.
type AgentWorkspace struct {
	remote *wsfs.RemoteFS
}

// NewAgent creates an agent-backed workspace.
func NewAgent(addr, token string) (*AgentWorkspace, error) {
	remote, err := wsfs.NewRemote(addr, token)
	if err != nil {
		return nil, fmt.Errorf("workspace/agent: dial %s: %w", addr, err)
	}
	return &AgentWorkspace{remote: remote}, nil
}

func (a *AgentWorkspace) SyncIn(ctx context.Context, workDir string) (bool, error) {
	remote, version, err := a.remote.GetManifest(ctx)
	if err != nil {
		return false, fmt.Errorf("workspace/agent: get manifest: %w", err)
	}
	if version == 0 || len(remote) == 0 {
		return false, nil // first step, nothing to sync
	}

	local, err := ComputeManifest(workDir)
	if err != nil {
		local = make(Manifest) // workspace may not exist yet
	}

	remoteM := fromWSFS(remote)
	_, download, _ := Diff(local, remoteM)

	downloaded := 0
	for _, path := range download {
		if err := downloadFile(ctx, a.remote, workDir, path); err != nil {
			return false, fmt.Errorf("workspace/agent: download %q: %w", path, err)
		}
		downloaded++
	}

	log.Info().
		Int("downloaded", downloaded).
		Int("skipped", len(remote)-downloaded).
		Int64("version", version).
		Msg("workspace: SyncIn complete")

	return true, nil
}

func (a *AgentWorkspace) SyncOut(ctx context.Context, workDir string) error {
	local, err := ComputeManifest(workDir)
	if err != nil {
		return fmt.Errorf("workspace/agent: compute manifest: %w", err)
	}

	remote, _, err := a.remote.GetManifest(ctx)
	if err != nil {
		return fmt.Errorf("workspace/agent: get manifest: %w", err)
	}

	remoteM := fromWSFS(remote)
	upload, _, remove := Diff(local, remoteM)

	uploaded, removed := 0, 0
	for _, path := range upload {
		if err := uploadFile(ctx, a.remote, workDir, path); err != nil {
			return fmt.Errorf("workspace/agent: upload %q: %w", path, err)
		}
		uploaded++
	}

	for _, path := range remove {
		if err := a.remote.Remove(ctx, path); err != nil && !wsfs.IsNotExist(err) {
			return fmt.Errorf("workspace/agent: remove %q: %w", path, err)
		}
		removed++
	}

	log.Info().
		Int("uploaded", uploaded).
		Int("removed", removed).
		Int("skipped", len(local)-uploaded).
		Msg("workspace: SyncOut complete")

	return nil
}

func (a *AgentWorkspace) Close() error {
	return a.remote.Close()
}

// ─────────────────────────────────────────────────────────────
// file transfer helpers
// ─────────────────────────────────────────────────────────────

func uploadFile(ctx context.Context, fs wsfs.FS, root, rel string) error {
	absPath := filepath.Join(root, filepath.FromSlash(rel))
	f, err := os.Open(absPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w, err := fs.Create(ctx, rel)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, f); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func downloadFile(ctx context.Context, fs wsfs.FS, root, rel string) error {
	absPath := filepath.Join(root, filepath.FromSlash(rel))

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}

	r, err := fs.Open(ctx, rel)
	if err != nil {
		return err
	}
	defer r.Close()

	tmp := absPath + ".flint-tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, absPath) // atomic
}

// ─────────────────────────────────────────────────────────────
// manifest conversion helpers
// ─────────────────────────────────────────────────────────────

// fromWSFS converts wsfs.Manifest → workspace.Manifest.
func fromWSFS(m wsfs.Manifest) Manifest {
	out := make(Manifest, len(m))
	for k, v := range m {
		out[k] = ManifestEntry{Hash: v.Hash, Size: v.Size, Mtime: v.Mtime}
	}
	return out
}

var _ Workspace = (*AgentWorkspace)(nil)
