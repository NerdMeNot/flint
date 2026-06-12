import { CheckCircle, XCircle, Loader2, Clock, Ban, type LucideIcon } from 'lucide-react'

// Single source of truth for run-status visual language (icon, spin, text colour,
// accent bar). Previously duplicated across RunStatusPill, RunRow, and the
// command palette. Labels are intentionally NOT centralised — context varies
// (e.g. a run row says "Passed" while a pill says "Succeeded").
export type RunStatusVisual = {
  Icon: LucideIcon
  spin?: boolean
  /** text colour utility */
  text: string
  /** accent-bar background utility */
  accent: string
  /** default human label */
  label: string
}

const runStatusVisual: Record<string, RunStatusVisual> = {
  succeeded: { Icon: CheckCircle, text: 'text-success', accent: 'bg-success', label: 'Succeeded' },
  failed: { Icon: XCircle, text: 'text-destructive', accent: 'bg-destructive', label: 'Failed' },
  running: { Icon: Loader2, spin: true, text: 'text-primary', accent: 'bg-primary', label: 'Running' },
  pending: { Icon: Clock, text: 'text-muted-foreground', accent: 'bg-muted-foreground/30', label: 'Pending' },
  cancelled: { Icon: Ban, text: 'text-muted-foreground', accent: 'bg-muted-foreground/30', label: 'Cancelled' },
}

export function runStatusVisualFor(status: string): RunStatusVisual {
  return runStatusVisual[status] ?? runStatusVisual.pending!
}
