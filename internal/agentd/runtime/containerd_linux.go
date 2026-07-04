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

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog/log"
)

// containerdNamespace isolates Flint's containers/images from anything else
// using the same containerd.
const containerdNamespace = "flint"

// ContainerdConfig locates (or launches) the runtime.
type ContainerdConfig struct {
	// Socket is the containerd socket to use. Empty = auto: the agent-managed
	// bundled containerd when a bundle is present, else the system socket.
	Socket string
	// DataDir is the agent state root (bundled containerd lives under it).
	DataDir string
	// BundleDir holds bundled containerd/runc binaries (extracted at install
	// by scripts/bundle-runtime.sh). Empty = DataDir/bin.
	BundleDir string
}

// Containerd executes each step as a container: real isolation, image layer
// caching across steps and runs, cgroup resource limits. The production
// runtime for flint-agent machines.
type Containerd struct {
	cfg    ContainerdConfig
	client *containerd.Client

	mu    sync.Mutex
	tasks map[string]*cdHandle
	child *exec.Cmd // bundled containerd process, when we launched it
	seq   int
}

type cdHandle struct {
	id        string
	container containerd.Container
	task      containerd.Task
	exitCh    <-chan containerd.ExitStatus
}

func (h *cdHandle) ID() string { return h.id }

// NewContainerd builds the production runtime.
func NewContainerd(cfg ContainerdConfig) *Containerd {
	if cfg.BundleDir == "" {
		cfg.BundleDir = filepath.Join(cfg.DataDir, "bin")
	}
	return &Containerd{cfg: cfg, tasks: map[string]*cdHandle{}}
}

