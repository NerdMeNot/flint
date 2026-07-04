import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { ArrowLeft } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { PoolEditor } from '#/components/runners/PoolEditor'

export const Route = createFileRoute('/settings/runners/$name')({
  component: RunnerEditPage,
})

function RunnerEditPage() {
  const { name } = Route.useParams()
  const { data } = useSuspenseQuery(orpc.runners.list.queryOptions({ input: {} }))
  const pool = data.items.find((r) => r.name === name)

  if (!pool) {
    return (
      <div className="space-y-4">
        <Link to="/settings/runners" className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors">
          <ArrowLeft size={13} /> Runner pools
        </Link>
        <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
          <span className="text-sm">Pool not found.</span>
        </div>
      </div>
    )
  }

  // Key by name so navigating between pools remounts the editor — otherwise its
  // useState seeds keep the previous pool's form values.
  return <PoolEditor key={pool.name} pool={pool} />
}
