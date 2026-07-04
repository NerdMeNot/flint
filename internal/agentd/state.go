package agentd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// identity is the persisted machine identity from a successful registration,
// so a daemon restart resumes as the same machine instead of re-registering.
type identity struct {
	MachineID     string `json:"machineId"`
	MachineToken  string `json:"machineToken"`
	PoolName      string `json:"poolName"`
	ServerHTTPURL string `json:"serverHttpUrl"`
}

func identityPath(dataDir string) string {
	return filepath.Join(dataDir, "identity.json")
}

// loadIdentity returns (nil, nil) when no identity exists yet.
func loadIdentity(dataDir string) (*identity, error) {
	data, err := os.ReadFile(identityPath(dataDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var id identity
	if err := json.Unmarshal(data, &id); err != nil {
		return nil, err
	}
	if id.MachineID == "" || id.MachineToken == "" {
		return nil, nil
	}
	return &id, nil
}

func saveIdentity(dataDir string, id identity) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(identityPath(dataDir), data, 0o600)
}

// dirs lays out the agent's on-disk state.
type dirs struct{ root string }

func (d dirs) runWorkspace(runID string) string {
	return filepath.Join(d.root, "runs", runID, "workspace")
}
func (d dirs) stepIO(runID, step string) string {
	return filepath.Join(d.root, "runs", runID, "steps", sanitizePath(step), "io")
}
func (d dirs) runRoot(runID string) string {
	return filepath.Join(d.root, "runs", runID)
}
func (d dirs) runsRoot() string {
	return filepath.Join(d.root, "runs")
}

// sanitizePath makes a step name filesystem-safe (matrix names carry [=,]).
func sanitizePath(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// residentRunIDs lists the run directories currently on disk (heartbeat GC
// input).
func (d dirs) residentRunIDs() []string {
	entries, err := os.ReadDir(d.runsRoot())
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids
}
