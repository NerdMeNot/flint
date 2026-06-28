import { Loader2 } from 'lucide-react'

// Default loading state for routes whose data is still resolving (client
// navigations, and the first client render of SSR-deferred queries). Calm and
// non-jarring — a centered spinner rather than a layout-shifting skeleton.
export function RoutePending() {
  return (
    <div className="flex min-h-[40vh] items-center justify-center" role="status" aria-label="Loading">
      <Loader2 size={20} className="animate-spin text-muted-foreground" aria-hidden />
    </div>
  )
}
