import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { ArrowLeft, Mail, KeyRound, Users, Shield, Plus, Key, Copy, Check, Clock, AlertTriangle, Calendar } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { ScopeBadges } from '#/components/ScopeBadges'
import { PermissionMatrix } from '#/components/PermissionMatrix'
import { Modal } from '#/components/Modal'
import { FormSelect } from '#/components/FormSelect'
import { Badge } from '#/components/Badge'
import { ConfirmButton } from '#/components/ConfirmButton'
import { MemberAvatar } from '#/components/MemberAvatar'
import { formatTime } from '#/lib/format-time'
import { useCopyToClipboard } from '#/hooks/use-copy-to-clipboard'

export const Route = createFileRoute('/settings/users/$id')({
  component: UserDetailPage,
})

function UserDetailPage() {
  const { id } = Route.useParams()
  const [showAssignRole, setShowAssignRole] = useState(false)

  const { data: usersData } = useSuspenseQuery(orpc.users.list.queryOptions({ input: {} }))
  const allUsers = usersData.items
  const { data: assignmentsData } = useSuspenseQuery(orpc.roles.assignments.list.queryOptions({ input: {} }))
  const assignments = assignmentsData.items
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items
  const { data: teamsData } = useSuspenseQuery(orpc.teams.list.queryOptions({ input: {} }))
  const teams = teamsData.items

  const user = allUsers.find((u) => u.id === id)
  if (!user) {
    return (
      <div className="space-y-3">
        <Link to="/settings/users" className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors">
          <ArrowLeft size={13} /> All users
        </Link>
        <p className="text-sm text-muted-foreground">User not found.</p>
      </div>
    )
  }

  const roleMap = new Map(roles.map((r) => [r.slug, r]))

  // Direct role assignments for this user
  const directRoles = assignments
    .filter((a) => a.subject === user.email)
    .map((a) => roleMap.get(a.role))
    .filter(Boolean) as typeof roles

  // Team-inherited roles
  const teamAssignments = assignments.filter((a) => a.subject.startsWith('team:'))
  const teamRoleMap = new Map<string, typeof roles>()
  for (const a of teamAssignments) {
    const role = roleMap.get(a.role)
    if (role) {
      const list = teamRoleMap.get(a.subject) ?? []
      list.push(role)
      teamRoleMap.set(a.subject, list)
    }
  }

  return (
    <div className="space-y-5">
      <Link
        to="/settings/users"
        className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
      >
        <ArrowLeft size={13} />
        All users
      </Link>

      {/* User header */}
      <div className="island-shell p-4 sm:p-5">
        <div className="flex items-center gap-4">
          <MemberAvatar name={user.name ?? user.email} size={48} />
          <div className="min-w-0">
            {user.name && (
              <h2 className="display-title text-lg font-bold text-foreground truncate">{user.name}</h2>
            )}
            <p className="text-sm text-muted-foreground flex items-center gap-1.5">
              <Mail size={13} className="shrink-0" />
              <span className="truncate min-w-0" title={user.email}>{user.email}</span>
            </p>
            <p className="text-xs text-muted-foreground opacity-50 font-mono mt-0.5">{user.id}</p>
          </div>
        </div>
      </div>

      {/* Direct roles */}
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <KeyRound size={14} className="text-muted-foreground" />
            <h3 className="text-xs font-semibold text-foreground">Direct Roles</h3>
            <span className="text-[11px] text-muted-foreground opacity-50">{directRoles.length}</span>
          </div>
          <button
            type="button"
            onClick={() => setShowAssignRole(true)}
            className="flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium text-primary hover:bg-accent transition-colors"
          >
            <Plus size={12} />
            Assign role
          </button>
        </div>

        {directRoles.length === 0 ? (
          <p className="text-xs text-muted-foreground opacity-50 pl-5">No roles assigned directly.</p>
        ) : (
          <div className="space-y-2">
            {directRoles.map((role) => (
              <div key={role.id} className="island-shell p-3 space-y-2">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-semibold text-foreground">{role.name}</span>
                    {role.isSystem && <Badge variant="primary">System</Badge>}
                  </div>
                  <ScopeBadges workspaces={role.workspaces} environments={role.environments} />
                </div>
                {role.description && (
                  <p className="text-xs text-muted-foreground">{role.description}</p>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Team memberships + inherited roles */}
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <Users size={14} className="text-muted-foreground" />
          <h3 className="text-xs font-semibold text-foreground">Team Memberships</h3>
        </div>

        <TeamMemberships userId={user.id} teams={teams} teamRoleMap={teamRoleMap} />
      </div>

      {/* Effective permissions summary */}
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <Shield size={14} className="text-muted-foreground" />
          <h3 className="text-xs font-semibold text-foreground">Effective Permissions</h3>
          <span className="text-[11px] text-muted-foreground opacity-50">Combined from all roles</span>
        </div>
        <div className="island-shell p-3">
          <PermissionMatrix permissions={mergePermissions(directRoles, teamRoleMap)} />
        </div>
      </div>

      {/* Personal tokens */}
      <PersonalTokensSection userId={user.id} userName={user.name ?? user.email} />

      {showAssignRole && (
        <AssignRoleToSubjectModal
          subject={user.email}
          subjectLabel={user.name ?? user.email}
          onClose={() => setShowAssignRole(false)}
        />
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Personal tokens section
// ---------------------------------------------------------------------------

const EXPIRY_OPTIONS = [
  { key: '30d', label: '30 days' },
  { key: '90d', label: '90 days' },
  { key: '1y', label: '1 year' },
  { key: 'none', label: 'No expiry' },
]

function computeExpiry(preset: string): string | undefined {
  const now = new Date()
  switch (preset) {
    case '30d': return new Date(now.getTime() + 30 * 86400000).toISOString()
    case '90d': return new Date(now.getTime() + 90 * 86400000).toISOString()
    case '1y': return new Date(now.getTime() + 365 * 86400000).toISOString()
    default: return undefined
  }
}

function PersonalTokensSection({ userId, userName }: { userId: string; userName: string }) {
  const { data: tokensData } = useSuspenseQuery(
    orpc.personalTokens.list.queryOptions({ input: { userId } }),
  )
  const tokens = tokensData.items
  const [showGenerate, setShowGenerate] = useState(false)
  const del = useAction((id: string) => client.personalTokens.delete({ id }), {
    invalidate: [orpc.personalTokens.list.key()],
  })

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Key size={14} className="text-muted-foreground" />
          <h3 className="text-xs font-semibold text-foreground">Personal Tokens</h3>
          <span className="text-[11px] text-muted-foreground opacity-50">{tokens.length}</span>
        </div>
        <button
          type="button"
          onClick={() => setShowGenerate(true)}
          className="flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium text-primary hover:bg-accent transition-colors"
        >
          <Plus size={12} />
          Generate token
        </button>
      </div>

      {tokens.length === 0 ? (
        <p className="text-xs text-muted-foreground opacity-50 pl-5">No personal tokens. Generate one to use the Flint CLI.</p>
      ) : (
        <div className="space-y-1.5">
          {tokens.map((token) => {
            const isExpired = token.expiresAt && new Date(token.expiresAt) < new Date()
            return (
              <div
                key={token.id}
                className={`island-shell p-3 flex items-center justify-between gap-3 ${isExpired ? 'border-warning' : ''}`}
              >
                <div className="flex items-center gap-3 min-w-0">
                  <Key size={13} className={isExpired ? 'text-warning shrink-0' : 'text-muted-foreground shrink-0'} />
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-foreground truncate">{token.name}</p>
                    <div className="flex flex-wrap items-center gap-3 text-[12px] text-muted-foreground mt-0.5">
                      <span className="flex items-center gap-1">
                        <Calendar size={10} />
                        Created {formatTime(token.createdAt)}
                      </span>
                      {token.lastUsedAt && (
                        <span className="flex items-center gap-1">
                          <Clock size={10} />
                          Last used {formatTime(token.lastUsedAt)}
                        </span>
                      )}
                      {token.expiresAt && (
                        <span className={`flex items-center gap-1 ${isExpired ? 'text-warning' : ''}`}>
                          <AlertTriangle size={10} />
                          {isExpired ? 'Expired' : 'Expires'} {formatTime(token.expiresAt)}
                        </span>
                      )}
                    </div>
                  </div>
                </div>
                <ConfirmButton onConfirm={() => del.mutate(token.id)} title="Revoke token" />
              </div>
            )
          })}
        </div>
      )}

      {showGenerate && (
        <GenerateTokenModal userId={userId} userName={userName} onClose={() => setShowGenerate(false)} />
      )}
    </div>
  )
}

function GenerateTokenModal({ userId, userName, onClose }: { userId: string; userName: string; onClose: () => void }) {
  const [name, setName] = useState('')
  const [expiry, setExpiry] = useState('90d')
  const [generatedToken, setGeneratedToken] = useState<string | null>(null)
  const { copied, copy } = useCopyToClipboard()

  const create = useAction(
    (input: { userId: string; name: string; expiresAt: string | undefined }) =>
      client.personalTokens.create(input),
    {
      invalidate: [orpc.personalTokens.list.key()],
      onSuccess: (result) => setGeneratedToken((result as { token: string }).token),
    },
  )

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim()) return

    create.mutate({
      userId,
      name,
      expiresAt: computeExpiry(expiry),
    })
  }

  return (
    <Modal open onClose={onClose} title="Generate Personal Token" subtitle={`Token for ${userName} — inherits your permissions`}>
      {generatedToken ? (
        <div className="px-5 py-5 space-y-4">
          <div className="rounded-lg border border-success bg-success-subtle p-4 space-y-2">
            <p className="text-xs font-semibold text-success">Token generated</p>
            <p className="text-[12px] text-muted-foreground">
              Copy this token now. It will not be shown again. Use it in the Flint CLI:
            </p>
            <code className="block text-[12px] text-muted-foreground font-mono mt-1">
              flint auth login --token &lt;token&gt;
            </code>
            <div className="flex items-center gap-2 mt-3">
              <code className="flex-1 rounded-md bg-card border border-border px-3 py-2 text-xs font-mono text-foreground break-all">
                {generatedToken}
              </code>
              <button
                type="button"
                onClick={() => generatedToken && copy(generatedToken)}
                className="shrink-0 flex items-center gap-1.5 rounded-lg border border-border px-3 py-2 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
              >
                {copied ? <Check size={12} className="text-success" /> : <Copy size={12} />}
                {copied ? 'Copied' : 'Copy'}
              </button>
            </div>
          </div>
          <div className="flex justify-end">
            <button type="button" onClick={onClose}
              className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
              Done
            </button>
          </div>
        </div>
      ) : (
        <form onSubmit={handleSubmit}>
          <div className="px-5 py-4 space-y-4">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Name <span className="text-destructive">*</span></label>
              <input
                type="text" required value={name} onChange={(e) => setName(e.target.value)}
                placeholder="MacBook Pro"
                className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
              />
              <p className="text-[12px] text-muted-foreground">A name to identify this token (e.g., your device name)</p>
            </div>

            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Expiry</label>
              <div className="flex flex-wrap gap-1.5">
                {EXPIRY_OPTIONS.map((opt) => (
                  <button key={opt.key} type="button" onClick={() => setExpiry(opt.key)}
                    className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                      expiry === opt.key
                        ? 'bg-accent text-primary ring-1 ring-primary/20'
                        : 'text-muted-foreground border border-border hover:text-foreground hover:bg-accent'
                    }`}>
                    {opt.label}
                  </button>
                ))}
              </div>
            </div>

            <div className="rounded-lg border border-border p-3">
              <p className="text-[12px] text-muted-foreground">
                This token will have the same permissions as <span className="font-medium text-foreground">{userName}</span>.
                If your roles change, the token's access changes automatically.
              </p>
            </div>
          </div>

          <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border">
            <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
              Cancel
            </button>
            <button type="submit" disabled={!name.trim() || create.isPending}
              className="flex items-center gap-1.5 rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
              <Key size={12} />
              {create.isPending ? 'Generating…' : 'Generate token'}
            </button>
          </div>
        </form>
      )}
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// Assign role to a specific subject (used from user + team detail pages)
// ---------------------------------------------------------------------------

function AssignRoleToSubjectModal({
  subject,
  subjectLabel,
  onClose,
}: {
  subject: string
  subjectLabel: string
  onClose: () => void
}) {
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items
  const [selectedRole, setSelectedRole] = useState('')
  const [submitted, setSubmitted] = useState(false)

  const role = roles.find((r) => r.slug === selectedRole)

  const assign = useAction(
    () => client.roles.assignments.create({ subjects: [subject], role: selectedRole }),
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
    <Modal open onClose={onClose} title="Assign Role" subtitle={`Assign a role to ${subjectLabel}`}>
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
// Team memberships — need to fetch each team to check membership
// ---------------------------------------------------------------------------

type TeamRole = { id: string; name: string; slug: string; isSystem: boolean; workspaces: string[]; environments: string[] }

function TeamMemberships({
  userId,
  teams,
  teamRoleMap,
}: {
  userId: string
  teams: Array<{ id: string; name: string; slug: string; source: string; memberCount: number }>
  teamRoleMap: Map<string, Array<TeamRole>>
}) {
  // Render one chip per team; each chip fetches its own team (with members) at
  // the top of its component and only renders if the user is a member — keeping
  // the hook call out of a `.map()` (Rules of Hooks).
  return (
    <>
      {/* The chip container; each chip short-circuits to null when the user is
          not a member, so we can't know the count up front. When every chip
          renders null this <div> is `:empty`, which reveals the sibling empty
          state via the `[&:empty+p]:block` rule below. */}
      <div className="space-y-2 empty:hidden [&:empty+p]:block">
        {teams.map((t) => (
          <TeamMembershipChip
            key={t.id}
            teamId={t.id}
            userId={userId}
            inherited={teamRoleMap.get(`team:${t.slug}`) ?? []}
          />
        ))}
      </div>
      <p className="text-xs text-muted-foreground opacity-50 pl-5 hidden">Not a member of any team.</p>
    </>
  )
}

function TeamMembershipChip({
  teamId,
  userId,
  inherited,
}: {
  teamId: string
  userId: string
  inherited: Array<TeamRole>
}) {
  const { data: team } = useSuspenseQuery(orpc.teams.get.queryOptions({ input: { id: teamId } }))
  if (!team.members.some((m) => m.id === userId)) return null

  return (
    <Link
      to="/settings/teams/$id"
      params={{ id: team.id }}
      className="island-shell p-3 flex items-center justify-between gap-3 hover:bg-accent transition-colors group"
    >
      <div className="flex items-center gap-2 min-w-0">
        <Users size={14} className="text-muted-foreground shrink-0" />
        <span className="text-sm font-medium text-foreground group-hover:text-primary transition-colors truncate">{team.name}</span>
        <Badge variant={team.source === 'idp' ? 'primary' : 'neutral'}>
          {team.source === 'idp' ? 'IdP' : 'Internal'}
        </Badge>
      </div>
      <div className="flex flex-wrap gap-1 shrink-0">
        {inherited.length === 0 ? (
          <span className="text-[11px] text-muted-foreground opacity-40">No team roles</span>
        ) : (
          inherited.map((role) => (
            <Badge key={role.slug} variant="neutral">{role.name}</Badge>
          ))
        )}
      </div>
    </Link>
  )
}

// ---------------------------------------------------------------------------
// Merge permissions from all roles (direct + team)
// ---------------------------------------------------------------------------

function mergePermissions(
  directRoles: Array<{ permissions: Array<{ object: string; action: string }> }>,
  teamRoleMap: Map<string, Array<{ permissions: Array<{ object: string; action: string }> }>>,
): Array<{ object: string; action: string }> {
  const set = new Set<string>()
  for (const role of directRoles) {
    for (const p of role.permissions) set.add(`${p.object}:${p.action}`)
  }
  for (const roles of teamRoleMap.values()) {
    for (const role of roles) {
      for (const p of role.permissions) set.add(`${p.object}:${p.action}`)
    }
  }
  return [...set].map((key) => {
    const [object, action] = key.split(':')
    return { object: object!, action: action! }
  })
}

