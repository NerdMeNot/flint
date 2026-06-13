import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Users, ArrowLeft, Mail, Link2, UserPlus, Check, Search, KeyRound, Plus } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { Modal } from '#/components/Modal'
import { FormSelect } from '#/components/FormSelect'
import { ScopeBadges } from '#/components/ScopeBadges'
import { Badge } from '#/components/Badge'
import { MemberAvatar } from '#/components/MemberAvatar'
import { ConfirmButton } from '#/components/ConfirmButton'

export const Route = createFileRoute('/settings/teams/$id')({
  component: TeamDetailPage,
})

function TeamDetailPage() {
  const { id } = Route.useParams()
  const [showAddMember, setShowAddMember] = useState(false)
  const [showAssignRole, setShowAssignRole] = useState(false)

  const { data: assignmentsData } = useSuspenseQuery(orpc.roles.assignments.list.queryOptions({ input: {} }))
  const assignments = assignmentsData.items
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items

  const { data: team } = useSuspenseQuery(
    orpc.teams.get.queryOptions({ input: { id } }),
  )

  const remove = useAction(client.teams.removeMember, {
    invalidate: [orpc.teams.get.key()],
  })

  return (
    <div className="space-y-5">
      <Link
        to="/settings/teams"
        className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
      >
        <ArrowLeft size={13} />
        All teams
      </Link>

      {/* Header */}
      <div className="island-shell p-4 sm:p-5">
        <div className="flex items-start justify-between gap-4">
          <div>
            <div className="flex items-center gap-2.5">
              <h2 className="display-title text-lg font-bold text-foreground">{team.name}</h2>
              <Badge variant={team.source === 'idp' ? 'primary' : 'neutral'}>
                {team.source === 'idp' ? 'IdP Synced' : 'Internal'}
              </Badge>
            </div>
            <p className="text-sm text-muted-foreground font-mono mt-0.5">{team.slug}</p>
            {team.idpGroup && (
              <p className="text-xs text-muted-foreground mt-1 flex items-center gap-1.5">
                <Link2 size={11} />
                IdP group: <span className="font-mono">{team.idpGroup}</span>
              </p>
            )}
          </div>
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground shrink-0">
            <Users size={13} />
            {team.members.length} {team.members.length === 1 ? 'member' : 'members'}
          </div>
        </div>

        {team.source === 'idp' && (
          <p className="text-xs text-muted-foreground mt-3 pt-3 border-t border-border">
            Membership is managed by your identity provider. Changes sync automatically on next login.
          </p>
        )}
      </div>

      {/* Team roles */}
      {(() => {
        const roleMap = new Map(roles.map((r) => [r.slug, r]))
        const teamSlug = team.slug
        const teamAssignedRoles = assignments
          .filter((a) => a.subject === `team:${teamSlug}`)
          .map((a) => roleMap.get(a.role))
          .filter(Boolean) as typeof roles

        return (
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <KeyRound size={14} className="text-muted-foreground" />
                <h3 className="text-xs font-semibold text-foreground">Team Roles</h3>
                <span className="text-[11px] text-muted-foreground opacity-50">Inherited by all members</span>
              </div>
              <button
                type="button"
                onClick={() => setShowAssignRole(true)}
                className="flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium text-primary hover:bg-primary/5 transition-colors"
              >
                <Plus size={12} />
                Assign role
              </button>
            </div>
            {teamAssignedRoles.length > 0 ? (
              <div className="space-y-1.5">
                {teamAssignedRoles.map((role) => (
                  <div key={role.id} className="island-shell p-3 flex items-center justify-between gap-3">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-foreground">{role.name}</span>
                      {role.isSystem && <Badge variant="primary">System</Badge>}
                    </div>
                    <ScopeBadges workspaces={role.workspaces} environments={role.environments} />
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-xs text-muted-foreground opacity-50 pl-5">No roles assigned to this team.</p>
            )}
          </div>
        )
      })()}

      {/* Members */}
      {team.members.length === 0 ? (
        <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
          <Users size={32} strokeWidth={1.2} />
          <span className="text-sm">No members in this team yet.</span>
        </div>
      ) : (
        <div className="island-shell !p-0 overflow-hidden">
          <div className="flex items-center justify-between px-5 py-3 border-b border-border">
            <h3 className="text-xs font-semibold text-foreground">Members</h3>
            {team.source === 'internal' && (
              <button
                type="button"
                onClick={() => setShowAddMember(true)}
                className="flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium text-primary hover:bg-primary/5 transition-colors"
              >
                <UserPlus size={12} />
                Add member
              </button>
            )}
          </div>
          <div className="divide-y divide-border">
            {team.members.map((member) => (
              <div
                key={member.id}
                className="flex items-center gap-4 px-5 py-3 hover:bg-accent/30 transition-colors"
              >
                <MemberAvatar name={member.name ?? member.email} />
                <div className="flex-1 min-w-0">
                  {member.name && (
                    <p className="text-sm font-medium text-foreground truncate">{member.name}</p>
                  )}
                  <p className="text-xs text-muted-foreground truncate flex items-center gap-1.5">
                    <Mail size={11} className="shrink-0" />
                    {member.email}
                  </p>
                </div>
                {team.source === 'internal' && (
                  <ConfirmButton
                    onConfirm={() => remove.mutate({ teamId: id, userId: member.id })}
                    title="Remove member"
                  />
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {showAddMember && (
        <AddMemberOverlay
          teamId={id}
          teamName={team.name}
          existingMemberIds={new Set(team.members.map((m) => m.id))}
          onClose={() => setShowAddMember(false)}
        />
      )}

      {showAssignRole && (
        <AssignRoleToTeamModal
          teamSlug={team.slug}
          teamName={team.name}
          onClose={() => setShowAssignRole(false)}
        />
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Assign role to team modal
// ---------------------------------------------------------------------------

function AssignRoleToTeamModal({
  teamSlug,
  teamName,
  onClose,
}: {
  teamSlug: string
  teamName: string
  onClose: () => void
}) {
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items
  const [selectedRole, setSelectedRole] = useState('')
  const [submitted, setSubmitted] = useState(false)

  const role = roles.find((r) => r.slug === selectedRole)

  const assign = useAction(
    () => client.roles.assignments.create({ subjects: [`team:${teamSlug}`], role: selectedRole }),
    {
      invalidate: [orpc.roles.assignments.list.key()],
      onSuccess: () => {
        setSubmitted(true)
        setTimeout(onClose, 1000)
      },
    },
  )

  function handleSubmit() {
    if (!selectedRole) return
    assign.mutate(undefined)
  }

  return (
    <Modal open onClose={onClose} title="Assign Role" subtitle={`Assign a role to ${teamName}`}>
      {submitted ? (
        <div className="px-5 py-8 text-center">
          <p className="text-sm text-success font-medium">Role assigned</p>
        </div>
      ) : (
        <>
          <div className="px-5 py-4 space-y-4">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Role</label>
              <FormSelect
                value={selectedRole}
                onChange={setSelectedRole}
                placeholder="Select a role..."
                options={roles.map((r) => ({ key: r.slug, label: r.name }))}
              />
            </div>
            {role && (
              <div className="rounded-lg border border-border p-3 space-y-2">
                <p className="text-xs text-muted-foreground">{role.description}</p>
                <ScopeBadges workspaces={role.workspaces} environments={role.environments} />
              </div>
            )}
          </div>
          <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border">
            <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
              Cancel
            </button>
            <button type="button" onClick={handleSubmit} disabled={!selectedRole}
              className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
              Assign role
            </button>
          </div>
        </>
      )}
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// Add member overlay
// ---------------------------------------------------------------------------

function AddMemberOverlay({
  teamId,
  teamName,
  existingMemberIds,
  onClose,
}: {
  teamId: string
  teamName: string
  existingMemberIds: Set<string>
  onClose: () => void
}) {
  const { data: usersData } = useSuspenseQuery(orpc.users.list.queryOptions({ input: {} }))
  const allUsers = usersData.items
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [search, setSearch] = useState('')
  const [submitted, setSubmitted] = useState(false)

  const add = useAction(client.teams.addMembers, {
    invalidate: [orpc.teams.get.key()],
    onSuccess: () => {
      setSubmitted(true)
      setTimeout(onClose, 1200)
    },
  })

  const available = allUsers.filter((u) => !existingMemberIds.has(u.id))
  const filtered = search
    ? available.filter((u) =>
        (u.name?.toLowerCase().includes(search.toLowerCase())) ||
        u.email.toLowerCase().includes(search.toLowerCase()),
      )
    : available

  function toggle(id: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  function handleSubmit() {
    if (selected.size === 0) return
    add.mutate({ teamId, userIds: [...selected] })
  }

  return (
    <Modal open onClose={onClose} title="Add members" subtitle={`Select users to add to ${teamName}`}>
      {submitted ? (
        <div className="px-5 py-8 text-center">
          <p className="text-sm text-success font-medium">
            {selected.size} {selected.size === 1 ? 'member' : 'members'} added
          </p>
        </div>
      ) : (
        <>
          <div className="px-5 pt-4 pb-2 shrink-0">
            <div className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 focus-within:ring-2 focus-within:ring-ring/40">
              <Search size={14} className="text-muted-foreground shrink-0" />
              <input
                type="text"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search by name or email..."
                className="flex-1 bg-transparent text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none"
              />
            </div>
            {selected.size > 0 && (
              <p className="text-xs text-primary mt-2">{selected.size} selected</p>
            )}
          </div>

          <div className="flex-1 overflow-y-auto px-2 pb-2">
            {filtered.length === 0 ? (
              <p className="text-xs text-muted-foreground text-center py-6">
                {search ? 'No users match your search.' : 'All users are already members.'}
              </p>
            ) : (
              filtered.map((user) => {
                const isSelected = selected.has(user.id)
                return (
                  <button
                    key={user.id}
                    type="button"
                    onClick={() => toggle(user.id)}
                    className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-colors ${
                      isSelected ? 'bg-primary/5' : 'hover:bg-accent'
                    }`}
                  >
                    <MemberAvatar name={user.name ?? user.email} />
                    <div className="flex-1 min-w-0 text-left">
                      {user.name && <p className="text-sm font-medium text-foreground truncate">{user.name}</p>}
                      <p className="text-xs text-muted-foreground truncate">{user.email}</p>
                    </div>
                    <div className={`w-5 h-5 rounded-md border flex items-center justify-center shrink-0 transition-colors ${
                      isSelected ? 'bg-primary border-primary text-white' : 'border-border'
                    }`}>
                      {isSelected && <Check size={12} />}
                    </div>
                  </button>
                )
              })
            )}
          </div>

          <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border shrink-0">
            <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
              Cancel
            </button>
            <button
              type="button"
              onClick={handleSubmit}
              disabled={selected.size === 0 || add.isPending}
              className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
            >
              {add.isPending
                ? 'Adding...'
                : `Add ${selected.size > 0 ? `${selected.size} ` : ''}member${selected.size !== 1 ? 's' : ''}`}
            </button>
          </div>
        </>
      )}
    </Modal>
  )
}