// Start connects to containerd, launching the bundled daemon when present.
// Resolution: explicit socket → bundled containerd (launch + supervise) →
// system socket → error with install guidance.
func (r *Containerd) Start(ctx context.Context) error {
	socket := r.cfg.Socket
	if socket == "" {
		if bundled := filepath.Join(r.cfg.BundleDir, "containerd"); fileExists(bundled) {
			var err error
			if socket, err = r.launchBundled(ctx, bundled); err != nil {
				return err
			}
		} else if fileExists("/run/containerd/containerd.sock") {
			socket = "/run/containerd/containerd.sock"
			log.Info().Msg("agentd: using system containerd")
		} else {
			return errors.New("agentd: no containerd available — install the bundled runtime (scripts/bundle-runtime.sh) or run system containerd")
		}
	}

	// The daemon may still be booting; retry the connection briefly.
	var client *containerd.Client
	var err error
	deadline := time.Now().Add(15 * time.Second)
	for {
		client, err = containerd.New(socket, containerd.WithDefaultNamespace(containerdNamespace))
		if err == nil {
			if _, verr := client.Version(namespaces.WithNamespace(ctx, containerdNamespace)); verr == nil {
				break
			}
			_ = client.Close()
			err = fmt.Errorf("containerd not ready")
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("agentd: connect containerd at %s: %w", socket, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	r.client = client

	// Recovery sweep: kill and remove any containers a previous agent process
	// left behind (fail-and-retry crash model — the control plane reschedules).
	r.sweepLeftovers(namespaces.WithNamespace(ctx, containerdNamespace))
	return nil
}

// launchBundled starts the agent-managed containerd child with a generated
// config rooted under the agent's data dir.
func (r *Containerd) launchBundled(ctx context.Context, bin string) (string, error) {
	root := filepath.Join(r.cfg.DataDir, "runtime")
	state := "/run/flint-agent/containerd"
	socket := filepath.Join(state, "containerd.sock")
	configPath := filepath.Join(r.cfg.DataDir, "containerd.toml")

	for _, dir := range []string{root, state} {
		if err := os.MkdirAll(dir, 0o711); err != nil {
			return "", err
		}
	}
	config := fmt.Sprintf(`version = 3
root = %q
state = %q

[grpc]
  address = %q

[plugins.'io.containerd.cri.v1.runtime']
  disable = true
`, root, state, socket)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		return "", err
	}

	cmd := exec.Command(bin, "--config", configPath)
	// Bundled runc/shim binaries resolve from the bundle dir first.
	cmd.Env = append(os.Environ(), "PATH="+r.cfg.BundleDir+":"+os.Getenv("PATH"))
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("agentd: launch bundled containerd: %w", err)
	}
	r.child = cmd
	go func() {
		if err := cmd.Wait(); err != nil && ctx.Err() == nil {
			log.Error().Err(err).Msg("agentd: bundled containerd exited unexpectedly")
		}
	}()
	log.Info().Str("socket", socket).Msg("agentd: bundled containerd launched")
	return socket, nil
}

// CreateStep pulls the image (layer-cached across steps), creates the
// container with the workspace/io/secrets binds and cgroup limits, and starts
// it with stdio attached.
func (r *Containerd) CreateStep(ctx context.Context, spec StepSpec) (Handle, error) {
	ctx = namespaces.WithNamespace(ctx, containerdNamespace)
	if spec.Image == "" {
		return nil, errors.New("containerd runtime requires a container image")
	}

	image, err := r.ensureImage(ctx, spec.Image)
	if err != nil {
		return nil, err
	}

	command := spec.Command
	if spec.SecretsEnvFile != "" {
		command = sourceSecretsWrapper(command)
	}

	// Path-dependent env uses the in-container paths the mounts establish.
	env := append(append([]string{}, spec.Env...),
		"FLINT_OUTPUT=/flint/io/emit",
		"FLINT_WORKSPACE=/workspace",
	)

	opts := []oci.SpecOpts{
		oci.WithImageConfig(image),
		oci.WithProcessArgs(command...),
		oci.WithProcessCwd("/workspace"),
		oci.WithEnv(env),
		oci.WithMounts(stepMounts(spec)),
		// Host networking in v1: no CNI dependency on the machine. Per-step
		// network namespaces + service containers are the documented follow-up.
		oci.WithHostNamespace("network"),
		oci.WithHostResolvconf,
	}
	if spec.CPUMillis > 0 {
		period := uint64(100000)
		quota := int64(spec.CPUMillis) * 100 // millis → CFS quota against 100ms period
		opts = append(opts, oci.WithCPUCFS(quota, period))
	}
	if spec.MemoryMB > 0 {
		opts = append(opts, oci.WithMemoryLimit(uint64(spec.MemoryMB)<<20))
	}
	if spec.Privileged {
		opts = append(opts, oci.WithPrivileged)
	}

	r.mu.Lock()
	r.seq++
	id := fmt.Sprintf("flint-%s-%d", sanitizeID(spec.StepName), r.seq)
	r.mu.Unlock()

	container, err := r.client.NewContainer(ctx, id,
		containerd.WithNewSnapshot(id+"-snap", image),
		containerd.WithNewSpec(opts...),
		containerd.WithContainerLabels(map[string]string{
			"flint.run":  spec.RunID,
			"flint.step": spec.StepName,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("containerd: create container: %w", err)
	}

	task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, spec.Stdout, spec.Stderr)))
	if err != nil {
		_ = container.Delete(ctx, containerd.WithSnapshotCleanup)
		return nil, fmt.Errorf("containerd: create task: %w", err)
	}
	exitCh, err := task.Wait(ctx)
	if err != nil {
		_, _ = task.Delete(ctx, containerd.WithProcessKill)
		_ = container.Delete(ctx, containerd.WithSnapshotCleanup)
		return nil, err
	}
	if err := task.Start(ctx); err != nil {
		_, _ = task.Delete(ctx, containerd.WithProcessKill)
		_ = container.Delete(ctx, containerd.WithSnapshotCleanup)
		return nil, fmt.Errorf("containerd: start task: %w", err)
	}

	h := &cdHandle{id: id, container: container, task: task, exitCh: exitCh}
	r.mu.Lock()
	r.tasks[id] = h
	r.mu.Unlock()
	return h, nil
}

func (r *Containerd) Wait(ctx context.Context, handle Handle) (ExitStatus, error) {
	h, err := r.handle(handle)
	if err != nil {
		return ExitStatus{}, err
	}
	select {
	case <-ctx.Done():
		return ExitStatus{}, ctx.Err()
	case st := <-h.exitCh:
		code, exitedAt, err := st.Result()
		if err != nil {
			return ExitStatus{}, err
		}
		return ExitStatus{Code: int(code), Finished: exitedAt}, nil
	}
}

