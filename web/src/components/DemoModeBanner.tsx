import { useQuery } from '@tanstack/react-query'
import { FlaskConical } from 'lucide-react'
import { orpc } from '#/lib/orpc'

// DemoModeBanner shows a persistent strip when the backend is serving canned
// mock data (`flint server --mock`) instead of real state. It exists so demo
// data is never mistaken for real state (writes are in-memory only).
export function DemoModeBanner() {
  const { data } = useQuery({
    ...orpc.meta.get.queryOptions(),
    staleTime: 10_000,
  })

  if (!data?.usingMockData) return null

  const label = 'Demo data — server is in mock mode (--mock). Changes are in-memory only.'

  return (
    <div
      role="status"
      className="flex items-center justify-center gap-2 bg-warning/15 px-4 py-1.5 text-center text-xs font-medium text-warning-foreground"
      style={{ color: 'var(--warning)' }}
    >
      <FlaskConical size={13} aria-hidden />
      <span>{label}</span>
    </div>
  )
}
