package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/runner"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/NerdMeNot/flint/pkg/units"
	agentv1 "github.com/NerdMeNot/flint/protogen/agent/v1"
)

// machineExecutor dispatches container steps (run/use/steps) to the machine
// fleet: Dispatch persists a step_assignments row — the pending-capacity queue
// — and returns immediately. The fleet scheduler binds it to a machine; the
// flint-agent daemon claims, executes, and reports completion through the
// engine's CompleteStep chokepoint. Dispatch never blocks on capacity: boot-
// new decisions belong to the fleet manager.
type machineExecutor struct {
	pool *pgxpool.Pool
	reg  *runner.Registry
	cfg  MachineExecutorConfig
}

// MachineExecutorConfig carries the endpoints stamped into step payloads.
type MachineExecutorConfig struct {
	// ServerHTTPURL is the base URL agents use for the HTTP data-plane
	// endpoints (secrets, clone tokens).
	ServerHTTPURL string
	// S3Bucket/S3Region/S3Endpoint enable the agent's artifact, cache, and S3
	// workspace paths; an empty bucket disables them.
	S3Bucket   string
	S3Region   string
	S3Endpoint string
}

// NewMachineExecutor builds the fleet-backed step executor.
func NewMachineExecutor(pool *pgxpool.Pool, reg *runner.Registry, cfg MachineExecutorConfig) StepExecutor {
	return &machineExecutor{pool: pool, reg: reg, cfg: cfg}
}

func (e *machineExecutor) Kind() string { return "machine" }

// Dispatch resolves the pool and resource envelope, persists the full dispatch
// payload, and returns the assignment id as the correlation handle.
func (e *machineExecutor) Dispatch(ctx context.Context, step claimedStep) (string, error) {
	var stepDef pipeline.Step
	if err := json.Unmarshal(step.stepDef, &stepDef); err != nil {
		return "", fmt.Errorf("engine: unmarshal step def: %w", err)
	}

	// Container image: step > pipeline default. Required — the production
	// runtime executes containers (loud rejection beats a machine-side error).
	if stepDef.Image == "" {
		stepDef.Image = step.pipelineImage
	}
	if stepDef.Image == "" {
		return "", fmt.Errorf("engine: step %q has no container image (set image: on the step or at pipeline level)", step.name)
	}

	poolSpec, err := e.reg.Resolve(stepDef.Runner)
	if err != nil {
		return "", fmt.Errorf("engine: resolve runner pool: %w", err)
	}
	if poolSpec.ID == "" {
		return "", fmt.Errorf("engine: runner pool %q has no id (not loaded from the DB?)", poolSpec.Name)
	}

	cpuMillis, memoryMB, diskGB, err := resolveStepResources(&stepDef, poolSpec)
	if err != nil {
		return "", fmt.Errorf("engine: step %q resources: %w", step.name, err)
	}

	payload := buildStepPayload(step, &stepDef, e.cfg, cpuMillis, memoryMB, diskGB)
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("engine: marshal step payload: %w", err)
	}

	assignmentID, err := db.New(e.pool).InsertStepAssignment(ctx, db.InsertStepAssignmentParams{
		StepID:     step.id,
		WorkflowID: step.workflowID,
		RunID:      step.runID,
		StepName:   step.name,
		Attempt:    int32(step.attempt),
		PoolID:     poolSpec.ID,
		CpuMillis:  cpuMillis,
		MemoryMb:   memoryMB,
		DiskGb:     diskGB,
		Payload:    payloadJSON,
	})
	if err != nil {
		return "", fmt.Errorf("engine: insert step assignment: %w", err)
	}

	log.Info().
		Str("assignment", assignmentID).
		Str("step", step.name).
		Str("pool", poolSpec.Name).
		Int64("cpuMillis", cpuMillis).
		Int64("memoryMB", memoryMB).
		Msg("engine: step assigned to fleet")
	return assignmentID, nil
}

// CleanupRun cancels queued work and flags running work for cancellation when
// a run reaches a terminal state. Workspace-directory GC on the machines rides
// the heartbeat channel, not this call.
func (e *machineExecutor) CleanupRun(ctx context.Context, runID string) error {
	q := db.New(e.pool)
	if _, err := q.CancelRunAssignments(ctx, runID); err != nil {
		return err
	}
	if n, err := q.RequestAssignmentCancel(ctx, runID); err != nil {
		return err
	} else if n > 0 {
		log.Info().Str("runID", runID).Int64("running", n).
			Msg("engine: cancellation requested for running assignments")
	}
	return nil
}

// resolveStepResources computes the assignment's resource envelope:
// step-explicit resources win, then the pool's default shape, then a modest
// floor so bin-packing always has real numbers.
func resolveStepResources(stepDef *pipeline.Step, poolSpec *runner.PoolSpec) (cpuMillis, memoryMB, diskGB int64, err error) {
	cpuMillis = poolSpec.Resources.CPUMillis
	memoryMB = poolSpec.Resources.MemoryMB
	diskGB = poolSpec.DiskGB

	if r := stepDef.Resources; r != nil {
		if r.CPU != "" {
			if cpuMillis, err = units.ParseCPUMillis(r.CPU); err != nil {
				return 0, 0, 0, err
			}
		}
		if r.Memory != "" {
			if memoryMB, err = units.ParseMemoryMB(r.Memory); err != nil {
				return 0, 0, 0, err
			}
		}
	}
	if stepDef.Disk != "" {
		if diskGB, err = units.ParseDiskGB(stepDef.Disk); err != nil {
			return 0, 0, 0, err
		}
	}
	if cpuMillis <= 0 {
		cpuMillis = 500
	}
	if memoryMB <= 0 {
		memoryMB = 512
	}
	return cpuMillis, memoryMB, diskGB, nil
}

// buildStepPayload projects the engine's dispatch context into the agent's
// StepPayload. Persisted at dispatch time so agents can claim later — or after
// a control-plane restart — without recomputing env or secret merges.
func buildStepPayload(step claimedStep, stepDef *pipeline.Step, cfg MachineExecutorConfig, cpuMillis, memoryMB, diskGB int64) *agentv1.StepPayload {
	p := &agentv1.StepPayload{
		StepId:        step.id,
		WorkflowId:    step.workflowID,
		StepName:      step.name,
		ExecType:      step.execType,
		StepDefJson:   step.stepDef,
		TaskToken:     step.taskToken,
		RunId:         step.runID,
		OrgId:         step.orgID,
		ProjectId:     step.projectID,
		Repo:          step.repo,
		Ref:           step.ref,
		CommitSha:     step.commitSHA,
		TriggerType:   step.triggerType,
		Environment:   step.environment,
		PipelineImage: step.pipelineImage,
		Env:           step.env,
		SecretMapping: step.secretMapping,
		ServerHttpUrl: cfg.ServerHTTPURL,
		MatrixKey:     extractMatrixKey(step.name),
		Resources: &agentv1.StepResources{
			CpuMillis: cpuMillis,
			MemoryMb:  memoryMB,
			DiskGb:    diskGB,
		},
	}
	if len(step.needsOutputs) > 0 {
		p.NeedsOutputsJson, _ = json.Marshal(step.needsOutputs)
	}
	if stepDef.Timeout != "" {
		if d, err := time.ParseDuration(stepDef.Timeout); err == nil {
			p.TimeoutSeconds = int64(d.Seconds())
		}
	}
	if cfg.S3Bucket != "" {
		p.Storage = &agentv1.ObjectStorage{
			Bucket: cfg.S3Bucket, Region: cfg.S3Region, Endpoint: cfg.S3Endpoint,
		}
	}
	return p
}
