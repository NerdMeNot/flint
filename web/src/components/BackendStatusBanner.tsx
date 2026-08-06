import { useQuery } from '@tanstack/react-query'
import { PlugZap } from 'lucide-react'

// BackendStatusBanner shows a non-blocking strip when the Go API is unreachable,
// so the user knows on-screen data may be stale while the app keeps running. It
// polls the same-origin /api/health probe; React Query's refetchOnReconnect and
// the data queries' own retry handle recovery, and this banner disappears as soon
// as the API answers again. Complements RootError (which covers a hard failure on
// initial load); this covers the API going away mid-session.
export function BackendStatusBanner() {
  const { isError } = useQuery({
    queryKey: ['backend-health'],
    queryFn: async () => {
      const res = await fetch('/api/health')
      if (!res.ok) throw new Error('backend unreachable')
      return true
    },
    refetchInterval: 5_000,
    // A hidden tab must not keep probing: with a dead session this poll was
    // part of what kept a backgrounded tab busy and growing.
    refetchIntervalInBackground: false,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })

  if (!isError) return null

  return (
    <div
      role="status"
      className="flex items-center justify-center gap-2 bg-warning-subtle px-4 py-1.5 text-center text-xs font-medium"
      style={{ color: 'var(--warning)' }}
    >
      <PlugZap size={13} aria-hidden />
      <span>Reconnecting to Flint — the API is unreachable, so data may be stale.</span>
    </div>
  )
}
