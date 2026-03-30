package auth_test

import (
	"testing"

	"github.com/NerdMeNot/flint/internal/auth"
)

func TestCan_OrgAdmin(t *testing.T) {
	// Org admin can do everything.
	actions := []auth.Action{
		auth.ActionOrgManage, auth.ActionOrgRead,
		auth.ActionProjectCreate, auth.ActionProjectUpdate, auth.ActionProjectArchive, auth.ActionProjectRead,
		auth.ActionPipelineRun, auth.ActionPipelineCancel, auth.ActionPipelineRead,
		auth.ActionGateApprove,
		auth.ActionSecretCreate, auth.ActionSecretRead, auth.ActionSecretDelete,
		auth.ActionRunnerManage, auth.ActionRunnerRead,
		auth.ActionRBACManage, auth.ActionAuditRead,
	}

	for _, action := range actions {
		if !auth.Can(auth.RoleOrgAdmin, action) {
			t.Errorf("OrgAdmin should be able to %s", action)
		}
	}
}

func TestCan_Viewer(t *testing.T) {
	// Viewer can only read.
	allowed := map[auth.Action]bool{
		auth.ActionOrgRead:      true,
		auth.ActionProjectRead:  true,
		auth.ActionPipelineRead: true,
		auth.ActionRunnerRead:   true,
	}

	denied := []auth.Action{
		auth.ActionOrgManage,
		auth.ActionProjectCreate, auth.ActionProjectUpdate, auth.ActionProjectArchive,
		auth.ActionPipelineRun, auth.ActionPipelineCancel,
		auth.ActionGateApprove,
		auth.ActionSecretCreate, auth.ActionSecretRead, auth.ActionSecretDelete,
		auth.ActionRunnerManage,
		auth.ActionRBACManage, auth.ActionAuditRead,
	}

	for action := range allowed {
		if !auth.Can(auth.RoleViewer, action) {
			t.Errorf("Viewer should be able to %s", action)
		}
	}

	for _, action := range denied {
		if auth.Can(auth.RoleViewer, action) {
			t.Errorf("Viewer should NOT be able to %s", action)
		}
	}
}

func TestCan_Developer(t *testing.T) {
	// Developer can run pipelines and approve gates but not manage infra.
	if !auth.Can(auth.RoleDeveloper, auth.ActionPipelineRun) {
		t.Error("Developer should be able to run pipelines")
	}
	if !auth.Can(auth.RoleDeveloper, auth.ActionGateApprove) {
		t.Error("Developer should be able to approve gates")
	}
	if auth.Can(auth.RoleDeveloper, auth.ActionRunnerManage) {
		t.Error("Developer should NOT manage runners")
	}
	if auth.Can(auth.RoleDeveloper, auth.ActionSecretCreate) {
		t.Error("Developer should NOT create secrets")
	}
}

func TestCan_PipelineAdmin(t *testing.T) {
	// Pipeline admin can manage projects and secrets but not org/RBAC.
	if !auth.Can(auth.RolePipelineAdmin, auth.ActionProjectCreate) {
		t.Error("PipelineAdmin should create projects")
	}
	if !auth.Can(auth.RolePipelineAdmin, auth.ActionSecretCreate) {
		t.Error("PipelineAdmin should create secrets")
	}
	if auth.Can(auth.RolePipelineAdmin, auth.ActionOrgManage) {
		t.Error("PipelineAdmin should NOT manage org")
	}
	if auth.Can(auth.RolePipelineAdmin, auth.ActionRBACManage) {
		t.Error("PipelineAdmin should NOT manage RBAC")
	}
}

func TestCan_InvalidRole(t *testing.T) {
	if auth.Can(auth.Role("superadmin"), auth.ActionOrgManage) {
		t.Error("invalid role should not have any permissions")
	}
}

func TestValidRole(t *testing.T) {
	for _, role := range auth.AllRoles() {
		if !auth.ValidRole(role) {
			t.Errorf("ValidRole(%q) = false, want true", role)
		}
	}
	if auth.ValidRole("invalid") {
		t.Error("ValidRole(invalid) = true, want false")
	}
}

func TestAllRoles(t *testing.T) {
	roles := auth.AllRoles()
	if len(roles) != 4 {
		t.Fatalf("len(AllRoles()) = %d, want 4", len(roles))
	}
	// First should be most privileged.
	if roles[0] != auth.RoleOrgAdmin {
		t.Errorf("AllRoles()[0] = %q, want org_admin", roles[0])
	}
	if roles[3] != auth.RoleViewer {
		t.Errorf("AllRoles()[3] = %q, want viewer", roles[3])
	}
}

func TestActionsForRole(t *testing.T) {
	actions := auth.ActionsForRole(auth.RoleViewer)
	if len(actions) != 4 {
		t.Errorf("len(ActionsForRole(viewer)) = %d, want 4", len(actions))
	}

	actions = auth.ActionsForRole(auth.Role("invalid"))
	if actions != nil {
		t.Errorf("ActionsForRole(invalid) = %v, want nil", actions)
	}
}
