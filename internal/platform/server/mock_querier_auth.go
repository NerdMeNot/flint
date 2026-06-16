package server

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// This file completes the in-memory Querier so NO server route can panic in mock
// mode. The platform handlers call ~160 distinct Querier methods; the display /
// CRUD ones with meaningful canned data live in mock_querier.go, and everything
// else — auth, sessions, MFA, the OIDC device flow, role scopes, and the
// remaining mutations — is implemented here with safe behavior:
//
//   - mutations are accepted as no-ops (return nil / a synthetic id / count 1),
//   - auth/session/device/MFA lookups return errNotFound (handlers treat that as
//     "invalid/expired" and respond gracefully rather than dereferencing nil),
//   - the periodic auth-store cleanup goroutine's deletes are no-ops.
//
// Login itself is short-circuited in handlePasswordLogin (mock mode issues a
// session for the mock admin), so these auth methods are defensive rather than
// load-bearing — but implementing them means an errant request to any auth route
// returns a clean error instead of crashing the process.

// ── role lookups (real data, mirrors ListRoles) ─────────────────────────────

var mockRoleSlugByID = map[string]string{
	"role-admin": "admin", "role-developer": "developer", "role-viewer": "viewer",
	"role-sec": "security-auditor", "role-stg": "staging-operator",
}

func (q *mockQuerier) roleRow(id string) (string, string, *string, bool, bool) {
	switch id {
	case "role-admin":
		return "Admin", "admin", sp("Full access to everything"), true, true
	case "role-developer":
		return "Developer", "developer", sp("Trigger runs, manage projects"), true, true
	case "role-viewer":
		return "Viewer", "viewer", sp("Read-only access"), true, true
	case "role-sec":
		return "Security Auditor", "security-auditor", sp("Read all + manage secrets and audit"), false, true
	case "role-stg":
		return "Staging Operator", "staging-operator", sp("Deploy to staging only"), false, true
	}
	return "", "", nil, false, false
}

func (q *mockQuerier) GetRoleByID(_ context.Context, id string) (db.GetRoleByIDRow, error) {
	name, slug, desc, sys, ok := q.roleRow(id)
	if !ok {
		return db.GetRoleByIDRow{}, errNotFound
	}
	return db.GetRoleByIDRow{ID: id, Name: name, Slug: slug, Description: desc, IsSystem: sys, CreatedAt: q.data.boot.Add(-9000 * time.Hour)}, nil
}

func (q *mockQuerier) GetRoleBySlug(_ context.Context, arg db.GetRoleBySlugParams) (db.GetRoleBySlugRow, error) {
	for id, slug := range mockRoleSlugByID {
		if slug == arg.Slug {
			name, s, desc, sys, _ := q.roleRow(id)
			return db.GetRoleBySlugRow{ID: id, Name: name, Slug: s, Description: desc, IsSystem: sys, CreatedAt: q.data.boot.Add(-9000 * time.Hour)}, nil
		}
	}
	return db.GetRoleBySlugRow{}, errNotFound
}

func (q *mockQuerier) ListRoleAssignmentsByRole(_ context.Context, roleID string) ([]db.ListRoleAssignmentsByRoleRow, error) {
	slug := mockRoleSlugByID[roleID]
	out := []db.ListRoleAssignmentsByRoleRow{}
	for _, u := range mockUsers() {
		if u.role == slug {
			out = append(out, db.ListRoleAssignmentsByRoleRow{Subject: u.email, RoleID: roleID})
		}
	}
	return out, nil
}

func (q *mockQuerier) SearchUsers(_ context.Context, arg db.SearchUsersParams) ([]db.SearchUsersRow, error) {
	pat := strings.ToLower(strings.Trim(arg.Email, "%"))
	out := []db.SearchUsersRow{}
	base := q.data.boot.Add(-9000 * time.Hour)
	for i, u := range mockUsers() {
		if pat == "" || strings.Contains(strings.ToLower(u.email+u.name), pat) {
			out = append(out, db.SearchUsersRow{ID: u.id, Email: u.email, Name: sp(u.name), CreatedAt: base.Add(time.Duration(i) * 220 * time.Hour)})
		}
	}
	return out, nil
}

// ── role scope / assignment mutations (no-op) ───────────────────────────────

