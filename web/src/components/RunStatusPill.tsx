import { CheckCircle, XCircle, Loader2, Clock, Ban } from 'lucide-react'

// Shared run-status pill for engine runs (CI + Workflows). Mirrors the icon and
// colour language used by RunRow so statuses read identically across products.
const config: Record<string, { icon: React.ReactNode; label: string; className: string }> = {
  succeeded: { icon: <CheckCircle size={13} />, label: 'Succeeded', className: 'text-success' },
  failed: { icon: <XCircle size={13} />, label: 'Failed', className: 'text-destructive' },
  running: { icon: <Loader2 size={13} className="animate-spin" />, label: 'Running', className: 'text-primary' },
  pending: { icon: <Clock size={13} />, label: 'Pending', className: 'text-muted-foreground' },
  cancelled: { icon: <Ban size={13} />, label: 'Cancelled', className: 'text-muted-foreground' },
}

export function RunStatusPill({ status, size = 'md' }: { status: string; size?: 'sm' | 'md' }) {
  const c = config[status] ?? config.pending!
  return (
    <span
      className={`inline-flex items-center gap-1.5 font-medium ${c.className} ${
        size === 'sm' ? 'text-[11px]' : 'text-xs'
      }`}
    >
      {c.icon}
      {c.label}
    </span>
  )
}
