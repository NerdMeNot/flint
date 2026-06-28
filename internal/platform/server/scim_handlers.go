package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/jackc/pgx/v5"
)

// SCIM 2.0 provisioning handlers. SCIM User id == Flint user UUID; SCIM Group id
// == Flint team UUID. Groups created via SCIM are marked source='scim'. After
// membership or active-state changes, Casbin policies are regenerated for the
// affected users so access reflects provisioning immediately.

// scimAudit records a SCIM provisioning mutation (org from the bearer token, no
// user JWT) for the audit log.
func (s *Server) scimAudit(ctx context.Context, c *app.RequestContext, action, resourceType, resourceID string) {
	s.auditEvent(ctx, scimOrgFromCtx(ctx), "", action, resourceType, resourceID, extractClientIP(c))
}

// ── Discovery ──────────────────────────────────────────────────────────────

func (s *Server) handleSCIMServiceProviderConfig(ctx context.Context, c *app.RequestContext) {
	supported := func(v bool) map[string]any { return map[string]any{"supported": v} }
	writeSCIM(c, consts.StatusOK, map[string]any{
		"schemas":               []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"patch":                 supported(true),
		"bulk":                  map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":                map[string]any{"supported": true, "maxResults": 200},
		"changePassword":        supported(false),
		"sort":                  supported(false),
		"etag":                  supported(false),
		"authenticationSchemes": []map[string]any{{"type": "oauthbearertoken", "name": "OAuth Bearer Token"}},
		"meta":                  map[string]any{"resourceType": "ServiceProviderConfig", "location": s.scimBaseURL() + "/scim/v2/ServiceProviderConfig"},
	})
}

// ── Users ──────────────────────────────────────────────────────────────────

func (s *Server) handleSCIMListUsers(ctx context.Context, c *app.RequestContext) {
	orgID := scimOrgFromCtx(ctx)
	users, err := s.deps.Q.ListScimUsers(ctx, orgID)
	if err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to list users")
		return
	}
	fAttr, fVal, ok := scimFilterEq(string(c.Query("filter")))
	if !ok {
		writeSCIMError(c, consts.StatusBadRequest, `unsupported filter; only a single 'attr eq "value"' clause is supported`)
		return
	}

	resources := make([]any, 0, len(users))
	for _, u := range users {
		if fVal != "" {
			switch fAttr {
			case "username", "emails", "emails.value":
				if !strings.EqualFold(u.Email, fVal) {
					continue
				}
			case "externalid":
				if u.ExternalID != fVal {
					continue
				}
			}
		}
		resources = append(resources, scimUserResource(s.scimBaseURL(), u.ID, u.ExternalID, u.Email, derefStr(u.Name), u.IsActive, u.CreatedAt))
	}
	writeSCIM(c, consts.StatusOK, scimListResponse{
		Schemas: []string{scimListSchema}, TotalResults: len(resources),
		StartIndex: 1, ItemsPerPage: len(resources), Resources: resources,
	})
}

func (s *Server) handleSCIMGetUser(ctx context.Context, c *app.RequestContext) {
	u, err := s.deps.Q.ScimGetUserInOrg(ctx, db.ScimGetUserInOrgParams{ID: c.Param("id"), OrgID: scimOrgFromCtx(ctx)})
	if errors.Is(err, pgx.ErrNoRows) {
		writeSCIMError(c, consts.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to load user")
		return
	}
	writeSCIM(c, consts.StatusOK, scimUserResource(s.scimBaseURL(), u.ID, u.ExternalID, u.Email, derefStr(u.Name), u.IsActive, timeZero()))
}

func (s *Server) handleSCIMCreateUser(ctx context.Context, c *app.RequestContext) {
	orgID := scimOrgFromCtx(ctx)
	var body scimUserBody
	if err := json.Unmarshal(c.Request.Body(), &body); err != nil {
		writeSCIMError(c, consts.StatusBadRequest, "invalid request body")
		return
	}
	email := primaryEmail(body)
	if email == "" {
		writeSCIMError(c, consts.StatusBadRequest, "userName or a primary email is required")
		return
	}
	externalID := body.ExternalID
	if externalID == "" {
		externalID = email
	}

	// SCIM POST is a create; if the user already exists, report a conflict.
	if existing, err := s.deps.Q.GetUserByExternalID(ctx, db.GetUserByExternalIDParams{OrgID: orgID, ExternalID: externalID}); err == nil {
		writeSCIM(c, consts.StatusConflict, map[string]any{
			"schemas": []string{scimErrorSchema}, "status": "409",
			"scimType": "uniqueness", "detail": "user already exists: " + existing.Email,
		})
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeSCIMError(c, consts.StatusInternalServerError, "lookup failed")
		return
	}

	active := body.Active == nil || *body.Active
	name := formattedName(body.Name)
	row, err := s.deps.Q.ScimUpsertUser(ctx, db.ScimUpsertUserParams{
		OrgID: orgID, Email: email, ExternalID: externalID, Name: nilIfEmpty(name), IsActive: active,
	})
	if err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to create user")
		return
	}
	s.scimAudit(ctx, c, "scim.user.created", "user", row.ID)
	writeSCIM(c, consts.StatusCreated, scimUserResource(s.scimBaseURL(), row.ID, externalID, email, name, active, row.CreatedAt))
}