func (q *mockQuerier) CreateRole(context.Context, db.CreateRoleParams) (string, error) {
	return "role-new", nil
}
func (q *mockQuerier) UpdateRole(context.Context, db.UpdateRoleParams) (int64, error) { return 1, nil }
func (q *mockQuerier) DeleteRole(context.Context, string) (int64, error)              { return 1, nil }
func (q *mockQuerier) InsertRoleAssignment(context.Context, db.InsertRoleAssignmentParams) error {
	return nil
}
func (q *mockQuerier) DeleteRoleAssignment(context.Context, db.DeleteRoleAssignmentParams) error {
	return nil
}
func (q *mockQuerier) InsertRolePermission(context.Context, db.InsertRolePermissionParams) error {
	return nil
}
func (q *mockQuerier) DeleteRolePermissions(context.Context, string) error { return nil }
func (q *mockQuerier) InsertRoleWorkspaceScope(context.Context, db.InsertRoleWorkspaceScopeParams) error {
	return nil
}
func (q *mockQuerier) DeleteRoleWorkspaceScopes(context.Context, string) error { return nil }
func (q *mockQuerier) InsertRoleEnvironmentScope(context.Context, db.InsertRoleEnvironmentScopeParams) error {
	return nil
}
func (q *mockQuerier) DeleteRoleEnvironmentScopes(context.Context, string) error { return nil }

// ── teams / api keys / personal tokens / forge / webhooks / sso mutations ────

func (q *mockQuerier) CreateTeam(context.Context, db.CreateTeamParams) (string, error) {
	return "team-new", nil
}
func (q *mockQuerier) DeleteTeam(context.Context, string) (int64, error)           { return 1, nil }
func (q *mockQuerier) AddTeamMember(context.Context, db.AddTeamMemberParams) error { return nil }
func (q *mockQuerier) RemoveTeamMember(context.Context, db.RemoveTeamMemberParams) (int64, error) {
	return 1, nil
}
func (q *mockQuerier) CreateAPIKey(context.Context, db.CreateAPIKeyParams) (string, error) {
	return "key-new", nil
}
func (q *mockQuerier) DeleteAPIKey(context.Context, string) (int64, error) { return 1, nil }
func (q *mockQuerier) CreatePersonalToken(context.Context, db.CreatePersonalTokenParams) (string, error) {
	return "pat-new", nil
}
func (q *mockQuerier) DeletePersonalToken(context.Context, string) error { return nil }
func (q *mockQuerier) InsertForgeConnection(context.Context, db.InsertForgeConnectionParams) (string, error) {
	return "forge-new", nil
}
func (q *mockQuerier) UpdateForgeConnectionByName(context.Context, db.UpdateForgeConnectionByNameParams) (string, error) {
	return "forge-gh", nil
}
func (q *mockQuerier) DeleteForgeConnectionByID(context.Context, string) error { return nil }
func (q *mockQuerier) CreateWebhook(context.Context, db.CreateWebhookParams) (string, error) {
	return "wh-new", nil
}
func (q *mockQuerier) DeleteWebhook(context.Context, db.DeleteWebhookParams) error { return nil }
func (q *mockQuerier) UpsertAuthProviderConfig(context.Context, db.UpsertAuthProviderConfigParams) (string, error) {
	return "provider-new", nil
}
func (q *mockQuerier) DeleteAuthProviderConfig(context.Context, string) (int64, error) {
	return 1, nil
}

// ── env-variable extras ──────────────────────────────────────────────────────

func (q *mockQuerier) UpsertSecretEnvVariableValue(context.Context, db.UpsertSecretEnvVariableValueParams) error {
	return nil
}
func (q *mockQuerier) GetGlobalVariableValue(context.Context, string) (string, error) {
	return "", errNotFound
}
func (q *mockQuerier) GlobalVariableValueExists(context.Context, string) (bool, error) {
	return false, nil
}

// ── profile / password / MFA mutations (no-op) ──────────────────────────────

func (q *mockQuerier) UpdateUserProfile(context.Context, db.UpdateUserProfileParams) error {
	return nil
}
func (q *mockQuerier) UpdateUserPassword(context.Context, db.UpdateUserPasswordParams) error {
	return nil
}
func (q *mockQuerier) ClearForcePasswordChange(context.Context, string) error { return nil }
func (q *mockQuerier) SetUserTOTPSecret(context.Context, db.SetUserTOTPSecretParams) error {
	return nil
}
func (q *mockQuerier) VerifyUserTOTP(context.Context, string) error { return nil }
func (q *mockQuerier) ClearUserTOTP(context.Context, string) error  { return nil }
func (q *mockQuerier) RecordTOTPUse(context.Context, db.RecordTOTPUseParams) (string, error) {
	return "", nil
}
func (q *mockQuerier) CheckMFARequiredForUser(context.Context, string) (bool, error) {
	return false, nil
}
func (q *mockQuerier) GetUserRecoveryCodes(context.Context, string) ([]string, error) {
	return nil, nil
}
func (q *mockQuerier) SetUserRecoveryCodes(context.Context, db.SetUserRecoveryCodesParams) error {
	return nil
}

