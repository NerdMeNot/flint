// Package agentd implements the flint-agent daemon: the persistent per-machine
// process that registers with the control plane, heartbeats, claims step
// assignments, and executes them. Machines need nothing pre-installed — the
// production runtime is bundled with the agent.
package agentd

import (
	"errors"
	"os"
	"runtime"
	"strconv"
)

// Config is the daemon's minimal boot configuration — flags/env only, no
// config file. Everything per-step arrives in the assignment's StepPayload.
type Config struct {
	// ServerURL is the control plane's gRPC address (host:port).
	ServerURL string
	// Token is the registration credential: a pool join token (static
	// machines) or the bootstrap token from cloud-init (elastic machines).
	// Unused once identity.json exists.
	Token string
	// MachineID pre-binds an elastic machine to its pre-allocated row.
	MachineID string
	// DataDir is the agent's state root (identity, workspaces, caches).
	DataDir string
	// Capacity is the max concurrent step executions (0 = NumCPU/2, min 1).
	Capacity int
	// Runtime selects the execution backend: "containerd" (default) or
	// "hostshell" (dev/test only — direct process execution, no isolation).
	Runtime string
	// Labels are capability labels reported at registration.
	Labels map[string]string
	// Insecure dials gRPC without TLS (dev/local).
	Insecure bool
	// AgentBinary overrides the flint-agent binary handed to group-steps
	// containers (default: this executable). Tests run the daemon in-process,
	// where os.Executable would be the test binary.
	AgentBinary string
}

// FromEnv fills unset fields from FLINT_AGENT_* variables.
func (c *Config) FromEnv() {
	if c.ServerURL == "" {
		c.ServerURL = os.Getenv("FLINT_AGENT_SERVER")
	}
	if c.Token == "" {
		c.Token = os.Getenv("FLINT_AGENT_TOKEN")
	}
	if c.MachineID == "" {
		c.MachineID = os.Getenv("FLINT_AGENT_MACHINE_ID")
	}
	if c.DataDir == "" {
		c.DataDir = os.Getenv("FLINT_AGENT_DATA_DIR")
	}
	if c.Capacity == 0 {
		if v, err := strconv.Atoi(os.Getenv("FLINT_AGENT_CAPACITY")); err == nil {
			c.Capacity = v
		}
	}
	if c.Runtime == "" {
		c.Runtime = os.Getenv("FLINT_AGENT_RUNTIME")
	}
}

// Validate applies defaults and checks required fields.
func (c *Config) Validate() error {
	if c.ServerURL == "" {
		return errors.New("agentd: server URL is required (--server / FLINT_AGENT_SERVER)")
	}
	if c.DataDir == "" {
		c.DataDir = "/var/lib/flint-agent"
	}
	if c.Capacity <= 0 {
		c.Capacity = max(1, runtime.NumCPU()/2)
	}
	if c.Runtime == "" {
		c.Runtime = "containerd"
	}
	return nil
}
