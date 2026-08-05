import { useQuery } from '@tanstack/react-query'
import { FlaskConical } from 'lucide-react'
import { orpc } from '#/lib/orpc'

// DemoModeBanner shows a persistent strip when the backend is a local demo
// deployment (seeded data + simulated step execution) rather than production, so
// demo state is never mistaken for real runs.
export function DemoModeBanner() {
  const { data } = useQuery({
    ...orpc.meta.get.queryOptions(),
    staleTime: 10_000,
  })

  if (!data?.isDemo) return null

  const label = 'Demo environment — data is seeded and step execution is simulated.'

  return (
    <div
      role="status"
      className="flex items-center justify-center gap-2 bg-warning-subtle px-4 py-1.5 text-center text-xs font-medium text-warning-foreground"
      style={{ color: 'var(--warning)' }}
    >
      <FlaskConical size={13} aria-hidden />
      <span>{label}</span>
    </div>
  )
}
