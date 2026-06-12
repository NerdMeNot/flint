import { runStatusVisualFor } from '#/lib/status'

// Shared run-status pill for engine runs (CI + Workflows). Icon and colour come
// from the central status visual language (#/lib/status) so statuses read
// identically across products.
export function RunStatusPill({ status, size = 'md' }: { status: string; size?: 'sm' | 'md' }) {
  const v = runStatusVisualFor(status)
  return (
    <span
      className={`inline-flex items-center gap-1.5 font-medium ${v.text} ${
        size === 'sm' ? 'text-[11px]' : 'text-xs'
      }`}
    >
      <v.Icon size={13} className={v.spin ? 'animate-spin' : undefined} />
      {v.label}
    </span>
  )
}
