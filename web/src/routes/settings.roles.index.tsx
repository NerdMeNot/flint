import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { KeyRound, Plus, Users, User, ChevronRight } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { ConfirmButton } from '#/components/ConfirmButton'
import { ScopeBadges } from '#/components/ScopeBadges'
import { Badge } from '#/components/Badge'
import { FormSelect } from '#/components/FormSelect'
import { SearchSelect } from '#/components/SearchSelect'
import { Modal } from '#/components/Modal'
import type { Role } from '#/lib/api/types'

export const Route = createFileRoute('/settings/roles/')({
  component: RolesPage,
})

type Tab = 'roles' | 'assignments'

function RolesPage() {
  const [tab, setTab] = useState<Tab>('roles')

  return (
    <div className="space-y-6">
      <div>
        <h2 className="display-title text-lg text-foreground">Roles &amp; Access</h2>
        <p className="text-muted-foreground text-xs mt-0.5">Manage roles, permissions, and assignments</p>
      </div>

      <div className="flex items-center gap-1 border-b border-border pb-px">
        {(['roles', 'assignments'] as const).map((t) => (
          <button
            key={t}
            type="button"
            onClick={() => setTab(t)}
            className={`px-3 py-2 text-xs font-medium border-b-2 transition-colors capitalize ${
              tab === t ? 'border-primary text-primary' : 'border-transparent text-muted-foreground hover:text-foreground hover:border-border'
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      {tab === 'roles' ? <RolesTab /> : <AssignmentsTab />}
    </div>
  )
}

function RolesTab() {
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items
  const del = useAction((id: string) => client.roles.delete({ id }), { invalidate: [orpc.roles.list.key()] })

  const systemRoles = roles.filter((r) => r.isSystem)
  const customRoles = roles.filter((r) => !r.isSystem)

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <div className="flex items-center gap-2">
          <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">System Roles</h3>
          <span className="text-[11px] text-muted-foreground opacity-50">{systemRoles.length}</span>
        </div>
        <div className="island-shell !p-0 overflow-hidden divide-y divide-border">
          {systemRoles.map((role) => <RoleRow key={role.id} role={role} />)}
        </div>
      </div>

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Custom Roles</h3>
            <span className="text-[11px] text-muted-foreground opacity-50">{customRoles.length}</span>
          </div>
          <Link
            to="/settings/roles/new"
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors"
            style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
          >
            <Plus size={12} /> Create role
          </Link>
        </div>
        {customRoles.length === 0 ? (
          <div className="island-shell p-8 flex flex-col items-center gap-2 text-muted-foreground">
            <KeyRound size={24} strokeWidth={1.2} />
            <span className="text-xs">No custom roles created yet.</span>
          </div>
        ) : (
          <div className="island-shell !p-0 overflow-hidden divide-y divide-border">
            {customRoles.map((role) => (
              <RoleRow key={role.id} role={role} onDelete={() => del.mutate(role.id)} />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function RoleRow({ role, onDelete }: { role: Role; onDelete?: () => void }) {
  const isWildcard = role.permissions.some((p) => p.object === '*' && p.action === '*')
  const permCount = isWildcard ? 'All' : `${role.permissions.length}`

  return (
    <div className="flex items-center gap-3 hover:bg-accent transition-colors">
      <Link
        to="/settings/roles/$id"
        params={{ id: role.id }}
        className="flex items-center gap-3 px-4 py-3 flex-1 min-w-0 group"
      >
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold text-foreground group-hover:text-primary transition-colors">{role.name}</span>
            {role.isSystem && <Badge variant="primary">System</Badge>}
            <span className="text-[11px] text-muted-foreground opacity-50">{permCount} permissions</span>
          </div>
          {role.description && <p className="text-xs text-muted-foreground mt-0.5 truncate">{role.description}</p>}
        </div>
        <div className="shrink-0 hidden sm:block">
          <ScopeBadges workspaces={role.workspaces} environments={role.environments} />
        </div>
        <ChevronRight size={15} className="text-muted-foreground/40 group-hover:text-foreground transition-colors shrink-0" />
      </Link>
      {onDelete && (
        <div className="pr-3 shrink-0">
          <ConfirmButton onConfirm={onDelete} title="Delete role" />
        </div>
      )}
    </div>
  )
}

// ── Assignments (global cross-role view) ──
function AssignmentsTab() {
  const { data: assignmentsData } = useSuspenseQuery(orpc.roles.assignments.list.queryOptions({ input: {} }))
  const assignments = assignmentsData.items
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items
  const [showAssign, setShowAssign] = useState(false)
  const removeAssignment = useAction(
    (a: { subject: string; role: string }) => client.roles.assignments.delete(a),
    { invalidate: [orpc.roles.assignments.list.key()] },
  )

  const roleMap = new Map(roles.map((r) => [r.slug, r]))

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">{assignments.length} assignments</span>
        <button
          type="button" onClick={() => setShowAssign(true)}
          className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors"
          style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
          <Plus size={12} /> Assign role
        </button>
      </div>

      <div className="island-shell !p-0 overflow-hidden">
        <div className="hidden sm:grid sm:grid-cols-[1fr_1fr_1fr_40px] gap-3 px-4 py-2.5 border-b border-border bg-muted text-[12px] font-medium text-muted-foreground uppercase tracking-wider">
          <span>Subject</span><span>Role</span><span>Scope</span><span />
        </div>
        <div className="divide-y divide-border">
          {assignments.map((a) => {
            const role = roleMap.get(a.role)
            const isTeam = a.subject.startsWith('team:')
            return (
              <div key={`${a.subject}-${a.role}`} className="px-4 py-3 sm:grid sm:grid-cols-[1fr_1fr_1fr_40px] sm:gap-3 sm:items-center space-y-2 sm:space-y-0 hover:bg-accent transition-colors">
                <div className="flex items-center gap-2 min-w-0">
                  {isTeam ? <Users size={13} className="text-muted-foreground shrink-0" /> : <User size={13} className="text-muted-foreground shrink-0" />}
                  <span className="text-sm text-foreground font-mono truncate">{a.subject}</span>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-sm text-foreground truncate min-w-0">{role?.name ?? a.role}</span>
                  {role?.isSystem && <Badge variant="primary">System</Badge>}
                </div>
                <div>{role && <ScopeBadges workspaces={role.workspaces} environments={role.environments} />}</div>
                <div className="flex justify-end">
                  <ConfirmButton onConfirm={() => removeAssignment.mutate({ subject: a.subject, role: a.role })} title="Remove assignment" />
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

function AssignRoleModal({ onClose }: { onClose: () => void }) {
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items
  const { data: usersData } = useSuspenseQuery(orpc.users.list.queryOptions({ input: {} }))
  const { data: teamsData } = useSuspenseQuery(orpc.teams.list.queryOptions({ input: {} }))

  const [selectedRole, setSelectedRole] = useState('')
  const [selectedUsers, setSelectedUsers] = useState<Set<string>>(new Set())
  const [selectedTeams, setSelectedTeams] = useState<Set<string>>(new Set())

  const role = roles.find((r) => r.slug === selectedRole)
  const userItems = usersData.items.map((u) => ({ id: u.email, label: u.name ?? u.email, detail: u.email, icon: <User size={13} className="text-muted-foreground shrink-0" /> }))
  const teamItems = teamsData.items.map((t) => ({ id: `team:${t.slug}`, label: t.name, detail: `${t.memberCount} members`, icon: <Users size={13} className="text-muted-foreground shrink-0" /> }))
  const total = selectedUsers.size + selectedTeams.size

  const assign = useAction(
    (subjects: string[]) => client.roles.assignments.create({ subjects, role: selectedRole }),
    { invalidate: [orpc.roles.assignments.list.key()], onSuccess: onClose },
  )

  return (
    <Modal open onClose={onClose} title="Assign Role" subtitle="Select a role and assign to users or teams" wide>
      <div className="flex-1 overflow-y-auto px-5 py-4 space-y-4">
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
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-foreground">Users</label>
          <SearchSelect items={userItems} selected={selectedUsers} onChange={setSelectedUsers} placeholder="Search users by name or email..." />
        </div>
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-foreground">Teams</label>
          <SearchSelect items={teamItems} selected={selectedTeams} onChange={setSelectedTeams} placeholder="Search teams..." />
        </div>
      </div>
      <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border shrink-0">
        <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
          Cancel
        </button>
        <button type="button" onClick={() => assign.mutate([...selectedUsers, ...selectedTeams])} disabled={!selectedRole || total === 0 || assign.isPending}
          className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
          style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
          Assign {total > 0 ? `to ${total} ` : ''}subject{total !== 1 ? 's' : ''}
        </button>
      </div>
    </Modal>
  )
}
