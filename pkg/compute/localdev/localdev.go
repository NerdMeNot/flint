// Package localdev implements a Flint compute provider that provisions
// "machines" as local flint-agent child processes on the host — the
// "my laptop is the cloud" provider.
//
// Its job is to exercise the full elastic fleet path (Quote → rank → Create →
// register → claim → run → idle scale-down → Destroy) and populate the
// decision ledger, pool insights, and run placement with real data — on a
// developer machine, with no cloud account, cross-platform. Each Create spawns
// `flint-agent daemon --runtime hostshell`, so steps run as host processes:
// there is NO container isolation here (that is what the containerd runtime and
// the real cloud providers are for). Boot is near-instant, so provision and
// scale-down cycles happen fast enough to watch the ledger fill.
//
// Not for production. It shells out to a local binary and tracks agents in
// memory, so a control-plane restart orphans them (the heartbeat-lease sweep
// then reaps their machine rows).
package localdev

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NerdMeNot/flint/pkg/compute"
)

func init() { compute.Register("local", newProvider) }

// Config is the provider's freeform config (compute_providers.config). Every
// field has a dev-sensible default, so an empty {} works.
type Config struct {
	// AgentBinary is the host path to the flint-agent binary Create runs.
	AgentBinary string `json:"agentBinary"`
	// ServerGRPCURL overrides the bootstrap's server address. Child processes
	// dial the host directly, so a plain "localhost:9443" works — which the
	// bootstrap may not carry if the control plane advertises an
	// externally-routable address. Empty = use the bootstrap's value.
	ServerGRPCURL string `json:"serverGrpcUrl"`
	// DataRoot is where per-machine agent data dirs are created.
	DataRoot string `json:"dataRoot"`
	// Economics knobs, so the ledger and insights carry realistic numbers.
	PricePerHourUSD     float64 `json:"pricePerHourUsd"`
	ExpectedBootSeconds int     `json:"expectedBootSeconds"`
	// InstanceType is the label shown in offers/placement (cosmetic).
	InstanceType string `json:"instanceType"`
	// Capacity is the max concurrent steps each spawned agent advertises.
	Capacity int `json:"capacity"`
}

func (c *Config) applyDefaults() {
	if c.AgentBinary == "" {
		c.AgentBinary = "bin/flint-agent"
	}
	if c.DataRoot == "" {
		c.DataRoot = filepath.Join(os.TempDir(), "flint-localdev")
	}
	if c.PricePerHourUSD == 0 {
		c.PricePerHourUSD = 0.10
	}
	if c.ExpectedBootSeconds == 0 {
		c.ExpectedBootSeconds = 3
	}
	if c.InstanceType == "" {
		c.InstanceType = "local.host"
	}
	if c.Capacity == 0 {
		c.Capacity = 2
	}
}

// Provider spawns flint-agent child processes as elastic "machines".
type Provider struct {
	name string
	cfg  Config

	mu    sync.Mutex
	procs map[string]*exec.Cmd // machineID → running agent process
}

func newProvider(_ context.Context, name string, configJSON json.RawMessage, _ []byte) (compute.Provider, error) {
	var cfg Config
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &cfg); err != nil {
			return nil, fmt.Errorf("localdev: parse config: %w", err)
		}
	}
	cfg.applyDefaults()
	if _, err := exec.LookPath(cfg.AgentBinary); err != nil {
		// A relative path that exists is fine too (LookPath only resolves PATH
		// and absolute/dot paths); fall back to a stat.
		if _, statErr := os.Stat(cfg.AgentBinary); statErr != nil {
			return nil, fmt.Errorf("localdev: agent binary %q not found (build it with `task build BIN=flint-agent`): %w", cfg.AgentBinary, err)
		}
	}
	return &Provider{name: name, cfg: cfg, procs: map[string]*exec.Cmd{}}, nil
}

func (p *Provider) Name() string { return p.name }

// Quote offers one local machine at the configured price. It only offers when
// the requested arch matches the host (a local process can't emulate a
// different CPU) and never claims spot capacity — a laptop isn't preemptible.
func (p *Provider) Quote(_ context.Context, req compute.Requirements) ([]compute.Offer, error) {
	if req.Arch != "" && req.Arch != runtime.GOARCH {
		return nil, nil // can't satisfy a foreign arch locally
	}
	if req.GPU != nil {
		return nil, nil // no local accelerators
	}
	if req.Capacity == compute.CapacitySpot {
		return nil, nil // local capacity is never preemptible
	}
	arch := req.Arch
	if arch == "" {
		arch = runtime.GOARCH
	}
	return []compute.Offer{{
		Provider:            p.name,
		InstanceType:        p.cfg.InstanceType,
		Region:              "local",
		Capacity:            compute.CapacityOnDemand,
		CPUMillis:           req.CPUMillis,
		MemoryMB:            req.MemoryMB,
		DiskGB:              req.DiskGB,
		Arch:                arch,
		PricePerHourUSD:     p.cfg.PricePerHourUSD,
		ExpectedBootSeconds: p.cfg.ExpectedBootSeconds,
		ExpiresAt:           time.Now().Add(5 * time.Minute),
	}}, nil
}

