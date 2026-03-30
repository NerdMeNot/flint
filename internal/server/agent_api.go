package server

import (
	"context"
	"encoding/json"

	"github.com/NerdMeNot/flint/internal/observe"
	"github.com/NerdMeNot/flint/pkg/secret"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// handleAgentSecrets returns secret values for a step.
// Authenticated by task token (agent only, not exposed to users).
// The agent passes its org_id and a JSON array of secret names.
func (s *Server) handleAgentSecrets(ctx context.Context, c *app.RequestContext) {
	log := observe.Logger(ctx)

	orgID := string(c.GetHeader("X-Flint-Org-ID"))
	projectID := string(c.GetHeader("X-Flint-Project-ID"))
	environment := string(c.GetHeader("X-Flint-Environment"))
	namesRaw := string(c.GetHeader("X-Flint-Secret-Names"))

	if orgID == "" || namesRaw == "" {
		c.JSON(consts.StatusBadRequest, utils.H{"error": "missing required headers"})
		return
	}

	var names []string
	if err := json.Unmarshal([]byte(namesRaw), &names); err != nil {
		c.JSON(consts.StatusBadRequest, utils.H{"error": "invalid secret names"})
		return
	}

	if s.deps.Secrets == nil {
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "secret store not configured"})
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
		c.JSON(consts.StatusBadRequest, utils.H{"error": "missing X-Flint-Repo header"})
		return
	}

	// Look up the forge connection for this repo and get a token.
	creds, err := s.deps.Q.GetCloneCredentials(ctx, repo)
	if err != nil {
		log.Warn().Str("repo", repo).Msg("no forge connection for repo")
		c.JSON(consts.StatusNotFound, utils.H{"error": "no forge connection for repo"})
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
