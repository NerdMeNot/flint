//go:build !linux

package runtime

import (
	"context"
	"errors"
	"time"
)

// ContainerdConfig mirrors the linux build's config so callers compile
// everywhere; the runtime itself is linux-only.
type ContainerdConfig struct {
	Socket    string
	DataDir   string
	BundleDir string
}

// Containerd is unavailable off linux — development uses hostshell.
type Containerd struct{}

func NewContainerd(ContainerdConfig) *Containerd { return &Containerd{} }

var errLinuxOnly = errors.New("agentd: the containerd runtime is linux-only — use --runtime hostshell for development")

func (r *Containerd) Start(context.Context) error { return errLinuxOnly }
func (r *Containerd) CreateStep(context.Context, StepSpec) (Handle, error) {
	return nil, errLinuxOnly
}
func (r *Containerd) Wait(context.Context, Handle) (ExitStatus, error) {
	return ExitStatus{}, errLinuxOnly
}
func (r *Containerd) Kill(context.Context, Handle, time.Duration) error {
	return errLinuxOnly
}
func (r *Containerd) Remove(context.Context, Handle) error { return errLinuxOnly }
func (r *Containerd) Close() error                         { return nil }
