package agentgrpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/fleet"
	"github.com/NerdMeNot/flint/pkg/logsink"
	agentv1 "github.com/NerdMeNot/flint/protogen/agent/v1"
)

// claimPollInterval is how often a parked ClaimStep re-checks for work. The
// scheduler-driven LISTEN/NOTIFY waker replaces this with sub-second wakeups;
// the poll stays as the portable fallback.
const claimPollInterval = 500 * time.Millisecond

// maxClaimWait caps the long-poll budget an agent may request.
const maxClaimWait = 30 * time.Second

// RegisterMachine exchanges a bootstrap/join token for a machine identity.
func (s *Server) RegisterMachine(ctx context.Context, req *agentv1.RegisterMachineRequest) (*agentv1.RegisterMachineResponse, error) {
	reg, err := s.fleet.Register(ctx, fleet.Registration{
		Token:        req.GetRegistrationToken(),
		MachineID:    req.GetMachineId(),
		Hostname:     req.GetHostname(),
		Arch:         req.GetArch(),
		OS:           req.GetOs(),
		CPUMillis:    req.GetCpuMillis(),
		MemoryMB:     req.GetMemoryMb(),
		DiskGB:       req.GetDiskGb(),
		AgentVersion: req.GetAgentVersion(),
		Labels:       req.GetLabels(),
	})
	if err != nil {
		if errors.Is(err, fleet.ErrBadRegistrationToken) {
			return nil, status.Error(codes.PermissionDenied, "registration token not recognized")
		}
		log.Error().Err(err).Msg("agentgrpc: registration failed")
		return nil, status.Error(codes.Internal, "registration failed")
	}
	return &agentv1.RegisterMachineResponse{
		MachineId:                reg.MachineID,
		MachineToken:             reg.MachineToken,
		HeartbeatIntervalSeconds: int32(reg.HeartbeatInterval.Seconds()),
		ServerHttpUrl:            s.cfg.ServerHTTPURL,
		PoolName:                 reg.PoolName,
	}, nil
}

// ClaimStep long-polls for work assigned to this machine.
func (s *Server) ClaimStep(ctx context.Context, req *agentv1.ClaimStepRequest) (*agentv1.ClaimStepResponse, error) {
	machine, ok := machineFromCtx(ctx)
	if !ok || machine.ID != req.GetMachineId() {
		return nil, status.Error(codes.PermissionDenied, "machine id does not match token identity")
	}

	wait := time.Duration(req.GetWaitSeconds()) * time.Second
	if wait <= 0 || wait > maxClaimWait {
		wait = maxClaimWait
	}
	deadline := time.Now().Add(wait)

	for {
		row, err := s.fleet.ClaimForMachine(ctx, machine.ID)
		if err != nil {
			log.Error().Err(err).Str("machine", machine.ID).Msg("agentgrpc: claim failed")
			return nil, status.Error(codes.Internal, "claim failed")
		}
		if row != nil {
			var payload agentv1.StepPayload
			if err := unmarshalPayload(row.Payload, &payload); err != nil {
				log.Error().Err(err).Str("assignment", row.ID).
					Msg("agentgrpc: corrupt assignment payload")
				return nil, status.Error(codes.Internal, "corrupt assignment payload")
			}
			return &agentv1.ClaimStepResponse{
				Assigned: true,
				Assignment: &agentv1.Assignment{
					AssignmentId: row.ID,
					RunId:        row.RunID,
					StepName:     row.StepName,
					Attempt:      row.Attempt,
					Payload:      &payload,
				},
			}, nil
		}
		if time.Now().After(deadline) {
			return &agentv1.ClaimStepResponse{Assigned: false}, nil
		}
		select {
		case <-ctx.Done():
			return &agentv1.ClaimStepResponse{Assigned: false}, nil
		case <-time.After(claimPollInterval):
		}
	}
}

