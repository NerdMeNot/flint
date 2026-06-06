import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Mail } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { ScopeBadges } from '#/components/ScopeBadges'

export const Route = createFileRoute('/settings/users/')({
  component: UsersPage,
})

function UsersPage() {
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
      <div>
        <h2 className="display-title text-2xl lg:text-3xl text-foreground">Users</h2>
        <p className="text-muted-foreground text-sm lg:text-base mt-1">{users.length} users</p>
      </div>

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
                      <span
                        key={role.slug}
                        className={`rounded-md px-1.5 py-0.5 text-[11px] font-medium ${
                          role.isSystem
                            ? 'bg-primary/10 text-primary border border-primary/20'
                            : 'bg-secondary text-foreground border border-border'
                        }`}
                      >
                        {role.name}
                      </span>
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
    </div>
  )
}

const avatarColors = [
  'bg-blue-500/15 text-blue-400',
  'bg-emerald-500/15 text-emerald-400',
  'bg-violet-500/15 text-violet-400',
  'bg-amber-500/15 text-amber-400',
  'bg-rose-500/15 text-rose-400',
  'bg-cyan-500/15 text-cyan-400',
  'bg-pink-500/15 text-pink-400',
  'bg-teal-500/15 text-teal-400',
]

function MemberAvatar({ name }: { name: string }) {
  const hash = name.split('').reduce((acc, c) => acc + c.charCodeAt(0), 0)
  const color = avatarColors[hash % avatarColors.length]!
  const initials = name.split(' ').map((w) => w[0]).slice(0, 2).join('').toUpperCase()

  return (
    <div className={`w-8 h-8 rounded-full flex items-center justify-center shrink-0 ${color}`}>
      <span className="text-xs font-semibold">{initials}</span>
    </div>
  )
}
