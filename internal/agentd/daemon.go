package agentd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	agentruntime "github.com/NerdMeNot/flint/internal/agentd/runtime"
	"github.com/NerdMeNot/flint/internal/version"
	agentv1 "github.com/NerdMeNot/flint/protogen/agent/v1"
)

// Daemon is the running agent: one registered machine executing assignments.
type Daemon struct {
	cfg    Config
	dirs   dirs
	client *client
	rt     agentruntime.Runtime
	id     identity
	data   *dataPlane

	mu       sync.Mutex
	active   map[string]*execution // assignment id → running execution
	draining bool
}

// Run boots the daemon and blocks until ctx is done or the server orders
// shutdown.
func Run(ctx context.Context, cfg Config) error {
	cfg.FromEnv()
	if err := cfg.Validate(); err != nil {
		return err
	}

	rt, err := buildRuntime(cfg)
	if err != nil {
		return err
	}
	if err := rt.Start(ctx); err != nil {
		return fmt.Errorf("agentd: runtime start: %w", err)
	}
	defer rt.Close() //nolint:errcheck

	c, err := dial(cfg)
	if err != nil {
		return fmt.Errorf("agentd: dial %s: %w", cfg.ServerURL, err)
	}
	defer c.close()

	data, err := newDataPlane(cfg.DataDir, 0)
	if err != nil {
		return fmt.Errorf("agentd: data plane init: %w", err)
	}

	d := &Daemon{
		cfg:    cfg,
		dirs:   dirs{root: cfg.DataDir},
		client: c,
		rt:     rt,
		data:   data,
		active: map[string]*execution{},
	}
	if err := d.registerOrResume(ctx); err != nil {
		return err
	}

	log.Info().Str("machine", d.id.MachineID).Str("pool", d.id.PoolName).
		Str("runtime", cfg.Runtime).Int("capacity", cfg.Capacity).
		Msg("agentd: ready")

	hbDone := make(chan string, 1) // heartbeat loop's terminal action
	go d.heartbeatLoop(ctx, hbDone)

	slots := make(chan struct{}, cfg.Capacity)
	for {
		select {
		case <-ctx.Done():
			return nil
		case action := <-hbDone:
			log.Info().Str("action", action).Msg("agentd: exiting on server command")
			return nil
		case slots <- struct{}{}:
		}

		if d.isDraining() {
			<-slots // release; drain claims nothing new
			select {
			case <-ctx.Done():
				return nil
			case action := <-hbDone:
				log.Info().Str("action", action).Msg("agentd: exiting on server command")
				return nil
			case <-time.After(2 * time.Second):
			}
			continue
		}

		resp, err := d.client.svc.ClaimStep(ctx, &agentv1.ClaimStepRequest{
			MachineId:   d.id.MachineID,
			WaitSeconds: 25,
		})
		if err != nil {
			<-slots
			if ctx.Err() != nil {
				return nil
			}
			log.Warn().Err(err).Msg("agentd: claim failed — backing off")
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
			}
			continue
		}
		if !resp.GetAssigned() {
			<-slots
			continue
		}

		assignment := resp.GetAssignment()
		go func() {
			defer func() { <-slots }()
			d.execute(ctx, assignment)
		}()
	}
}

// registerOrResume loads a persisted identity or registers with the token.
func (d *Daemon) registerOrResume(ctx context.Context) error {
	if id, err := loadIdentity(d.cfg.DataDir); err != nil {
		return err
	} else if id != nil {
		d.id = *id
		d.client.token = id.MachineToken
		log.Info().Str("machine", id.MachineID).Msg("agentd: resuming existing identity")
		return nil
	}

	if d.cfg.Token == "" {
		return fmt.Errorf("agentd: no stored identity and no registration token (--token / FLINT_AGENT_TOKEN)")
	}
	hostname, _ := os.Hostname()
	resp, err := d.client.svc.RegisterMachine(ctx, &agentv1.RegisterMachineRequest{
		RegistrationToken: d.cfg.Token,
		MachineId:         d.cfg.MachineID,
		Hostname:          hostname,
		Arch:              runtime.GOARCH,
		Os:                runtime.GOOS,
		CpuMillis:         int64(runtime.NumCPU()) * 1000,
		MemoryMb:          totalMemoryMB(),
		DiskGb:            freeDiskGB(d.cfg.DataDir),
		AgentVersion:      version.Version,
		Labels:            d.cfg.Labels,
	})
	if err != nil {
		return fmt.Errorf("agentd: registration failed: %w", err)
	}
	d.id = identity{
		MachineID:     resp.GetMachineId(),
		MachineToken:  resp.GetMachineToken(),
		PoolName:      resp.GetPoolName(),
		ServerHTTPURL: resp.GetServerHttpUrl(),
	}
	d.client.token = d.id.MachineToken
	if err := saveIdentity(d.cfg.DataDir, d.id); err != nil {
		return fmt.Errorf("agentd: persisting identity: %w", err)
	}
	log.Info().Str("machine", d.id.MachineID).Str("pool", d.id.PoolName).
		Msg("agentd: registered")
	return nil
}

