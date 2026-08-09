package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func (s *Server) handleGetTeam(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")

	t, err := s.deps.Q.GetTeam(ctx, id)
	if err != nil {
		apiNotFound(ctx, c, "team not found")
		return
	}
	team := teamWithMembersResponse{
		ID:       t.ID,
		Name:     t.Name,
		Slug:     t.Slug,
		Source:   t.Source,
		IDPGroup: t.IdpGroup,
	}
	if team.Source == "" {
		team.Source = "internal"
	}

	// Get members.
	members, err := s.deps.Q.ListTeamMembers(ctx, id)
	if err != nil {
		team.Members = []teamMemberResponse{}
	} else {
		team.Members = make([]teamMemberResponse, 0, len(members))
		for _, m := range members {
			team.Members = append(team.Members, teamMemberResponse{
				ID:    m.ID,
				Email: m.Email,
				Name:  m.Name,
			})
		}
	}
	team.MemberCount = len(team.Members)

	c.JSON(consts.StatusOK, team)
}

// ── Roles -- update ──────────────────────────────────────────