func (s *Server) handleSCIMReplaceUser(ctx context.Context, c *app.RequestContext) {
	orgID := scimOrgFromCtx(ctx)
	u, err := s.deps.Q.ScimGetUserInOrg(ctx, db.ScimGetUserInOrgParams{ID: c.Param("id"), OrgID: scimOrgFromCtx(ctx)})
	if errors.Is(err, pgx.ErrNoRows) {
		writeSCIMError(c, consts.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to load user")
		return
	}
	var body scimUserBody
	if err := json.Unmarshal(c.Request.Body(), &body); err != nil {
		writeSCIMError(c, consts.StatusBadRequest, "invalid request body")
		return
	}
	email := primaryEmail(body)
	if email == "" {
		email = u.Email
	}
	active := body.Active == nil || *body.Active
	name := formattedName(body.Name)
	if _, err := s.deps.Q.ScimUpsertUser(ctx, db.ScimUpsertUserParams{
		OrgID: orgID, Email: email, ExternalID: u.ExternalID, Name: nilIfEmpty(name), IsActive: active,
	}); err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to update user")
		return
	}
	if !active {
		s.deprovisionUser(ctx, u.ID, email)
	}
	writeSCIM(c, consts.StatusOK, scimUserResource(s.scimBaseURL(), u.ID, u.ExternalID, email, name, active, timeZero()))
}

func (s *Server) handleSCIMPatchUser(ctx context.Context, c *app.RequestContext) {
	u, err := s.deps.Q.ScimGetUserInOrg(ctx, db.ScimGetUserInOrgParams{ID: c.Param("id"), OrgID: scimOrgFromCtx(ctx)})
	if errors.Is(err, pgx.ErrNoRows) {
		writeSCIMError(c, consts.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to load user")
		return
	}
	var patch scimPatchBody
	if err := json.Unmarshal(c.Request.Body(), &patch); err != nil {
		writeSCIMError(c, consts.StatusBadRequest, "invalid patch body")
		return
	}

	active := u.IsActive
	for _, op := range patch.Operations {
		if v, ok := patchActiveValue(op); ok {
			active = v
		}
	}
	if active != u.IsActive {
		if err := s.deps.Q.SetUserActive(ctx, db.SetUserActiveParams{ID: u.ID, IsActive: active}); err != nil {
			writeSCIMError(c, consts.StatusInternalServerError, "failed to update active state")
			return
		}
		if !active {
			s.deprovisionUser(ctx, u.ID, u.Email)
		}
	}
	s.scimAudit(ctx, c, "scim.user.updated", "user", u.ID)
	writeSCIM(c, consts.StatusOK, scimUserResource(s.scimBaseURL(), u.ID, u.ExternalID, u.Email, derefStr(u.Name), active, timeZero()))
}

func (s *Server) handleSCIMDeleteUser(ctx context.Context, c *app.RequestContext) {
	u, err := s.deps.Q.ScimGetUserInOrg(ctx, db.ScimGetUserInOrgParams{ID: c.Param("id"), OrgID: scimOrgFromCtx(ctx)})
	if errors.Is(err, pgx.ErrNoRows) {
		writeSCIMError(c, consts.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to load user")
		return
	}
	// Deprovision = deactivate + revoke sessions (soft delete, reversible by re-create).
	if err := s.deps.Q.SetUserActive(ctx, db.SetUserActiveParams{ID: u.ID, IsActive: false}); err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to deactivate user")
		return
	}
	s.deprovisionUser(ctx, u.ID, u.Email)
	s.scimAudit(ctx, c, "scim.user.deactivated", "user", u.ID)
	c.SetStatusCode(consts.StatusNoContent)
}

// deprovisionUser revokes the user's sessions and regenerates their Casbin
// policies (best-effort) so deactivation takes effect immediately.
func (s *Server) deprovisionUser(ctx context.Context, userID, email string) {
	if err := s.deps.Q.RevokeUserSessions(ctx, userID); err != nil {
		logErr(ctx, err, "scim: failed to revoke sessions")
	}
	s.regenSubject(ctx, email)
}

// ── Groups ─────────────────────────────────────────────────────────────────

func (s *Server) handleSCIMListGroups(ctx context.Context, c *app.RequestContext) {
	orgID := scimOrgFromCtx(ctx)
	teams, err := s.deps.Q.ListScimTeams(ctx, orgID)
	if err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to list groups")
		return
	}
	fAttr, fVal, ok := scimFilterEq(string(c.Query("filter")))
	if !ok {
		writeSCIMError(c, consts.StatusBadRequest, `unsupported filter; only a single 'attr eq "value"' clause is supported`)
		return
	}
	resources := make([]any, 0, len(teams))
	for _, t := range teams {
		if fVal != "" && fAttr == "displayname" && !strings.EqualFold(t.Name, fVal) {
			continue
		}
		resources = append(resources, scimGroupResource(s.scimBaseURL(), t.ID, t.Name, nil))
	}
	writeSCIM(c, consts.StatusOK, scimListResponse{
		Schemas: []string{scimListSchema}, TotalResults: len(resources),
		StartIndex: 1, ItemsPerPage: len(resources), Resources: resources,
	})
}