// heartbeatLoop beats every interval; server commands (drain/shutdown,
// cancellations, workspace GC) apply here. Sends the terminal action to done.
func (d *Daemon) heartbeatLoop(ctx context.Context, done chan<- string) {
	interval := 10 * time.Second
	start := time.Now()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}

		resp, err := d.client.svc.Heartbeat(ctx, &agentv1.HeartbeatRequest{
			MachineId:           d.id.MachineID,
			ActiveAssignmentIds: d.activeIDs(),
			ResidentRunIds:      d.dirs.residentRunIDs(),
			Status: &agentv1.AgentStatus{
				FreeSlots:     int32(d.cfg.Capacity - len(d.activeIDs())),
				UptimeSeconds: int64(time.Since(start).Seconds()),
				AgentVersion:  version.Version,
			},
		})
		if err != nil {
			log.Warn().Err(err).Msg("agentd: heartbeat failed")
			continue
		}

		if iv := time.Duration(resp.GetHeartbeatIntervalSeconds()) * time.Second; iv > 0 && iv != interval {
			interval = iv
			t.Reset(interval)
		}

		for _, cancel := range resp.GetCancellations() {
			d.cancelAssignment(ctx, cancel.GetAssignmentId(), cancel.GetReason())
		}
		for _, runID := range resp.GetGcRunIds() {
			d.gcRun(runID)
		}

		switch resp.GetAction() {
		case "drain":
			d.setDraining(true)
			if len(d.activeIDs()) == 0 {
				done <- "drain"
				return
			}
		case "shutdown":
			done <- "shutdown"
			return
		}
	}
}

func (d *Daemon) isDraining() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.draining
}

func (d *Daemon) setDraining(v bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if v && !d.draining {
		log.Warn().Msg("agentd: draining — finishing running work, claiming nothing new")
	}
	d.draining = v
}

func (d *Daemon) activeIDs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	ids := make([]string, 0, len(d.active))
	for id := range d.active {
		ids = append(ids, id)
	}
	return ids
}

func (d *Daemon) cancelAssignment(ctx context.Context, assignmentID, reason string) {
	d.mu.Lock()
	ex := d.active[assignmentID]
	d.mu.Unlock()
	if ex == nil {
		return
	}
	log.Warn().Str("assignment", assignmentID).Str("reason", reason).
		Msg("agentd: cancelling step")
	ex.cancel(ctx)
}

// gcRun deletes a terminal run's workspace directory.
func (d *Daemon) gcRun(runID string) {
	// Never GC a run with active work (paranoia; the server only sends
	// terminal runs).
	d.mu.Lock()
	for _, ex := range d.active {
		if ex.runID == runID {
			d.mu.Unlock()
			return
		}
	}
	d.mu.Unlock()
	if err := os.RemoveAll(d.dirs.runRoot(runID)); err == nil {
		log.Debug().Str("run", runID).Msg("agentd: workspace GC")
	}
}

func buildRuntime(cfg Config) (agentruntime.Runtime, error) {
	switch cfg.Runtime {
	case "hostshell":
		log.Warn().Msg("agentd: hostshell runtime — steps run as UNISOLATED host processes (dev/test only)")
		return agentruntime.NewHostShell(), nil
	case "containerd":
		return agentruntime.NewContainerd(agentruntime.ContainerdConfig{DataDir: cfg.DataDir}), nil
	default:
		return nil, fmt.Errorf("agentd: unknown runtime %q (want containerd or hostshell)", cfg.Runtime)
	}
}
