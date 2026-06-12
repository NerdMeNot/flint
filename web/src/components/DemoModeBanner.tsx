import { useQuery } from '@tanstack/react-query'
import { FlaskConical } from 'lucide-react'
import { orpc } from '#/lib/orpc'

// DemoModeBanner shows a persistent strip when the app is serving mock data
// instead of the real backend — either because FLINT_API_MODE=mock, or because
// the backend is unreachable in auto mode. It exists so demo data is never
// mistaken for real state (and so writes that only mutate the mock store are
// understood as such).
export function DemoModeBanner() {
  const { data } = useQuery({
    ...orpc.meta.get.queryOptions(),
    staleTime: 10_000,
  })

  if (!data?.usingMockData) return null

  const label =
    data.mode === 'mock'
      ? 'Demo data — mock mode (FLINT_API_MODE=mock). Changes are in-memory only.'
      : 'Backend unreachable — showing demo data. Changes are not persisted.'

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