func (s *Server) handleSCIMGetGroup(ctx context.Context, c *app.RequestContext) {
	team, err := s.deps.Q.ScimGetTeamInOrg(ctx, db.ScimGetTeamInOrgParams{ID: c.Param("id"), OrgID: scimOrgFromCtx(ctx)})
	if errors.Is(err, pgx.ErrNoRows) {
		writeSCIMError(c, consts.StatusNotFound, "group not found")
		return
	}
	if err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to load group")
		return
	}
	writeSCIM(c, consts.StatusOK, scimGroupResource(s.scimBaseURL(), team.ID, team.Name, s.groupMembers(ctx, team.ID)))
}

func (s *Server) handleSCIMCreateGroup(ctx context.Context, c *app.RequestContext) {
	orgID := scimOrgFromCtx(ctx)
	var body scimGroupBody
	if err := json.Unmarshal(c.Request.Body(), &body); err != nil || body.DisplayName == "" {
		writeSCIMError(c, consts.StatusBadRequest, "displayName is required")
		return
	}
	teamID, err := s.deps.Q.CreateScimTeam(ctx, db.CreateScimTeamParams{
		OrgID: orgID, Name: body.DisplayName, Slug: slugify(body.DisplayName),
	})
	if err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to create group")
		return
	}
	for _, m := range body.Members {
		s.addGroupMember(ctx, teamID, m.Value)
	}
	s.scimAudit(ctx, c, "scim.group.created", "team", teamID)
	writeSCIM(c, consts.StatusCreated, scimGroupResource(s.scimBaseURL(), teamID, body.DisplayName, s.groupMembers(ctx, teamID)))
}

func (s *Server) handleSCIMReplaceGroup(ctx context.Context, c *app.RequestContext) {
	team, err := s.deps.Q.ScimGetTeamInOrg(ctx, db.ScimGetTeamInOrgParams{ID: c.Param("id"), OrgID: scimOrgFromCtx(ctx)})
	if errors.Is(err, pgx.ErrNoRows) {
		writeSCIMError(c, consts.StatusNotFound, "group not found")
		return
	}
	if err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to load group")
		return
	}
	var body scimGroupBody
	if err := json.Unmarshal(c.Request.Body(), &body); err != nil {
		writeSCIMError(c, consts.StatusBadRequest, "invalid request body")
		return
	}
	if body.DisplayName != "" && body.DisplayName != team.Name {
		if err := s.deps.Q.UpdateTeamName(ctx, db.UpdateTeamNameParams{ID: team.ID, Name: body.DisplayName}); err != nil {
			writeSCIMError(c, consts.StatusInternalServerError, "failed to rename group")
			return
		}
		team.Name = body.DisplayName
	}
	// Replace the full membership set.
	s.replaceGroupMembers(ctx, team.ID, body.Members)
	writeSCIM(c, consts.StatusOK, scimGroupResource(s.scimBaseURL(), team.ID, team.Name, s.groupMembers(ctx, team.ID)))
}

