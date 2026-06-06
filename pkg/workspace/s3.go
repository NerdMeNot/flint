package workspace

import (
	"context"
	"fmt"

	"github.com/NerdMeNot/flint/pkg/wsfs"
	"github.com/rs/zerolog/log"
)

// S3Workspace syncs workspace state via S3 with a per-run key prefix.
// Uses the same incremental manifest diff as AgentWorkspace, but stores
// the manifest as a JSON object in S3 alongside the files.
type S3Workspace struct {
	s3 *wsfs.S3FS
}

// NewS3 creates an S3-backed workspace scoped to a single run.
// All objects are stored under: s3://bucket/workspace/runs/{runID}/
func NewS3(bucket, region, runID string) (*S3Workspace, error) {
	prefix := fmt.Sprintf("workspace/runs/%s/", runID)
	s3fs, err := wsfs.NewS3WithPrefix(bucket, region, prefix)
	if err != nil {
		return nil, fmt.Errorf("workspace/s3: init: %w", err)
	}
	return &S3Workspace{s3: s3fs}, nil
}

func (s *S3Workspace) SyncIn(ctx context.Context, workDir string) (bool, error) {
	remote, version, err := s.s3.GetManifest(ctx)
	if err != nil {
		return false, fmt.Errorf("workspace/s3: get manifest: %w", err)
	}
	if version == 0 || len(remote) == 0 {
		return false, nil
	}

	local, err := ComputeManifest(workDir)
	if err != nil {
		local = make(Manifest)
	}

	remoteM := fromWSFS(remote)
	_, download, _ := Diff(local, remoteM)

	downloaded := 0
	for _, path := range download {
		if err := downloadFile(ctx, s.s3, workDir, path); err != nil {
			return false, fmt.Errorf("workspace/s3: download %q: %w", path, err)
		}
		downloaded++
	}

	log.Info().
		Int("downloaded", downloaded).
		Int64("version", version).
		Msg("workspace/s3: SyncIn complete")

	return true, nil
}

func (s *S3Workspace) SyncOut(ctx context.Context, workDir string) error {
	local, err := ComputeManifest(workDir)
	if err != nil {
		return fmt.Errorf("workspace/s3: compute manifest: %w", err)
	}

	remote, version, err := s.s3.GetManifest(ctx)
	if err != nil {
		return fmt.Errorf("workspace/s3: get manifest: %w", err)
	}

	remoteM := fromWSFS(remote)
	upload, _, remove := Diff(local, remoteM)

	uploaded, removed := 0, 0
	for _, path := range upload {
		if err := uploadFile(ctx, s.s3, workDir, path); err != nil {
			return fmt.Errorf("workspace/s3: upload %q: %w", path, err)
		}
		uploaded++
	}

	for _, path := range remove {
		if err := s.s3.Remove(ctx, path); err != nil && !wsfs.IsNotExist(err) {
			return fmt.Errorf("workspace/s3: remove %q: %w", path, err)
		}
		removed++
	}

	// Persist the updated manifest. Convert local Manifest → wsfs.Manifest.
	wsfsLocal := make(wsfs.Manifest, len(local))
	for k, v := range local {
		wsfsLocal[k] = wsfs.ManifestEntry{Hash: v.Hash, Size: v.Size, Mtime: v.Mtime}
	}
	if err := s.s3.PutManifest(ctx, wsfsLocal, version+1); err != nil {
		return fmt.Errorf("workspace/s3: write manifest: %w", err)
	}

	log.Info().
		Int("uploaded", uploaded).
		Int("removed", removed).
		Msg("workspace/s3: SyncOut complete")

	return nil
}

func (s *S3Workspace) Close() error {
	return nil // S3 client has no connection to close
}

var _ Workspace = (*S3Workspace)(nil)
