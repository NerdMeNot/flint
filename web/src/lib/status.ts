import { CheckCircle, XCircle, Loader2, Clock, Ban, type LucideIcon } from 'lucide-react'

// Single source of truth for run-status visual language (icon, spin, text colour,
// accent bar). Previously duplicated across RunStatusPill, RunRow, and the
// command palette. Labels are intentionally NOT centralised — context varies
// (e.g. a run row says "Passed" while a pill says "Succeeded").
export type RunStatusVisual = {
  Icon: LucideIcon
  spin?: boolean
  /** text colour utility — for icons and inline text on a card surface */
  text: string
  /** accent-bar background utility */
  accent: string
  /** solid chip fill + its ink. Every pairing is a designed fill/ink couple:
   *  the ink is never a constant, because a bright dark-mode fill needs
   *  near-black on it while the same token in light mode needs white. The
   *  pending pair leans on muted-foreground and background always being
   *  opposite in lightness, so it clears contrast in all six palettes. */
  chip: string
  /** default human label */
  label: string
}

const runStatusVisual: Record<string, RunStatusVisual> = {
  succeeded: { Icon: CheckCircle, text: 'text-success', accent: 'bg-success', chip: 'bg-success text-success-foreground', label: 'Succeeded' },
  failed: { Icon: XCircle, text: 'text-destructive', accent: 'bg-destructive', chip: 'bg-destructive text-destructive-foreground', label: 'Failed' },
  running: { Icon: Loader2, spin: true, text: 'text-primary', accent: 'bg-primary', chip: 'bg-primary text-primary-foreground', label: 'Running' },
  pending: { Icon: Clock, text: 'text-muted-foreground', accent: 'bg-muted-foreground', chip: 'bg-muted-foreground text-background', label: 'Pending' },
  cancelled: { Icon: Ban, text: 'text-muted-foreground', accent: 'bg-muted-foreground', chip: 'bg-muted-foreground text-background', label: 'Cancelled' },
}

export function runStatusVisualFor(status: string): RunStatusVisual {
  return runStatusVisual[status] ?? runStatusVisual.pending!
}
