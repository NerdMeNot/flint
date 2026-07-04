package agentgrpc

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/secret"
	agentv1 "github.com/NerdMeNot/flint/protogen/agent/v1"
)

// GetStepSecrets resolves the step's secret mapping at execution time. The
// assignment row is the authorization: it must be live and bound to the
// calling machine, and its persisted payload carries the scope (org/project/
// environment) and the env→store-name mapping.
func (s *Server) GetStepSecrets(ctx context.Context, req *agentv1.GetStepSecretsRequest) (*agentv1.GetStepSecretsResponse, error) {
	a, payload, err := s.assignmentForMachine(ctx, req.GetAssignmentId())
	if err != nil {
		return nil, err
	}
	if a.Status != "running" && a.Status != "assigned" {
		return nil, status.Error(codes.FailedPrecondition, "assignment is not live")
	}
	if len(payload.GetSecretMapping()) == 0 {
		return &agentv1.GetStepSecretsResponse{}, nil
	}
	if s.secrets == nil {
		return nil, status.Error(codes.FailedPrecondition, "secret store not configured on the server")
	}

	orgID, projectID, environment := payload.GetOrgId(), payload.GetProjectId(), payload.GetEnvironment()
	out := make(map[string]string, len(payload.GetSecretMapping()))
	for envVar, storeName := range payload.GetSecretMapping() {
		val, err := s.lookupSecret(ctx, orgID, projectID, environment, storeName)
		if err != nil {
			return nil, status.Error(codes.NotFound,
				fmt.Sprintf("secret %q not found in environment/project/org scope", storeName))
		}
		out[envVar] = val
	}
	return &agentv1.GetStepSecretsResponse{Secrets: out}, nil
}

// lookupSecret resolves one secret with environment → project → org fallback,
// mirroring the HTTP agent endpoint's scope rules.
func (s *Server) lookupSecret(ctx context.Context, orgID, projectID, environment, name string) (string, error) {
	if environment != "" {
		if val, err := s.secrets.Get(ctx, secret.Ref{
			OrgID: orgID, ProjectID: projectID, Environment: environment, Name: name,
		}); err == nil {
			return val, nil
		}
	}
	if val, err := s.secrets.Get(ctx, secret.Ref{
		OrgID: orgID, ProjectID: projectID, Name: name,
	}); err == nil {
		return val, nil
	}
	return s.secrets.Get(ctx, secret.Ref{OrgID: orgID, Name: name})
}

// GetCloneToken returns the clone URL (and forge token, once connections mint
// per-clone tokens) for the step's repository.
func (s *Server) GetCloneToken(ctx context.Context, req *agentv1.GetCloneTokenRequest) (*agentv1.GetCloneTokenResponse, error) {
	_, payload, err := s.assignmentForMachine(ctx, req.GetAssignmentId())
	if err != nil {
		return nil, err
	}
	repo := payload.GetRepo()
	if repo == "" {
		return nil, status.Error(codes.FailedPrecondition, "step has no associated repo")
	}
	creds, err := db.New(s.fleet.Pool()).GetCloneCredentials(ctx, repo)
	if err != nil {
		return nil, status.Error(codes.NotFound, "no forge connection for repo")
	}
	return &agentv1.GetCloneTokenResponse{
		CloneUrl: cloneURLForForge(creds.ForgeType, repo),
	}, nil
}

// assignmentForMachine loads the assignment, checks it is bound to the calling
// machine, and parses its payload.
func (s *Server) assignmentForMachine(ctx context.Context, assignmentID string) (db.StepAssignment, *agentv1.StepPayload, error) {
	machine, ok := machineFromCtx(ctx)
	if !ok {
		return db.StepAssignment{}, nil, status.Error(codes.Unauthenticated, "no machine identity")
	}
	a, err := s.fleet.GetAssignment(ctx, assignmentID)
	if err != nil {
		return db.StepAssignment{}, nil, status.Error(codes.NotFound, "unknown assignment")
	}
	if a.MachineID == nil || *a.MachineID != machine.ID {
		return db.StepAssignment{}, nil, status.Error(codes.PermissionDenied, "assignment is not bound to this machine")
	}
	var payload agentv1.StepPayload
	if err := json.Unmarshal(a.Payload, &payload); err != nil {
		return db.StepAssignment{}, nil, status.Error(codes.Internal, "corrupt assignment payload")
	}
	return a, &payload, nil
}

// cloneURLForForge maps a forge type to its https clone URL pattern.
func cloneURLForForge(forgeType, repo string) string {
	switch forgeType {
	case "gitlab":
		return "https://gitlab.com/" + repo + ".git"
	case "bitbucket":
		return "https://bitbucket.org/" + repo + ".git"
	default:
		return "https://github.com/" + repo + ".git"
	}
}
