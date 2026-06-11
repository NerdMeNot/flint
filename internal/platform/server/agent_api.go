package server

import (
	"context"
	"encoding/json"

	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/pkg/secret"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// handleAgentSecrets returns secret values for a step.
//
// The secret scope (org/project/environment) is derived SERVER-SIDE from the
// signed task token's workflow, NOT from client headers — a step pod runs
// untrusted user code and must not be able to read another run's secrets by
// rewriting headers. The agent only chooses WHICH secret names it wants; the
// scope it can reach is fixed by the run that minted its token.
func (s *Server) handleAgentSecrets(ctx context.Context, c *app.RequestContext) {
	log := observe.Logger(ctx)

	tokenStr := string(c.GetHeader("X-Flint-Task-Token"))
	if tokenStr == "" {
		apiUnauthorized(ctx, c, "missing task token")
		return
	}
	tok, err := engine.DecodeTaskToken(tokenStr, []byte(s.deps.Config.Auth.JWT.Secret))
	if err != nil {
		apiUnauthorized(ctx, c, "invalid task token")
		return
	}

	// Derive the run's identity from its workflow input (the trusted source).
	inputJSON, err := s.deps.Q.GetWorkflowInput(ctx, tok.WorkflowID)
	if err != nil {
		apiNotFound(ctx, c, "run not found")
		return
	}
	var input engine.StartWorkflowInput
	if err := json.Unmarshal(inputJSON, &input); err != nil {
		apiInternal(ctx, c, "invalid run input")
		return
	}
	orgID, projectID, environment := input.OrgID, input.ProjectID, input.Environment

	namesRaw := string(c.GetHeader("X-Flint-Secret-Names"))
	if namesRaw == "" {
		apiBadRequest(ctx, c, "missing secret names")
		return
	}

	var names []string
	if err := json.Unmarshal([]byte(namesRaw), &names); err != nil {
		apiBadRequest(ctx, c, "invalid secret names")
		return
	}

	if s.deps.Secrets == nil {
		apiInternal(ctx, c, "secret store not configured")
		return
	}

	secrets := make(map[string]string, len(names))
	for _, name := range names {
		var val string
		var err error

		// Try environment-scoped first (if environment is set).
		if environment != "" {
			val, err = s.deps.Secrets.Get(ctx, secret.Ref{
				OrgID: orgID, ProjectID: projectID, Environment: environment, Name: name,
			})
		}

		// Fall back to project-scoped.
		if err != nil || environment == "" {
			val, err = s.deps.Secrets.Get(ctx, secret.Ref{
				OrgID: orgID, ProjectID: projectID, Name: name,
			})
		}

		// Fall back to org-scoped.
		if err != nil {
			val, err = s.deps.Secrets.Get(ctx, secret.Ref{OrgID: orgID, Name: name})
		}

		if err != nil {
			log.Warn().Str("secret", name).Msg("secret not found")
			continue
		}
		secrets[name] = val
	}

	c.JSON(consts.StatusOK, utils.H{"secrets": secrets})
}

// handleAgentCloneToken returns a short-lived token for cloning a repo.
// The agent uses this to authenticate git clone for private repos.
func (s *Server) handleAgentCloneToken(ctx context.Context, c *app.RequestContext) {
	log := observe.Logger(ctx)

	repo := string(c.GetHeader("X-Flint-Repo"))
	if repo == "" {
		apiBadRequest(ctx, c, "missing X-Flint-Repo header")
		return
	}

	// Look up the forge connection for this repo and get a token.
	creds, err := s.deps.Q.GetCloneCredentials(ctx, repo)
	if err != nil {
		log.Warn().Str("repo", repo).Msg("no forge connection for repo")
		apiNotFound(ctx, c, "no forge connection for repo")
		return
	}

	// For now, return the forge type so the agent knows the clone URL pattern.
	// The actual token comes from the forge connection credentials.
	// In production, decrypt credentialsEnc and extract the installation token.
	c.JSON(consts.StatusOK, utils.H{
		"forgeType": creds.ForgeType,
		"cloneURL":  cloneURLForForge(creds.ForgeType, repo),
	})
}

func cloneURLForForge(forgeType, repo string) string {
	switch forgeType {
	case "github":
		return "https://github.com/" + repo + ".git"
	case "gitlab":
		return "https://gitlab.com/" + repo + ".git"
	case "bitbucket":
		return "https://bitbucket.org/" + repo + ".git"
	default:
		return "https://github.com/" + repo + ".git"
	}
}
