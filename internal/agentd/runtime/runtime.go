// Package runtime abstracts how the flint-agent daemon executes one step on
// the machine. Implementations: containerd (production — bundled runtime,
// container-per-step), hostshell (dev/test — direct process execution, NO
// isolation), fake (unit tests — scripted outcomes).
package runtime

import (
	"context"
	"io"
	"time"
)

// StepSpec is everything a Runtime needs to execute one step.
type StepSpec struct {
	RunID    string
	StepName string

	// Image is the container image (ignored by hostshell).
	Image string
	// Command is the resolved argv (shell wrapper already applied).
	Command []string
	// WorkingDir inside the workspace.
	WorkingDir string
	// Env is the complete non-secret environment (KEY=VALUE).
	Env []string
	// SecretsEnvFile is a host path to a KEY=VALUE file sourced into the step
	// (tmpfs; never the agent's own env). Empty = no secrets.
	SecretsEnvFile string
	// WorkspaceDir is the host path bind-mounted (or chdir'd) as /workspace.
	WorkspaceDir string
	// IODir is the host path for the step's control files (emit/output).
	IODir string
	// AgentBinary is the host path of the flint-agent binary, bind-mounted
	// read-only at /flint/bin/flint-agent so group steps can run the steps
	// driver inside the user's image (static binary, matching architecture).
	AgentBinary string

	CPUMillis  int64
	MemoryMB   int64
	Privileged bool

	// Stdout/Stderr receive the step's output as it happens.
	Stdout, Stderr io.Writer
}

// Handle identifies a started step for Wait/Kill/Remove.
type Handle interface {
	ID() string
}

// ExitStatus is the step's terminal state.
type ExitStatus struct {
	Code     int
	OOMKill  bool
	Finished time.Time
}

// AgentBinaryMount is the in-container path of the flint-agent binary; a
// command starting with it is the group-steps driver invocation. Runtimes
// without a mount namespace (hostshell) rewrite it to the host binary.
const AgentBinaryMount = "/flint/bin/flint-agent"

// Runtime executes steps on this machine.
type Runtime interface {
	// Start prepares the runtime (extract bundle, launch containerd, …).
	Start(ctx context.Context) error
	// CreateStep starts the step (and its service containers, when supported).
	CreateStep(ctx context.Context, spec StepSpec) (Handle, error)
	// Wait blocks until the step exits.
	Wait(ctx context.Context, h Handle) (ExitStatus, error)
	// Kill terminates the step: SIGTERM, grace, SIGKILL.
	Kill(ctx context.Context, h Handle, grace time.Duration) error
	// Remove releases all resources for the step. Always called.
	Remove(ctx context.Context, h Handle) error
	// Close shuts the runtime down.
	Close() error
}
