import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Mail, Users } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { ScopeBadges } from '#/components/ScopeBadges'
import { PageHeader } from '#/components/PageHeader'
import { EmptyState } from '#/components/EmptyState'
import { MemberAvatar } from '#/components/MemberAvatar'
import { Badge } from '#/components/Badge'

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
      <PageHeader
        title="Users"
        subtitle={`${users.length} ${users.length === 1 ? 'user' : 'users'}`}
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
