package auth

// permissions maps each role to the actions it can perform.
// This is the single source of truth for Flint's authorization model.
var permissions = map[Role]map[Action]bool{
	RoleOrgAdmin: {
		ActionOrgManage:      true,
		ActionOrgRead:        true,
		ActionProjectCreate:  true,
		ActionProjectUpdate:  true,
		ActionProjectArchive: true,
		ActionProjectRead:    true,
		ActionPipelineRun:    true,
		ActionPipelineCancel: true,
		ActionPipelineRead:   true,
		ActionGateApprove:    true,
		ActionSecretCreate:   true,
		ActionSecretRead:     true,
		ActionSecretDelete:   true,
		ActionRunnerManage:   true,
		ActionRunnerRead:     true,
		ActionRBACManage:     true,
		ActionAuditRead:      true,
	},
	RolePipelineAdmin: {
		ActionOrgRead:        true,
		ActionProjectCreate:  true,
		ActionProjectUpdate:  true,
		ActionProjectArchive: true,
		ActionProjectRead:    true,
		ActionPipelineRun:    true,
		ActionPipelineCancel: true,
		ActionPipelineRead:   true,
		ActionGateApprove:    true,
		ActionSecretCreate:   true,
		ActionSecretRead:     true,
		ActionSecretDelete:   true,
		ActionRunnerRead:     true,
	},
	RoleDeveloper: {
		ActionOrgRead:        true,
		ActionProjectRead:    true,
		ActionPipelineRun:    true,
		ActionPipelineCancel: true,
		ActionPipelineRead:   true,
		ActionGateApprove:    true,
		ActionSecretRead:     true,
		ActionRunnerRead:     true,
	},
	RoleViewer: {
		ActionOrgRead:      true,
		ActionProjectRead:  true,
		ActionPipelineRead: true,
		ActionRunnerRead:   true,
	},
}

// Can returns true if the given role is allowed to perform the action.
func Can(role Role, action Action) bool {
	if perms, ok := permissions[role]; ok {
		return perms[action]
	}
	return false
}

// ValidRole returns true if the role is one of the four built-in roles.
func ValidRole(role Role) bool {
	_, ok := permissions[role]
	return ok
}

// AllRoles returns all built-in roles in descending privilege order.
func AllRoles() []Role {
	return []Role{RoleOrgAdmin, RolePipelineAdmin, RoleDeveloper, RoleViewer}
}

// ActionsForRole returns all actions permitted for a role.
func ActionsForRole(role Role) []Action {
	perms, ok := permissions[role]
	if !ok {
		return nil
	}
	actions := make([]Action, 0, len(perms))
	for action := range perms {
		actions = append(actions, action)
	}
	return actions
}
