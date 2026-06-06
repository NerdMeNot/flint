package workspace

import "context"

// PVCWorkspace is a no-op workspace backend for shared-volume (PVC) mode.
// All step pods mount the same ReadWriteMany PVC, so no sync is needed.
type PVCWorkspace struct{}

// NewPVC creates a PVC workspace. Since all pods share the volume, there's
// nothing to configure.
func NewPVC() *PVCWorkspace {
	return &PVCWorkspace{}
}

func (p *PVCWorkspace) SyncIn(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (p *PVCWorkspace) SyncOut(_ context.Context, _ string) error {
	return nil
}

func (p *PVCWorkspace) Close() error {
	return nil
}

var _ Workspace = (*PVCWorkspace)(nil)