// ── sessions / login attempts ───────────────────────────────────────────────

func (q *mockQuerier) CreateSession(context.Context, db.CreateSessionParams) (string, error) {
	return "mock-session", nil
}
func (q *mockQuerier) GetSessionByTokenHash(context.Context, string) (db.GetSessionByTokenHashRow, error) {
	return db.GetSessionByTokenHashRow{}, errNotFound
}
func (q *mockQuerier) RotateSessionToken(context.Context, db.RotateSessionTokenParams) error {
	return nil
}
func (q *mockQuerier) RevokeSession(context.Context, string) error       { return nil }
func (q *mockQuerier) RevokeSessionByHash(context.Context, string) error { return nil }
func (q *mockQuerier) RevokeUserSessions(context.Context, string) error  { return nil }
func (q *mockQuerier) UpdateSessionIdpToken(context.Context, db.UpdateSessionIdpTokenParams) error {
	return nil
}
func (q *mockQuerier) RecordLoginAttempt(context.Context, db.RecordLoginAttemptParams) error {
	return nil
}
func (q *mockQuerier) CountRecentFailuresByEmail(context.Context, string) (int64, error) {
	return 0, nil
}
func (q *mockQuerier) GetUserForAuth(context.Context, db.GetUserForAuthParams) (db.GetUserForAuthRow, error) {
	return db.GetUserForAuthRow{}, errNotFound // login is short-circuited in mock mode
}

// ── token validation (middleware bypasses auth in mock; empty + no-op) ───────

func (q *mockQuerier) ListValidAPIKeys(context.Context) ([]db.ListValidAPIKeysRow, error) {
	return []db.ListValidAPIKeysRow{}, nil
}
func (q *mockQuerier) ListValidPersonalTokensWithUser(context.Context) ([]db.ListValidPersonalTokensWithUserRow, error) {
	return []db.ListValidPersonalTokensWithUserRow{}, nil
}
func (q *mockQuerier) TouchAPIKey(context.Context, string) error        { return nil }
func (q *mockQuerier) TouchPersonalToken(context.Context, string) error { return nil }

// ── OIDC device flow + MFA pending (not used by the browser; safe defaults) ──

func (q *mockQuerier) InsertDeviceCode(context.Context, db.InsertDeviceCodeParams) error { return nil }
func (q *mockQuerier) DeleteDeviceCode(context.Context, string) error                    { return nil }
func (q *mockQuerier) DeleteExpiredDeviceCodes(context.Context) error                    { return nil }
func (q *mockQuerier) TouchDeviceCodePoll(context.Context, string) error                 { return nil }
func (q *mockQuerier) CompleteDeviceCode(context.Context, db.CompleteDeviceCodeParams) error {
	return nil
}
func (q *mockQuerier) SetDeviceCodeOAuthState(context.Context, db.SetDeviceCodeOAuthStateParams) error {
	return nil
}
func (q *mockQuerier) GetDeviceCode(context.Context, string) (db.GetDeviceCodeRow, error) {
	return db.GetDeviceCodeRow{}, errNotFound
}
func (q *mockQuerier) ClaimCompletedDeviceCode(context.Context, string) (db.ClaimCompletedDeviceCodeRow, error) {
	return db.ClaimCompletedDeviceCodeRow{}, errNotFound
}
func (q *mockQuerier) FindDeviceCodeByUserCode(context.Context, string) (string, error) {
	return "", errNotFound
}
func (q *mockQuerier) FindDeviceCodeByOAuthState(context.Context, *string) (string, error) {
	return "", errNotFound
}
func (q *mockQuerier) GetDeviceCodeNonce(context.Context, string) (*string, error) {
	return nil, errNotFound
}
func (q *mockQuerier) GetDeviceCodeRefreshToken(context.Context, string) (*string, error) {
	return nil, errNotFound
}
func (q *mockQuerier) InsertMFAPendingToken(context.Context, db.InsertMFAPendingTokenParams) error {
	return nil
}
func (q *mockQuerier) GetMFAPendingToken(context.Context, string) (db.GetMFAPendingTokenRow, error) {
	return db.GetMFAPendingTokenRow{}, errNotFound
}
func (q *mockQuerier) DeleteMFAPendingToken(context.Context, string) error { return nil }
func (q *mockQuerier) DeleteExpiredMFAPendingTokens(context.Context) error { return nil }

