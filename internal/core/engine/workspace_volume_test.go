package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceVolume_DiskSizesEmptyDir(t *testing.T) {
	// A job with disk: caps the scratch emptyDir at that size.
	v := workspaceVolume(false, "run-1", nil, "20Gi")
	require.NotNil(t, v.EmptyDir)
	require.NotNil(t, v.EmptyDir.SizeLimit)
	assert.Equal(t, "20Gi", v.EmptyDir.SizeLimit.String())
	assert.Nil(t, v.PersistentVolumeClaim)
}

func TestWorkspaceVolume_NoDiskIsUnsizedEmptyDir(t *testing.T) {
	v := workspaceVolume(false, "run-1", nil, "")
	require.NotNil(t, v.EmptyDir)
	assert.Nil(t, v.EmptyDir.SizeLimit)
}

func TestWorkspaceVolume_PVCModeIgnoresDisk(t *testing.T) {
	v := workspaceVolume(true, "run-1", nil, "20Gi")
	require.NotNil(t, v.PersistentVolumeClaim)
	assert.Nil(t, v.EmptyDir)
}
