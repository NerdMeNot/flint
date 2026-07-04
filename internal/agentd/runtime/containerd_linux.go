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
	cni    *cniManager

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

	// Step-with-services state, torn down in Remove.
	services  []*svcHandle
	netnsPath string
}

type svcHandle struct {
	container containerd.Container
	task      containerd.Task
}

func (h *cdHandle) ID() string { return h.id }

// NewContainerd builds the production runtime.
func NewContainerd(cfg ContainerdConfig) *Containerd {
	if cfg.BundleDir == "" {
		cfg.BundleDir = filepath.Join(cfg.DataDir, "bin")
	}
	return &Containerd{
		cfg:   cfg,
		tasks: map[string]*cdHandle{},
		cni:   &cniManager{bundleDir: cfg.BundleDir},
	}
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
	if n := sweepNetNS(); n > 0 {
		log.Warn().Int("netns", n).Msg("agentd: removed stale step network namespaces")
	}
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

# Kubernetes' CRI surface is dead weight here (and drags CNI config checks).
disabled_plugins = ['io.containerd.cri.v1.runtime', 'io.containerd.cri.v1.images', 'io.containerd.grpc.v1.cri']

[grpc]
  address = %q
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
// it with stdio attached. Steps with services get a private network namespace
// shared with their service containers; service-less steps use host
// networking (no CNI dependency).
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

	r.mu.Lock()
	r.seq++
	id := fmt.Sprintf("flint-%s-%d", sanitizeID(spec.StepName), r.seq)
	r.mu.Unlock()

	h := &cdHandle{id: id}
	// One failure-cleanup path for everything allocated below.
	fail := func(err error) (Handle, error) {
		r.teardownServices(context.WithoutCancel(ctx), h)
		return nil, err
	}

	// Networking: services ⇒ a pinned netns shared by step + services, NAT'd
	// through the flint bridge, with service names aliased to 127.0.0.1.
	netOpts := []oci.SpecOpts{oci.WithHostNamespace("network"), oci.WithHostResolvconf}
	var hostsMount []specs.Mount
	if len(spec.Services) > 0 {
		nsPath, err := createNetNS(id)
		if err != nil {
			return nil, err
		}
		h.netnsPath = nsPath
		if err := r.cni.Setup(ctx, id, nsPath); err != nil {
			return fail(err)
		}
		hostsPath, err := writeHostsFile(spec.IODir, spec.Services)
		if err != nil {
			return fail(err)
		}
		hostsMount = []specs.Mount{{
			Destination: "/etc/hosts", Type: "bind",
			Source: hostsPath, Options: []string{"rbind", "ro"},
		}}
		netOpts = []oci.SpecOpts{
			oci.WithLinuxNamespace(specs.LinuxNamespace{Type: specs.NetworkNamespace, Path: nsPath}),
			oci.WithHostResolvconf,
		}

		for _, svc := range spec.Services {
			if err := r.startService(ctx, h, spec, svc, hostsMount); err != nil {
				return fail(fmt.Errorf("service %s: %w", svc.Name, err))
			}
		}
	}

	opts := []oci.SpecOpts{
		oci.WithImageConfig(image),
		oci.WithProcessArgs(command...),
		oci.WithProcessCwd("/workspace"),
		oci.WithEnv(env),
		oci.WithMounts(append(stepMounts(spec), hostsMount...)),
	}
	opts = append(opts, netOpts...)
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

	container, err := r.client.NewContainer(ctx, id,
		containerd.WithNewSnapshot(id+"-snap", image),
		containerd.WithNewSpec(opts...),
		containerd.WithContainerLabels(map[string]string{
			"flint.run":  spec.RunID,
			"flint.step": spec.StepName,
		}),
	)
	if err != nil {
		return fail(fmt.Errorf("containerd: create container: %w", err))
	}
	h.container = container

	task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, spec.Stdout, spec.Stderr)))
	if err != nil {
		_ = container.Delete(ctx, containerd.WithSnapshotCleanup)
		return fail(fmt.Errorf("containerd: create task: %w", err))
	}
	exitCh, err := task.Wait(ctx)
	if err != nil {
		_, _ = task.Delete(ctx, containerd.WithProcessKill)
		_ = container.Delete(ctx, containerd.WithSnapshotCleanup)
		return fail(err)
	}
	if err := task.Start(ctx); err != nil {
		_, _ = task.Delete(ctx, containerd.WithProcessKill)
		_ = container.Delete(ctx, containerd.WithSnapshotCleanup)
		return fail(fmt.Errorf("containerd: start task: %w", err))
	}

	h.task = task
	h.exitCh = exitCh
	r.mu.Lock()
	r.tasks[id] = h
	r.mu.Unlock()
	return h, nil
}

// startService runs one service container in the step's netns. The image's
// own entrypoint runs; output interleaves into the step's log stream (boot
// noise beats invisible failures). Readiness is the step's business — the
// usual wait-for-port loop — matching what CI users already write.
func (r *Containerd) startService(ctx context.Context, h *cdHandle, spec StepSpec, svc ServiceSpec, hostsMount []specs.Mount) error {
	img, err := r.ensureImage(ctx, svc.Image)
	if err != nil {
		return err
	}
	id := h.id + "-svc-" + sanitizeID(svc.Name)
	container, err := r.client.NewContainer(ctx, id,
		containerd.WithNewSnapshot(id+"-snap", img),
		containerd.WithNewSpec(
			oci.WithImageConfig(img),
			oci.WithEnv(svc.Env),
			oci.WithMounts(hostsMount),
			oci.WithLinuxNamespace(specs.LinuxNamespace{Type: specs.NetworkNamespace, Path: h.netnsPath}),
			oci.WithHostResolvconf,
		),
		containerd.WithContainerLabels(map[string]string{
			"flint.run":     spec.RunID,
			"flint.step":    spec.StepName,
			"flint.service": svc.Name,
		}),
	)
	if err != nil {
		return err
	}
	task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, spec.Stdout, spec.Stderr)))
	if err != nil {
		_ = container.Delete(ctx, containerd.WithSnapshotCleanup)
		return err
	}
	if err := task.Start(ctx); err != nil {
		_, _ = task.Delete(ctx, containerd.WithProcessKill)
		_ = container.Delete(ctx, containerd.WithSnapshotCleanup)
		return err
	}
	h.services = append(h.services, &svcHandle{container: container, task: task})
	return nil
}

// teardownServices kills service containers and releases the step's netns +
// bridge attachment. Safe on partially-constructed handles.
func (r *Containerd) teardownServices(ctx context.Context, h *cdHandle) {
	ctx = namespaces.WithNamespace(ctx, containerdNamespace)
	for _, s := range h.services {
		_, _ = s.task.Delete(ctx, containerd.WithProcessKill)
		_ = s.container.Delete(ctx, containerd.WithSnapshotCleanup)
	}
	h.services = nil
	if h.netnsPath != "" {
		r.cni.Teardown(ctx, h.id, h.netnsPath)
		removeNetNS(h.netnsPath)
		h.netnsPath = ""
	}
}

// writeHostsFile emits the step's /etc/hosts: service names alias 127.0.0.1
// because step and services share one network namespace.
func writeHostsFile(ioDir string, services []ServiceSpec) (string, error) {
	var b strings.Builder
	b.WriteString("127.0.0.1 localhost")
	for _, s := range services {
		b.WriteString(" " + s.Name)
	}
	b.WriteString("\n::1 localhost\n")
	path := filepath.Join(ioDir, "hosts")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("write hosts file: %w", err)
	}
	return path, nil
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
	r.teardownServices(ctx, h)
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
