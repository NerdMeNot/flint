package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// HostShell executes steps as direct child processes — no image, no
// namespaces, NO isolation. It exists for development on any OS and for
// integration tests; production machines use the containerd runtime. The
// daemon refuses to select it unless explicitly asked.
type HostShell struct {
	mu    sync.Mutex
	procs map[string]*hostProc
	seq   int
}

type hostProc struct {
	id   string
	cmd  *exec.Cmd
	done chan struct{}
	exit ExitStatus
	err  error
}

func (h *hostProc) ID() string { return h.id }

// NewHostShell builds the dev/test runtime.
func NewHostShell() *HostShell {
	return &HostShell{procs: map[string]*hostProc{}}
}

func (r *HostShell) Start(context.Context) error { return nil }
func (r *HostShell) Close() error                { return nil }

// CreateStep launches the command with the workspace as its working dir.
// Secrets are injected by prepending a `set -a; . file` sourcing prefix when
// the command is a shell invocation; env otherwise.
func (r *HostShell) CreateStep(ctx context.Context, spec StepSpec) (Handle, error) {
	if len(spec.Command) == 0 {
		return nil, errors.New("hostshell: empty command")
	}
	// Loud rejection beats a step that green-lights against a service that was
	// never started.
	if len(spec.Services) > 0 {
		return nil, errors.New("hostshell: services require the containerd runtime (hostshell is a dev shim with no containers)")
	}
	command := spec.Command
	// No mount namespace here: the container path of the agent binary maps
	// back to the host binary.
	if command[0] == AgentBinaryMount && spec.AgentBinary != "" {
		command = append([]string{spec.AgentBinary}, command[1:]...)
	}
	//nolint:gosec // executing the user's CI step command is the entire point
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = spec.WorkspaceDir
	if spec.WorkingDir != "" && spec.WorkingDir != "/workspace" {
		cmd.Dir = spec.WorkingDir
	}
	cmd.Env = append(os.Environ(), spec.Env...)
	// Path-dependent env is the runtime's to define: host paths here,
	// in-container paths under containerd.
	cmd.Env = append(cmd.Env,
		"FLINT_OUTPUT="+filepath.Join(spec.IODir, "emit"),
		"FLINT_WORKSPACE="+spec.WorkspaceDir,
	)
	if spec.SecretsEnvFile != "" {
		secrets, err := readEnvFile(spec.SecretsEnvFile)
		if err != nil {
			return nil, fmt.Errorf("hostshell: secrets env file: %w", err)
		}
		cmd.Env = append(cmd.Env, secrets...)
	}
	cmd.Stdout = spec.Stdout
	cmd.Stderr = spec.Stderr
	// Own process group so Kill reaps the whole tree, not just the shell.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.seq++
	p := &hostProc{
		id:   fmt.Sprintf("hostshell-%s-%d", spec.StepName, r.seq),
		cmd:  cmd,
		done: make(chan struct{}),
	}
	r.procs[p.id] = p
	r.mu.Unlock()

	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			code = 1
			if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
				code = exitErr.ExitCode()
			} else {
				p.err = err
			}
		}
		p.exit = ExitStatus{Code: code, Finished: time.Now()}
		close(p.done)
	}()
	return p, nil
}

func (r *HostShell) Wait(ctx context.Context, h Handle) (ExitStatus, error) {
	p, err := r.proc(h)
	if err != nil {
		return ExitStatus{}, err
	}
	select {
	case <-ctx.Done():
		return ExitStatus{}, ctx.Err()
	case <-p.done:
		return p.exit, p.err
	}
}

func (r *HostShell) Kill(_ context.Context, h Handle, grace time.Duration) error {
	p, err := r.proc(h)
	if err != nil {
		return err
	}
	if p.cmd.Process == nil {
		return nil
	}
	pgid := -p.cmd.Process.Pid
	_ = syscall.Kill(pgid, syscall.SIGTERM)
	select {
	case <-p.done:
		return nil
	case <-time.After(grace):
		return syscall.Kill(pgid, syscall.SIGKILL)
	}
}

func (r *HostShell) Remove(_ context.Context, h Handle) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.procs, h.ID())
	return nil
}

func (r *HostShell) proc(h Handle) (*hostProc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.procs[h.ID()]
	if !ok {
		return nil, fmt.Errorf("hostshell: unknown handle %s", h.ID())
	}
	return p, nil
}

func readEnvFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "=") {
			out = append(out, line)
		}
	}
	return out, nil
}