// Create spawns a flint-agent process that registers with the bootstrap token.
// Idempotent per bootstrap.MachineID.
func (p *Provider) Create(_ context.Context, offer compute.Offer, bootstrap compute.Bootstrap) (compute.MachineRef, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ref := compute.MachineRef{Provider: p.name, ID: bootstrap.MachineID, State: compute.RefRunning}
	if _, alive := p.running()[bootstrap.MachineID]; alive {
		return ref, nil // already running (OS truth) — idempotent across restarts
	}

	server := p.cfg.ServerGRPCURL
	if server == "" {
		server = bootstrap.ServerGRPCURL
	}
	if server == "" {
		return compute.MachineRef{}, fmt.Errorf("localdev: no server gRPC URL (set the provider's serverGrpcUrl or wire the fleet bootstrap endpoints)")
	}

	dataDir := filepath.Join(p.cfg.DataRoot, bootstrap.MachineID)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return compute.MachineRef{}, fmt.Errorf("localdev: create data dir: %w", err)
	}

	//nolint:gosec // launching the configured local agent binary is the point
	cmd := exec.Command(p.cfg.AgentBinary, "daemon",
		"--server", server,
		"--token", bootstrap.RegistrationToken,
		"--machine-id", bootstrap.MachineID,
		"--runtime", "hostshell",
		"--insecure",
		"--data-dir", dataDir,
		"--capacity", strconv.Itoa(p.cfg.Capacity),
	)
	// Agent logs interleave into the server's stderr — visible in the dev loop.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "FLINT_LOCALDEV_MACHINE="+bootstrap.MachineID)
	if err := cmd.Start(); err != nil {
		return compute.MachineRef{}, fmt.Errorf("localdev: start agent: %w", err)
	}
	p.procs[bootstrap.MachineID] = cmd
	// Reap the child when it exits so ProcessState is populated for List.
	go func() { _ = cmd.Wait() }()
	return ref, nil
}

// Destroy stops the agent process for a ref and removes its data dir. It kills
// by OS truth (the running process whose --data-dir is this machine's), not the
// in-memory handle, so it works even after the provider was reconstructed.
func (p *Provider) Destroy(_ context.Context, ref compute.MachineRef) error {
	p.mu.Lock()
	if cmd, ok := p.procs[ref.ID]; ok && cmd.Process != nil {
		_ = cmd.Process.Kill()
		delete(p.procs, ref.ID)
	}
	p.mu.Unlock()
	if pid, ok := p.running()[ref.ID]; ok {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Kill()
		}
	}
	_ = os.RemoveAll(filepath.Join(p.cfg.DataRoot, ref.ID))
	return nil
}

// List returns the agents actually running for this provider — the reconcile
// source of truth. It scans OS processes (not an in-memory map), so it reports
// correctly even after the fleet reconstructs the provider instance and after a
// control-plane restart leaves orphaned agents. This matches the Provider
// contract's implicit requirement: List reflects external, re-queryable truth.
func (p *Provider) List(_ context.Context) ([]compute.MachineRef, error) {
	running := p.running()
	out := make([]compute.MachineRef, 0, len(running))
	for id := range running {
		out = append(out, compute.MachineRef{Provider: p.name, ID: id, State: compute.RefRunning})
	}
	return out, nil
}

// running scans the process table for this provider's live agents, keyed by
// machine id. An agent is ours when its command line carries a --data-dir under
// our DataRoot; the machine id is the leaf of that path (Create lays it out as
// <DataRoot>/<machineID>).
func (p *Provider) running() map[string]int {
	out := map[string]int{}
	psOut, err := exec.Command("ps", "-eo", "pid=,args=").Output()
	if err != nil {
		return out
	}
	prefix := p.cfg.DataRoot + string(os.PathSeparator)
	for line := range strings.SplitSeq(string(psOut), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "flint-agent") || !strings.Contains(line, "daemon") {
			continue
		}
		// The data-dir arg is <DataRoot>/<machineID>; the machine id is the leaf
		// after the prefix, up to the next space.
		_, afterPrefix, found := strings.Cut(line, prefix)
		if !found {
			continue
		}
		pidStr, _, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		if id, _, _ := strings.Cut(afterPrefix, " "); strings.TrimSpace(id) != "" {
			out[strings.TrimSpace(id)] = pid
		}
	}
	return out
}