// Heartbeat renews the lease and relays server commands.
func (s *Server) Heartbeat(ctx context.Context, req *agentv1.HeartbeatRequest) (*agentv1.HeartbeatResponse, error) {
	machine, ok := machineFromCtx(ctx)
	if !ok || machine.ID != req.GetMachineId() {
		return nil, status.Error(codes.PermissionDenied, "machine id does not match token identity")
	}
	res, err := s.fleet.Heartbeat(ctx, machine, fleet.HeartbeatInput{
		ActiveAssignmentIDs: req.GetActiveAssignmentIds(),
		ResidentRunIDs:      req.GetResidentRunIds(),
		DrainRequested:      req.GetDrainingRequested(),
		DrainReason:         req.GetDrainReason(),
		AgentVersion:        req.GetStatus().GetAgentVersion(),
	})
	if err != nil {
		log.Error().Err(err).Str("machine", machine.ID).Msg("agentgrpc: heartbeat failed")
		return nil, status.Error(codes.Internal, "heartbeat failed")
	}
	resp := &agentv1.HeartbeatResponse{
		Action:                   res.Action,
		HeartbeatIntervalSeconds: int32(res.Interval.Seconds()),
		GcRunIds:                 res.GCRunIDs,
	}
	for _, c := range res.Cancellations {
		resp.Cancellations = append(resp.Cancellations, &agentv1.CancelStep{
			AssignmentId: c.AssignmentID, RunId: c.RunID, StepName: c.StepName,
			Reason: "user_cancelled",
		})
	}
	return resp, nil
}

// ReportStepComplete routes the result through the engine's idempotent
// CompleteStep chokepoint (task-token verified), then finalizes the
// assignment and the machine's busy/idle state.
func (s *Server) ReportStepComplete(ctx context.Context, req *agentv1.ReportStepCompleteRequest) (*agentv1.ReportStepCompleteResponse, error) {
	machine, ok := machineFromCtx(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no machine identity")
	}
	r := req.GetResult()
	success := r.GetStatus() == "succeeded"
	result := engine.StepResult{
		Success:  success,
		ExitCode: int(r.GetExitCode()),
		Outputs:  r.GetOutputs(),
		Error:    r.GetErrorMessage(),
	}
	if err := s.eng.CompleteStep(ctx, req.GetTaskToken(), result); err != nil {
		log.Error().Err(err).Str("assignment", req.GetAssignmentId()).
			Msg("agentgrpc: CompleteStep rejected")
		return nil, status.Error(codes.InvalidArgument, "step completion rejected: "+err.Error())
	}

	assignmentStatus := "succeeded"
	var errMsg *string
	if !success {
		assignmentStatus = "failed"
		if m := r.GetErrorMessage(); m != "" {
			errMsg = &m
		}
	}
	if err := s.fleet.CompleteAssignment(ctx, req.GetAssignmentId(), machine.ID, assignmentStatus, errMsg); err != nil {
		// The engine already accepted the result — never fail the agent here.
		log.Error().Err(err).Str("assignment", req.GetAssignmentId()).
			Msg("agentgrpc: assignment finalization failed (engine already accepted result)")
	}
	return &agentv1.ReportStepCompleteResponse{Accepted: true}, nil
}