func (s *Server) handleSCIMPatchGroup(ctx context.Context, c *app.RequestContext) {
	team, err := s.deps.Q.ScimGetTeamInOrg(ctx, db.ScimGetTeamInOrgParams{ID: c.Param("id"), OrgID: scimOrgFromCtx(ctx)})
	if errors.Is(err, pgx.ErrNoRows) {
		writeSCIMError(c, consts.StatusNotFound, "group not found")
		return
	}
	if err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to load group")
		return
	}
	var patch scimPatchBody
	if err := json.Unmarshal(c.Request.Body(), &patch); err != nil {
		writeSCIMError(c, consts.StatusBadRequest, "invalid patch body")
		return
	}

	for _, op := range patch.Operations {
		path := strings.ToLower(strings.TrimSpace(op.Path))
		switch {
		case path == "displayname":
			var name string
			if json.Unmarshal(op.Value, &name) == nil && name != "" {
				_ = s.deps.Q.UpdateTeamName(ctx, db.UpdateTeamNameParams{ID: team.ID, Name: name})
				team.Name = name
			}
		case strings.HasPrefix(path, "members") && strings.EqualFold(op.Op, "remove"):
			// Either a filtered path members[value eq "id"] or a value array.
			if id := memberIDFromPath(op.Path); id != "" {
				s.removeGroupMember(ctx, team.ID, id)
			} else {
				for _, m := range parseMembers(op.Value) {
					s.removeGroupMember(ctx, team.ID, m.Value)
				}
			}
		case path == "members": // add or replace
			members := parseMembers(op.Value)
			if strings.EqualFold(op.Op, "replace") {
				s.replaceGroupMembers(ctx, team.ID, members)
			} else {
				for _, m := range members {
					s.addGroupMember(ctx, team.ID, m.Value)
				}
			}
		}
	}
	s.scimAudit(ctx, c, "scim.group.updated", "team", team.ID)
	writeSCIM(c, consts.StatusOK, scimGroupResource(s.scimBaseURL(), team.ID, team.Name, s.groupMembers(ctx, team.ID)))
}

func (s *Server) handleSCIMDeleteGroup(ctx context.Context, c *app.RequestContext) {
	team, err := s.deps.Q.ScimGetTeamInOrg(ctx, db.ScimGetTeamInOrgParams{ID: c.Param("id"), OrgID: scimOrgFromCtx(ctx)})
	if errors.Is(err, pgx.ErrNoRows) {
		writeSCIMError(c, consts.StatusNotFound, "group not found")
		return
	}
	if err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to load group")
		return
	}
	members := s.groupMembers(ctx, team.ID)
	if _, err := s.deps.Q.DeleteTeam(ctx, team.ID); err != nil {
		writeSCIMError(c, consts.StatusInternalServerError, "failed to delete group")
		return
	}
	for _, m := range members {
		s.regenSubject(ctx, m.Display) // Display holds the member email
	}
	s.scimAudit(ctx, c, "scim.group.deleted", "team", team.ID)
	c.SetStatusCode(consts.StatusNoContent)
}

// ── Group membership helpers ─────────────────────────────────────────────────

func (s *Server) groupMembers(ctx context.Context, teamID string) []scimMember {
	rows, err := s.deps.Q.ListTeamMembers(ctx, teamID)
	if err != nil {
		return nil
	}
	members := make([]scimMember, 0, len(rows))
	for _, r := range rows {
		members = append(members, scimMember{Value: r.ID, Display: r.Email})
	}
	return members
}

func (s *Server) addGroupMember(ctx context.Context, teamID, userID string) {
	if userID == "" {
		return
	}
	if err := s.deps.Q.AddTeamMember(ctx, db.AddTeamMemberParams{TeamID: teamID, UserID: userID}); err != nil {
		logErr(ctx, err, "scim: add member")
		return
	}
	s.regenSubjectByUserID(ctx, userID)
}

func (s *Server) removeGroupMember(ctx context.Context, teamID, userID string) {
	if userID == "" {
		return
	}
	if _, err := s.deps.Q.RemoveTeamMember(ctx, db.RemoveTeamMemberParams{TeamID: teamID, UserID: userID}); err != nil {
		logErr(ctx, err, "scim: remove member")
		return
	}
	s.regenSubjectByUserID(ctx, userID)
}

// replaceGroupMembers sets the group's membership to exactly the given members.
func (s *Server) replaceGroupMembers(ctx context.Context, teamID string, desired []scimMember) {
	want := make(map[string]bool, len(desired))
	for _, m := range desired {
		if m.Value != "" {
			want[m.Value] = true
		}
	}
	current := s.groupMembers(ctx, teamID)
	have := make(map[string]bool, len(current))
	for _, m := range current {
		have[m.Value] = true
		if !want[m.Value] {
			s.removeGroupMember(ctx, teamID, m.Value)
		}
	}
	for id := range want {
		if !have[id] {
			s.addGroupMember(ctx, teamID, id)
		}
	}
}

func (s *Server) regenSubjectByUserID(ctx context.Context, userID string) {
	u, err := s.deps.Q.GetUserByID(ctx, userID)
	if err != nil {
		return
	}
	s.regenSubject(ctx, u.Email)
}

func (s *Server) regenSubject(ctx context.Context, email string) {
	if email == "" || s.deps.Enforcer == nil {
		return
	}
	if err := auth.RegenerateForSubject(ctx, s.deps.Q, s.deps.DB, s.deps.Enforcer, email); err != nil {
		logErr(ctx, err, "scim: policy regen")
	}
}
