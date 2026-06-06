package workspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_PVCMode(t *testing.T) {
	ws, err := New("pvc", "", "", "", "", "")

	require.NoError(t, err)
	require.NotNil(t, ws)
	assert.IsType(t, &PVCWorkspace{}, ws, "mode=pvc should return PVCWorkspace")
}

func TestNew_EmptyModeNoAddrNoBucket(t *testing.T) {
	// mode="" with no addr and no bucket — no backend available.
	ws, err := New("", "", "", "", "", "")

	assert.NoError(t, err)
	assert.Nil(t, ws, "empty mode with no addr/bucket should return nil workspace")
}

func TestNew_AgentModeNoAddrNoBucket(t *testing.T) {
	// mode="agent" with no addr and no bucket — no backend available.
	ws, err := New("agent", "", "", "", "", "")

	assert.NoError(t, err)
	assert.Nil(t, ws, "agent mode with no addr/bucket should return nil workspace")
}

func TestNew_InvalidMode(t *testing.T) {
	ws, err := New("invalid", "", "", "", "", "")

	assert.Error(t, err)
	assert.Nil(t, ws)
	assert.Contains(t, err.Error(), "unknown mode")
	assert.Contains(t, err.Error(), "invalid")
}

func TestNew_AgentModeWithAddr(t *testing.T) {
	// This will fail at dial time since there's no server, but it should
	// attempt to create an AgentWorkspace (not return nil or a PVC).
	// We use a bogus address — NewAgent will fail connecting.
	ws, err := New("agent", "localhost:0", "token", "", "", "")

	// NewAgent dials eagerly, so we expect an error from the failed connection.
	// The key assertion is that it attempted AgentWorkspace creation.
	if err != nil {
		assert.Contains(t, err.Error(), "workspace/agent")
	} else {
		assert.IsType(t, &AgentWorkspace{}, ws)
		_ = ws.Close()
	}
}

func TestNew_S3ModeWithoutBucket(t *testing.T) {
	ws, err := New("s3", "", "", "", "", "")

	assert.Error(t, err)
	assert.Nil(t, ws)
	assert.Contains(t, err.Error(), "requires bucket")
}