// ExecuteStep receives the per-step stream: a hello frame binding it to an
// assignment, then log batches which land in the server's durable log sink.
// Cancellation is pushed back when the assignment is flagged.
func (s *Server) ExecuteStep(stream agentv1.AgentService_ExecuteStepServer) error {
	ctx := stream.Context()
	machine, ok := machineFromCtx(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "no machine identity")
	}

	var (
		bound    bool
		orgID    string
		runID    string
		stepName string
		matrix   string
		lastSeq  int64
	)

	// Cancellation watcher: poll the assignment's cancel flag while the
	// stream lives (cheap single-row read; heartbeat is the redundant path).
	cancelCh := make(chan string, 1)
	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		select {
		case aid := <-cancelCh:
			_ = stream.Send(&agentv1.ExecuteStepResponse{Payload: &agentv1.ExecuteStepResponse_Cancel{
				Cancel: &agentv1.CancelStep{AssignmentId: aid, RunId: runID, StepName: stepName, Reason: "user_cancelled"},
			}})
		default:
		}

		switch p := msg.GetPayload().(type) {
		case *agentv1.ExecuteStepRequest_Hello:
			if p.Hello.GetMachineId() != machine.ID {
				return status.Error(codes.PermissionDenied, "hello machine id does not match token identity")
			}
			// The assignment row is the authoritative binding: this machine
			// must be the one the scheduler bound the assignment to.
			a, err := s.fleet.GetAssignment(ctx, p.Hello.GetAssignmentId())
			if err != nil {
				return status.Error(codes.NotFound, "unknown assignment")
			}
			if a.MachineID == nil || *a.MachineID != machine.ID {
				return status.Error(codes.PermissionDenied, "assignment is not bound to this machine")
			}
			var payload agentv1.StepPayload
			_ = json.Unmarshal(a.Payload, &payload)
			orgID, runID, stepName = payload.GetOrgId(), a.RunID, a.StepName
			bound = true
			go s.watchCancellation(watchCtx, a.ID, cancelCh)

		case *agentv1.ExecuteStepRequest_LogBatch:
			if !bound {
				return status.Error(codes.FailedPrecondition, "log batch before stream hello")
			}
			matrix = p.LogBatch.GetMatrixKey()
			lines := make([]logsink.LogLine, 0, len(p.LogBatch.GetLines()))
			for _, l := range p.LogBatch.GetLines() {
				// Dedupe retransmits after a reconnect: sequences are
				// monotonic per step.
				if l.GetSequence() > 0 && l.GetSequence() <= lastSeq {
					continue
				}
				if l.GetSequence() > 0 {
					lastSeq = l.GetSequence()
				}
				lines = append(lines, logsink.LogLine{
					Timestamp: l.GetTimestamp().AsTime(),
					Stream:    l.GetStream(),
					Content:   l.GetContent(),
				})
			}
			if len(lines) > 0 {
				ref := logsink.LogRef{OrgID: orgID, RunID: runID, StepName: stepName, MatrixKey: matrix}
				if err := s.logs.Write(ctx, ref, lines); err != nil {
					log.Warn().Err(err).Str("run", runID).Str("step", stepName).
						Msg("agentgrpc: log write failed")
					continue // don't ack what didn't persist
				}
			}
			_ = stream.Send(&agentv1.ExecuteStepResponse{Payload: &agentv1.ExecuteStepResponse_LogAck{
				LogAck: &agentv1.LogAck{AckedSequence: lastSeq},
			}})

		case *agentv1.ExecuteStepRequest_Heartbeat:
			_ = stream.Send(&agentv1.ExecuteStepResponse{Payload: &agentv1.ExecuteStepResponse_Ack{
				Ack: &agentv1.AckHeartbeat{},
			}})

		case *agentv1.ExecuteStepRequest_StepStarted, *agentv1.ExecuteStepRequest_StepProgress:
			// Informational; the durable lifecycle lives in the engine.
		}
	}
}

// watchCancellation polls the assignment's cancel flag and pushes at most one
// cancellation into ch.
func (s *Server) watchCancellation(ctx context.Context, assignmentID string, ch chan<- string) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a, err := s.fleet.GetAssignment(ctx, assignmentID)
			if err != nil {
				continue
			}
			if a.CancelRequested {
				select {
				case ch <- assignmentID:
				default:
				}
				return
			}
		}
	}
}

// unmarshalPayload decodes the persisted assignment payload (stored as
// protojson so the DB row stays inspectable) into the proto message.
func unmarshalPayload(data []byte, payload *agentv1.StepPayload) error {
	return json.Unmarshal(data, payload)
}
