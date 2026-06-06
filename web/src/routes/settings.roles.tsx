import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import {
  KeyRound,
  ChevronDown,
  ChevronRight,
  Plus,
  Pencil,
  Trash2,
  Users,
  User,
  X,
} from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { Modal } from '#/components/Modal'
import { PermissionMatrix, CI_CATALOG } from '#/components/PermissionMatrix'
import { ScopeBadges } from '#/components/ScopeBadges'
import { FormSelect } from '#/components/FormSelect'
import { SearchSelect } from '#/components/SearchSelect'
import type { Role, Permission } from '#/lib/api/types'

export const Route = createFileRoute('/settings/roles')({
  component: RolesPage,
})

type Tab = 'roles' | 'assignments'

function RolesPage() {
  const [tab, setTab] = useState<Tab>('roles')

  return (
    <div className="space-y-6">
      <div>
        <h2 className="display-title text-lg text-foreground">Roles & Access</h2>
        <p className="text-muted-foreground text-xs mt-0.5">Manage roles, permissions, and assignments</p>
      </div>

      <div className="flex items-center gap-1 border-b border-border pb-px">
        {(['roles', 'assignments'] as const).map((t) => (
          <button
            key={t}
            type="button"
            onClick={() => setTab(t)}
            className={`px-3 py-2 text-xs font-medium border-b-2 transition-colors capitalize ${
              tab === t
                ? 'border-primary text-primary'
                : 'border-transparent text-muted-foreground hover:text-foreground hover:border-border'
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      {tab === 'roles' && <RolesTab />}
      {tab === 'assignments' && <AssignmentsTab />}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Roles Tab
// ---------------------------------------------------------------------------

function RolesTab() {
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items
  const [expanded, setExpanded] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [editingRole, setEditingRole] = useState<Role | null>(null)

  const systemRoles = roles.filter((r) => r.isSystem)
  const customRoles = roles.filter((r) => !r.isSystem)

  return (
    <div className="space-y-6">
      {/* System roles */}
      <div className="space-y-2">
        <div className="flex items-center gap-2">
          <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">System Roles</h3>
          <span className="text-[11px] text-muted-foreground opacity-50">{systemRoles.length}</span>
        </div>
        <div className="space-y-2">
          {systemRoles.map((role) => (
            <RoleCard
              key={role.id}
              role={role}
              expanded={expanded === role.id}
              onToggle={() => setExpanded(expanded === role.id ? null : role.id)}
            />
          ))}
        </div>
      </div>

      {/* Custom roles */}
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Custom Roles</h3>
            <span className="text-[11px] text-muted-foreground opacity-50">{customRoles.length}</span>
          </div>
          <button
            type="button"
            onClick={() => setShowCreate(true)}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors"
            style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
          >
            <Plus size={12} />
            Create role
          </button>
        </div>
        {customRoles.length === 0 ? (
          <div className="island-shell p-8 flex flex-col items-center gap-2 text-muted-foreground">
            <KeyRound size={24} strokeWidth={1.2} />
            <span className="text-xs">No custom roles created yet.</span>
          </div>
        ) : (
          <div className="space-y-2">
            {customRoles.map((role) => (
              <RoleCard
                key={role.id}
                role={role}
                expanded={expanded === role.id}
                onToggle={() => setExpanded(expanded === role.id ? null : role.id)}
                onEdit={() => setEditingRole(role)}
                onDelete={() => client.roles.delete({ id: role.id })}
              />
            ))}
          </div>
        )}
      </div>

      {showCreate && <RoleFormModal onClose={() => setShowCreate(false)} />}
      {editingRole && <RoleFormModal role={editingRole} onClose={() => setEditingRole(null)} />}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Role Card
// ---------------------------------------------------------------------------

function RoleCard({
  role, expanded, onToggle, onEdit, onDelete,
}: {
  role: Role; expanded: boolean; onToggle: () => void; onEdit?: () => void; onDelete?: () => void
}) {
  const isWildcard = role.permissions.some((p) => p.object === '*' && p.action === '*')
  const permCount = isWildcard ? 'All' : `${role.permissions.length}`

  return (
    <div className="island-shell !p-0 overflow-hidden">
      <div
        role="button"
        onClick={onToggle}
        className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-accent/30 transition-colors cursor-pointer"
      >
        {expanded
          ? <ChevronDown size={14} className="text-muted-foreground shrink-0" />
          : <ChevronRight size={14} className="text-muted-foreground shrink-0" />}

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold text-foreground">{role.name}</span>
            {role.isSystem && (
              <span className="island-kicker !text-[11px] bg-primary/10 text-primary border-primary/20">System</span>
            )}
            <span className="text-[11px] text-muted-foreground opacity-50">{permCount} permissions</span>
          </div>
          {role.description && (
            <p className="text-xs text-muted-foreground mt-0.5 truncate">{role.description}</p>
          )}
        </div>

        <div className="shrink-0 hidden sm:block" onClick={(e) => e.stopPropagation()}>
          <ScopeBadges workspaces={role.workspaces} environments={role.environments} />
        </div>

        {!role.isSystem && (
          <div className="flex items-center gap-1 shrink-0" onClick={(e) => e.stopPropagation()}>
            {onEdit && (
              <button type="button" onClick={onEdit} className="w-7 h-7 flex items-center justify-center rounded-md text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
                <Pencil size={13} />
              </button>
            )}
            {onDelete && (
              <button type="button" onClick={onDelete} className="w-7 h-7 flex items-center justify-center rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/5 transition-colors">
                <Trash2 size={13} />
              </button>
            )}
          </div>
        )}
      </div>

      {expanded && (
        <div className="px-4 pb-4 pt-1 border-t border-border/50">
          <div className="sm:hidden mb-3">
            <ScopeBadges workspaces={role.workspaces} environments={role.environments} />
          </div>
          <PermissionMatrix permissions={role.permissions} />
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Role Form Modal (Create / Edit)
// ---------------------------------------------------------------------------

function RoleFormModal({ role, onClose }: { role?: Role; onClose: () => void }) {
  const isEdit = !!role
  const { data: wsData } = useSuspenseQuery(orpc.workspaces.list.queryOptions({ input: {} }))
  const workspaces = wsData.items
  const { data: envData } = useSuspenseQuery(orpc.environments.list.queryOptions({ input: {} }))
  const environments = envData.items

  const [name, setName] = useState(role?.name ?? '')
  const [description, setDescription] = useState(role?.description ?? '')
  const [permissions, setPermissions] = useState<Permission[]>(role?.permissions ?? [])
  const [wsScope, setWsScope] = useState<'all' | 'specific'>(role && role.workspaces.length > 0 ? 'specific' : 'all')
  const [selectedWs, setSelectedWs] = useState<Set<string>>(new Set(role?.workspaces ?? []))
  const [envScope, setEnvScope] = useState<'all' | 'specific'>(role && role.environments.length > 0 ? 'specific' : 'all')
  const [selectedEnv, setSelectedEnv] = useState<Set<string>>(new Set(role?.environments ?? []))
  const [submitted, setSubmitted] = useState(false)

  const slug = name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim() || permissions.length === 0) return

    const data = {
      name, slug,
      description: description || undefined,
      permissions,
      workspaces: wsScope === 'specific' ? [...selectedWs] : [],
      environments: envScope === 'specific' ? [...selectedEnv] : [],
    }

    if (isEdit && role) {
      client.roles.update({ id: role.id, ...data })
    } else {
      client.roles.create(data)
    }

    setSubmitted(true)
    setTimeout(onClose, 1000)
  }

  function toggleSet(set: Set<string>, setFn: (s: Set<string>) => void, key: string) {
    setFn(new Set(set.has(key) ? [...set].filter((k) => k !== key) : [...set, key]))
  }

  return (
    <Modal open onClose={onClose} title={isEdit ? 'Edit Role' : 'Create Role'} subtitle={isEdit ? role.name : 'Define permissions and scope'} wide>
      {submitted ? (
        <div className="px-5 py-8 text-center">
          <p className="text-sm text-success font-medium">Role {isEdit ? 'updated' : 'created'}</p>
        </div>
      ) : (
        <form onSubmit={handleSubmit} className="flex flex-col overflow-hidden">
          <div className="flex-1 overflow-y-auto px-5 py-4 space-y-5">
            {/* Name */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Name <span className="text-destructive">*</span></label>
              <input
                type="text" required value={name} onChange={(e) => setName(e.target.value)}
                placeholder="Prod Release Manager"
                className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
              />
              {slug && <p className="text-[12px] text-muted-foreground font-mono">slug: {slug}</p>}
            </div>

            {/* Description */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Description</label>
              <textarea
                value={description} onChange={(e) => setDescription(e.target.value)}
                placeholder="What can this role do?" rows={2}
                className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40 resize-none"
              />
            </div>

            {/* Permissions */}
            <div className="space-y-2">
              <label className="text-xs font-medium text-foreground">Permissions <span className="text-destructive">*</span></label>
              <div className="rounded-lg border border-border p-3">
                <PermissionMatrix permissions={permissions} editable onChange={setPermissions} />
              </div>
              {permissions.length === 0 && <p className="text-[12px] text-destructive">Select at least one permission</p>}
            </div>

            {/* Scope — only shown when CI permissions are selected */}
            {hasCIPermissions(permissions) && (
              <div className="space-y-4 rounded-lg border border-border p-4">
                <div>
                  <h4 className="text-xs font-semibold text-foreground">CI Scope</h4>
                  <p className="text-[12px] text-muted-foreground mt-0.5">
                    Limit CI permissions to specific workspaces and/or environments. Admin permissions are always platform-wide.
                  </p>
                </div>

                {/* Workspace scope */}
                <div className="space-y-2">
                  <label className="text-xs font-medium text-foreground">Workspaces</label>
                  <div className="flex items-center gap-4">
                    <label className="flex items-center gap-2 text-xs cursor-pointer">
                      <input type="radio" name="ws-scope" checked={wsScope === 'all'} onChange={() => setWsScope('all')} className="accent-primary" />
                      <span className="text-muted-foreground">All workspaces</span>
                    </label>
                    <label className="flex items-center gap-2 text-xs cursor-pointer">
                      <input type="radio" name="ws-scope" checked={wsScope === 'specific'} onChange={() => setWsScope('specific')} className="accent-primary" />
                      <span className="text-muted-foreground">Specific</span>
                    </label>
                  </div>
                  {wsScope === 'specific' && (
                    <div className="flex flex-wrap gap-1.5 pt-1">
                      {workspaces.map((ws) => (
                        <button key={ws.slug} type="button" onClick={() => toggleSet(selectedWs, setSelectedWs, ws.slug)}
                          className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                            selectedWs.has(ws.slug)
                              ? 'bg-primary/15 text-primary ring-1 ring-primary/20'
                              : 'text-muted-foreground border border-border hover:text-foreground hover:bg-accent'
                          }`}>
                          {ws.name}
                        </button>
                      ))}
                    </div>
                  )}
                </div>

                {/* Environment scope */}
                <div className="space-y-2">
                  <label className="text-xs font-medium text-foreground">Environments</label>
                  <div className="flex items-center gap-4">
                    <label className="flex items-center gap-2 text-xs cursor-pointer">
                      <input type="radio" name="env-scope" checked={envScope === 'all'} onChange={() => setEnvScope('all')} className="accent-primary" />
                      <span className="text-muted-foreground">All environments</span>
                    </label>
                    <label className="flex items-center gap-2 text-xs cursor-pointer">
                      <input type="radio" name="env-scope" checked={envScope === 'specific'} onChange={() => setEnvScope('specific')} className="accent-primary" />
                      <span className="text-muted-foreground">Specific</span>
                    </label>
                  </div>
                  {envScope === 'specific' && (
                    <div className="flex flex-wrap gap-1.5 pt-1">
                      {environments.map((env) => (
                        <button key={env.name} type="button" onClick={() => toggleSet(selectedEnv, setSelectedEnv, env.name)}
                          className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                            selectedEnv.has(env.name)
                              ? 'bg-emerald-500/15 text-emerald-400 ring-1 ring-emerald-500/20'
                              : 'text-muted-foreground border border-border hover:text-foreground hover:bg-accent'
                          }`}>
                          {env.name}
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              </div>
            )}
          </div>

          <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border shrink-0">
            <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
              Cancel
            </button>
            <button type="submit" disabled={!name.trim() || permissions.length === 0}
              className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
              {isEdit ? 'Save changes' : 'Create role'}
            </button>
          </div>
        </form>
      )}
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// Assignments Tab
// ---------------------------------------------------------------------------

function AssignmentsTab() {
  const { data: assignmentsData } = useSuspenseQuery(orpc.roles.assignments.list.queryOptions({ input: {} }))
  const assignments = assignmentsData.items
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items
  const [showAssign, setShowAssign] = useState(false)

  const roleMap = new Map(roles.map((r) => [r.slug, r]))

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">{assignments.length} assignments</span>
        <button
          type="button" onClick={() => setShowAssign(true)}
          className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors"
          style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
          <Plus size={12} />
          Assign role
        </button>
      </div>

      <div className="island-shell !p-0 overflow-hidden">
        <div className="hidden sm:grid sm:grid-cols-[1fr_1fr_1fr_40px] gap-3 px-4 py-2.5 border-b border-border bg-muted/30 text-[12px] font-medium text-muted-foreground uppercase tracking-wider">
          <span>Subject</span>
          <span>Role</span>
          <span>Scope</span>
          <span />
        </div>

        <div className="divide-y divide-border">
          {assignments.map((a) => {
            const role = roleMap.get(a.role)
            const isTeam = a.subject.startsWith('team:')
            return (
              <div key={`${a.subject}-${a.role}`} className="px-4 py-3 sm:grid sm:grid-cols-[1fr_1fr_1fr_40px] sm:gap-3 sm:items-center space-y-2 sm:space-y-0 hover:bg-accent/30 transition-colors">
                <div className="flex items-center gap-2 min-w-0">
                  {isTeam ? <Users size={13} className="text-muted-foreground shrink-0" /> : <User size={13} className="text-muted-foreground shrink-0" />}
                  <span className="text-sm text-foreground font-mono truncate">{a.subject}</span>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-sm text-foreground truncate">{role?.name ?? a.role}</span>
                  {role?.isSystem && <span className="island-kicker !text-[11px] bg-primary/10 text-primary border-primary/20">System</span>}
                </div>
                <div>{role && <ScopeBadges workspaces={role.workspaces} environments={role.environments} />}</div>
                <div className="flex justify-end">
                  <button type="button" onClick={() => client.roles.assignments.delete({ subject: a.subject, role: a.role })}
                    className="w-7 h-7 flex items-center justify-center rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/5 transition-colors" title="Remove">
                    <X size={13} />
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      </div>

      {showAssign && <AssignRoleModal onClose={() => setShowAssign(false)} />}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Assign Role Modal
// ---------------------------------------------------------------------------

function AssignRoleModal({ onClose, preSelectedRole }: { onClose: () => void; preSelectedRole?: string }) {
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items
  const { data: usersData } = useSuspenseQuery(orpc.users.list.queryOptions({ input: {} }))
  const allUsers = usersData.items
  const { data: teamsData } = useSuspenseQuery(orpc.teams.list.queryOptions({ input: {} }))
  const teams = teamsData.items

  const [selectedRole, setSelectedRole] = useState(preSelectedRole ?? '')
  const [selectedUsers, setSelectedUsers] = useState<Set<string>>(new Set())
  const [selectedTeams, setSelectedTeams] = useState<Set<string>>(new Set())
  const [submitted, setSubmitted] = useState(false)

  const role = roles.find((r) => r.slug === selectedRole)

  const userItems = allUsers.map((u) => ({
    id: u.email,
    label: u.name ?? u.email,
    detail: u.email,
    icon: <User size={13} className="text-muted-foreground shrink-0" />,
  }))

  const teamItems = teams.map((t) => ({
    id: `team:${t.slug}`,
    label: t.name,
    detail: `${t.memberCount} members`,
    icon: <Users size={13} className="text-muted-foreground shrink-0" />,
  }))

  const totalSelected = selectedUsers.size + selectedTeams.size

  function handleSubmit() {
    if (!selectedRole || totalSelected === 0) return
    const subjects = [...selectedUsers, ...selectedTeams]
    client.roles.assignments.create({ subjects, role: selectedRole })
    setSubmitted(true)
    setTimeout(onClose, 1000)
  }

  return (
    <Modal open onClose={onClose} title="Assign Role" subtitle="Select a role and assign to users or teams" wide>
      {submitted ? (
        <div className="px-5 py-8 text-center">
          <p className="text-sm text-success font-medium">Role assigned to {totalSelected} subject{totalSelected !== 1 ? 's' : ''}</p>
        </div>
      ) : (
        <>
          <div className="flex-1 overflow-y-auto px-5 py-4 space-y-4">
            {/* Role */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Role</label>
              <FormSelect value={selectedRole} onChange={setSelectedRole} placeholder="Select a role..." options={roles.map((r) => ({ key: r.slug, label: r.name }))} />
            </div>

            {role && (
              <div className="rounded-lg border border-border p-3 space-y-2">
                <p className="text-xs text-muted-foreground">{role.description}</p>
                <ScopeBadges workspaces={role.workspaces} environments={role.environments} />
              </div>
            )}

            {/* Users */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Users</label>
              <SearchSelect
                items={userItems}
                selected={selectedUsers}
                onChange={setSelectedUsers}
                placeholder="Search users by name or email..."
              />
            </div>

            {/* Teams */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Teams</label>
              <SearchSelect
                items={teamItems}
                selected={selectedTeams}
                onChange={setSelectedTeams}
                placeholder="Search teams..."
              />
            </div>
          </div>

          <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border shrink-0">
            <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
              Cancel
            </button>
            <button type="button" onClick={handleSubmit} disabled={!selectedRole || totalSelected === 0}
              className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
              Assign {totalSelected > 0 ? `to ${totalSelected} ` : ''}subject{totalSelected !== 1 ? 's' : ''}
            </button>
          </div>
        </>
      )}
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function hasCIPermissions(permissions: Permission[]): boolean {
  const ciObjects = new Set(Object.keys(CI_CATALOG))
  return permissions.some((p) => ciObjects.has(p.object))
}
