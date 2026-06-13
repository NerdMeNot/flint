import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { ArrowLeft } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { RoleEditor } from '#/components/roles/RoleEditor'

export const Route = createFileRoute('/settings/roles/$id')({
  component: RoleDetailPage,
})

function RoleDetailPage() {
  const { id } = Route.useParams()
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const role = rolesData.items.find((r) => r.id === id)

  if (!role) {
    return (
      <div className="space-y-4">
        <Link to="/settings/roles" className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors">
          <ArrowLeft size={13} /> Roles
        </Link>
        <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
          <span className="text-sm">Role not found.</span>
        </div>
      </div>
    )
  }

  return <RoleEditor role={role} />
}