func (r *Containerd) Kill(ctx context.Context, handle Handle, grace time.Duration) error {
	h, err := r.handle(handle)
	if err != nil {
		return err
	}
	ctx = namespaces.WithNamespace(ctx, containerdNamespace)
	_ = h.task.Kill(ctx, syscall.SIGTERM)
	select {
	case <-h.exitCh:
		return nil
	case <-time.After(grace):
		return h.task.Kill(ctx, syscall.SIGKILL)
	}
}

func (r *Containerd) Remove(ctx context.Context, handle Handle) error {
	h, err := r.handle(handle)
	if err != nil {
		return nil // already gone
	}
	ctx = namespaces.WithNamespace(ctx, containerdNamespace)
	_, _ = h.task.Delete(ctx, containerd.WithProcessKill)
	_ = h.container.Delete(ctx, containerd.WithSnapshotCleanup)
	r.mu.Lock()
	delete(r.tasks, h.id)
	r.mu.Unlock()
	return nil
}

func (r *Containerd) Close() error {
	if r.client != nil {
		_ = r.client.Close()
	}
	if r.child != nil && r.child.Process != nil {
		_ = r.child.Process.Signal(syscall.SIGTERM)
	}
	return nil
}

// ensureImage pulls the image if absent — the content store makes layers a
// cross-step, cross-run cache.
func (r *Containerd) ensureImage(ctx context.Context, ref string) (containerd.Image, error) {
	if img, err := r.client.GetImage(ctx, ref); err == nil {
		return img, nil
	}
	start := time.Now()
	img, err := r.client.Pull(ctx, ref, containerd.WithPullUnpack)
	if err != nil {
		return nil, fmt.Errorf("containerd: pull %s: %w", ref, err)
	}
	log.Info().Str("image", ref).Dur("took", time.Since(start)).Msg("agentd: image pulled")
	return img, nil
}

// sweepLeftovers removes containers a crashed agent left behind.
func (r *Containerd) sweepLeftovers(ctx context.Context) {
	containers, err := r.client.Containers(ctx, `labels."flint.run"`)
	if err != nil || len(containers) == 0 {
		return
	}
	for _, c := range containers {
		if task, err := c.Task(ctx, nil); err == nil {
			_, _ = task.Delete(ctx, containerd.WithProcessKill)
		}
		_ = c.Delete(ctx, containerd.WithSnapshotCleanup)
	}
	log.Warn().Int("containers", len(containers)).
		Msg("agentd: removed leftover step containers from a previous agent run")
}

func (r *Containerd) handle(h Handle) (*cdHandle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.tasks[h.ID()]
	if !ok {
		return nil, fmt.Errorf("containerd: unknown handle %s", h.ID())
	}
	return ch, nil
}

// stepMounts binds the run workspace, the step IO dir, and (ro) the secrets
// env file into the container.
func stepMounts(spec StepSpec) []specs.Mount {
	mounts := []specs.Mount{
		{Destination: "/workspace", Type: "bind", Source: spec.WorkspaceDir, Options: []string{"rbind", "rw"}},
		{Destination: "/flint/io", Type: "bind", Source: spec.IODir, Options: []string{"rbind", "rw"}},
	}
	if spec.SecretsEnvFile != "" {
		mounts = append(mounts, specs.Mount{
			Destination: "/flint/secrets/env", Type: "bind",
			Source: spec.SecretsEnvFile, Options: []string{"rbind", "ro"},
		})
	}
	if spec.AgentBinary != "" {
		mounts = append(mounts, specs.Mount{
			Destination: AgentBinaryMount, Type: "bind",
			Source: spec.AgentBinary, Options: []string{"rbind", "ro"},
		})
	}
	return mounts
}

// sourceSecretsWrapper injects `set -a; . /flint/secrets/env` sourcing into a
// shell command so secret values live only in the step process's environment
// — never in the container spec (which containerd persists to disk).
func sourceSecretsWrapper(command []string) []string {
	if len(command) == 3 && (command[0] == "/bin/sh" || command[0] == "sh") && command[1] == "-c" {
		return []string{command[0], "-c", "set -a; . /flint/secrets/env 2>/dev/null; set +a; " + command[2]}
	}
	// Non-shell entrypoints get the file mounted but not auto-sourced.
	return command
}

func sanitizeID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
