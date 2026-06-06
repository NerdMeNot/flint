package workspace

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPVCWorkspace_SyncIn_ReturnsNoSync(t *testing.T) {
	ws := NewPVC()

	synced, err := ws.SyncIn(context.Background(), t.TempDir())

	require.NoError(t, err)
	assert.False(t, synced, "PVC SyncIn should always return false (no sync needed)")
}

func TestPVCWorkspace_SyncOut_ReturnsNil(t *testing.T) {
	ws := NewPVC()

	err := ws.SyncOut(context.Background(), t.TempDir())

	assert.NoError(t, err, "PVC SyncOut should always return nil")
}

func TestPVCWorkspace_Close_ReturnsNil(t *testing.T) {
	ws := NewPVC()

	err := ws.Close()

	assert.NoError(t, err, "PVC Close should always return nil")
}

func TestPVCWorkspace_ImplementsInterface(t *testing.T) {
	var ws Workspace = NewPVC()

	synced, err := ws.SyncIn(context.Background(), "/any/path")
	require.NoError(t, err)
	assert.False(t, synced)

	assert.NoError(t, ws.SyncOut(context.Background(), "/any/path"))
	assert.NoError(t, ws.Close())
}