// ── agent-facing (no jobs run in mock; safe defaults) ────────────────────────

func (q *mockQuerier) GetCloneCredentials(context.Context, string) (db.GetCloneCredentialsRow, error) {
	return db.GetCloneCredentialsRow{}, errNotFound
}
func (q *mockQuerier) GetWorkflowInput(context.Context, string) ([]byte, error) {
	return []byte("{}"), nil
}

// ── Workflows product (runs + schedules) ─────────────────────────────────────

func (q *mockQuerier) ListWorkflowRuns(_ context.Context, arg db.ListWorkflowRunsParams) ([]db.ListWorkflowRunsRow, error) {
	// newest first; keyset cursor resumes after the row whose id matches.
	wr := make([]*mockRun, 0, len(q.data.workflowRuns))
	for i := range q.data.workflowRuns {
		wr = append(wr, &q.data.workflowRuns[i])
	}
	sort.Slice(wr, func(i, j int) bool {
		if wr[i].startedAgo != wr[j].startedAgo {
			return wr[i].startedAgo < wr[j].startedAgo
		}
		return wr[i].id > wr[j].id
	})
	start := 0
	if arg.CursorID != "" {
		for k, r := range wr {
			if r.id == arg.CursorID {
				start = k + 1
				break
			}
		}
	}
	out := []db.ListWorkflowRunsRow{}
	for k := start; k < len(wr); k++ {
		r := wr[k]
		steps := q.data.steps(r)
		status := statusFromSteps(steps)
		started := time.Now().Add(-r.startedAgo)
		row := db.ListWorkflowRunsRow{
			ID: r.id, Status: status, TriggerType: r.trigger, TriggeredBy: sp(r.by), StartedAt: started,
		}
		if status == "succeeded" || status == "failed" || status == "cancelled" {
			var last time.Time
			for _, s := range steps {
				if s.FinishedAt != nil && s.FinishedAt.After(last) {
					last = *s.FinishedAt
				}
			}
			if !last.IsZero() {
				row.FinishedAt = &last
				row.DurationMs = i4(int32(last.Sub(started)/time.Millisecond), true)
			}
		}
		if status == "failed" {
			row.ErrorMessage = sp("transform failed: command exited with code 1")
		}
		out = append(out, row)
		if arg.Lim > 0 && int32(len(out)) >= arg.Lim {
			break
		}
	}
	return out, nil
}

func (q *mockQuerier) ListWorkflowSchedules(_ context.Context, _ string) ([]db.ListWorkflowSchedulesRow, error) {
	now := q.data.boot
	out := make([]db.ListWorkflowSchedulesRow, 0, len(q.data.wfSchedules))
	for _, s := range q.data.wfSchedules {
		row := db.ListWorkflowSchedulesRow{
			ID: s.id, Name: s.name, Cron: s.cron, Enabled: s.enabled,
			NextRunAt: now.Add(time.Duration(s.nextInMin) * time.Minute),
			CreatedAt: now.Add(-2000 * time.Hour),
		}
		if s.lastAgoMin > 0 {
			last := now.Add(-time.Duration(s.lastAgoMin) * time.Minute)
			row.LastRunAt = &last
		}
		out = append(out, row)
	}
	return out, nil
}

func (q *mockQuerier) CreateWorkflowSchedule(context.Context, db.CreateWorkflowScheduleParams) (string, error) {
	return "wfs-new", nil
}
func (q *mockQuerier) InsertWorkflowRun(context.Context, db.InsertWorkflowRunParams) error {
	return nil
}

// Worker-only scheduler queries — no worker runs in mock mode.
func (q *mockQuerier) ListDueWorkflowSchedules(context.Context) ([]db.ListDueWorkflowSchedulesRow, error) {
	return []db.ListDueWorkflowSchedulesRow{}, nil
}
func (q *mockQuerier) AdvanceWorkflowScheduleIfDue(context.Context, db.AdvanceWorkflowScheduleIfDueParams) (int64, error) {
	return 0, nil
}
