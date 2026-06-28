import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Mail, Users, Plus } from 'lucide-react'
import { useState } from 'react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { Modal } from '#/components/Modal'
import { FormSelect } from '#/components/FormSelect'
import { ScopeBadges } from '#/components/ScopeBadges'
import { PageHeader } from '#/components/PageHeader'
import { EmptyState } from '#/components/EmptyState'
import { MemberAvatar } from '#/components/MemberAvatar'
import { Badge } from '#/components/Badge'

export const Route = createFileRoute('/settings/users/')({
  component: UsersPage,
})

const userInputClass =
  'w-full rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring/40'

function NewUserModal({ onClose }: { onClose: () => void }) {
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items
  const [email, setEmail] = useState('')
  const [name, setName] = useState('')
  const [role, setRole] = useState('')
  const [password, setPassword] = useState('')
  const [generated, setGenerated] = useState<string | null>(null)

  const create = useAction(client.users.create, {
    invalidate: [orpc.users.list.key(), orpc.roles.assignments.list.key()],
    onSuccess: (res) => {
      // Keep the modal open to show a server-generated password once; otherwise close.
      if (res?.generatedPassword) setGenerated(res.generatedPassword)
      else onClose()
    },
  })

  if (generated) {
    return (
      <Modal open onClose={onClose} title="User created" subtitle={email}>
        <div className="px-5 py-4 space-y-3">
          <p className="text-sm text-foreground">
            Share this temporary password with the user — it won't be shown again.
          </p>
          <code className="block rounded-lg border border-border bg-muted/40 px-3 py-2 text-sm font-mono break-all">
            {generated}
          </code>
        </div>
        <div className="flex justify-end px-5 py-3 border-t border-border">
          <button type="button" onClick={onClose} className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white" style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
            Done
          </button>
        </div>
      </Modal>
    )
  }

  return (
    <Modal open onClose={onClose} title="Add user" subtitle="Create a local (email + password) user">
      <div className="px-5 py-4 space-y-4">
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-foreground">Email</label>
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="dev@flint.dev" className={userInputClass} autoFocus />
        </div>
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-foreground">Name</label>
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Optional" className={userInputClass} />
        </div>
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-foreground">Role</label>
          <FormSelect
            value={role}
            onChange={setRole}
            placeholder="Default role"
            options={roles.map((r) => ({ key: r.slug, label: r.name }))}
          />
        </div>
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-foreground">Password</label>
          <input type="text" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Leave blank to auto-generate" className={`${userInputClass} font-mono`} />
        </div>
        {create.isError && <p className="text-xs text-red-500">Could not create user — that email may already be in use.</p>}
      </div>
      <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border">
        <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
          Cancel
        </button>
        <button
          type="button"
          disabled={!email.trim() || create.isPending}
          onClick={() => create.mutate({
            email: email.trim(),
            name: name.trim() || undefined,
            role: role || undefined,
            password: password || undefined,
          })}
          className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
          style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
        >
          {create.isPending ? 'Creating…' : 'Create user'}
        </button>
      </div>
    </Modal>
  )
}

function UsersPage() {
  const [showNew, setShowNew] = useState(false)
  const { data: usersData } = useSuspenseQuery(orpc.users.list.queryOptions({ input: {} }))
  const users = usersData.items
  const { data: assignmentsData } = useSuspenseQuery(orpc.roles.assignments.list.queryOptions({ input: {} }))
  const assignments = assignmentsData.items
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items
  const roleMap = new Map(roles.map((r) => [r.slug, r]))

  // Build per-user role and team lookups
  const userRoles = new Map<string, typeof roles>()
  for (const a of assignments) {
    if (a.subject.startsWith('team:')) continue
    const list = userRoles.get(a.subject) ?? []
    const role = roleMap.get(a.role)
    if (role) list.push(role)
    userRoles.set(a.subject, list)
  }

  // Team assignments — expand to user members
  const teamRoles = new Map<string, typeof roles>()
  for (const a of assignments) {
    if (!a.subject.startsWith('team:')) continue
    const role = roleMap.get(a.role)
    if (role) {
      const list = teamRoles.get(a.subject) ?? []
      list.push(role)
      teamRoles.set(a.subject, list)
    }
  }

  return (
    <div className="space-y-5">
      {showNew && <NewUserModal onClose={() => setShowNew(false)} />}
      <PageHeader
        title="Users"
        subtitle={`${users.length} ${users.length === 1 ? 'user' : 'users'}`}
        action={
          <button
            type="button"
            onClick={() => setShowNew(true)}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors"
            style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
          >
            <Plus size={14} />
            Add user
          </button>
        }
      />

      {users.length === 0 ? (
        <EmptyState icon={Users} message="No users yet." />
      ) : (
      <div className="island-shell !p-0 overflow-hidden">
        <div className="hidden sm:grid sm:grid-cols-[1fr_1fr_1fr] gap-3 px-4 py-2.5 border-b border-border bg-muted/30 text-[12px] font-medium text-muted-foreground uppercase tracking-wider">
          <span>User</span>
          <span>Roles</span>
          <span>Scope</span>
        </div>

        <div className="divide-y divide-border">
          {users.map((user) => {
            const directRoles = userRoles.get(user.email) ?? []

            return (
              <Link
                key={user.id}
                to="/settings/users/$id"
                params={{ id: user.id }}
                className="block px-4 py-3 sm:grid sm:grid-cols-[1fr_1fr_1fr] sm:gap-3 sm:items-center space-y-2 sm:space-y-0 hover:bg-accent/30 transition-colors group"
              >
                <div className="flex items-center gap-3 min-w-0">
                  <MemberAvatar name={user.name ?? user.email} />
                  <div className="min-w-0">
                    {user.name && (
                      <p className="text-sm font-medium text-foreground group-hover:text-primary transition-colors truncate">{user.name}</p>
                    )}
                    <p className="text-xs text-muted-foreground truncate flex items-center gap-1">
                      <Mail size={10} className="shrink-0" />
                      {user.email}
                    </p>
                  </div>
                </div>

                <div className="flex flex-wrap gap-1">
                  {directRoles.length === 0 ? (
                    <span className="text-xs text-muted-foreground opacity-40">No roles</span>
                  ) : (
                    directRoles.map((role) => (
                      <Badge key={role.slug} variant={role.isSystem ? 'primary' : 'neutral'}>
                        {role.name}
                      </Badge>
                    ))
                  )}
                </div>

                <div className="hidden sm:block">
                  {directRoles.length > 0 && (
                    <div className="flex flex-wrap gap-1">
                      {directRoles.some((r) => r.workspaces.length === 0 && r.environments.length === 0) ? (
                        <span className="text-[11px] text-muted-foreground opacity-50">All</span>
                      ) : (
                        <ScopeBadges
                          workspaces={[...new Set(directRoles.flatMap((r) => r.workspaces))]}
                          environments={[...new Set(directRoles.flatMap((r) => r.environments))]}
                        />
                      )}
                    </div>
                  )}
                </div>
              </Link>
            )
          })}
        </div>
      </div>
      )}
    </div>
  )
}
