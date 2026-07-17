// Shared labels/tones for engine transition events (engine_events), used by
// the run-level Timeline view and the per-step timeline panel.

export const EVENT_LABELS: Record<string, string> = {
  queued: 'Queued',
  claimed: 'Started',
  parked: 'Waiting',
  dispatched: 'Dispatched',
  succeeded: 'Succeeded',
  failed: 'Failed',
  skipped: 'Skipped',
  cancelled: 'Cancelled',
  timed_out: 'Timed out',
  retry_scheduled: 'Retry scheduled',
  retry_requeued: 'Retry re-queued',
  gate_approved: 'Gate approved',
  gate_rejected: 'Gate rejected',
  wait_signaled: 'Signal received',
  dispatch_failed: 'Dispatch failed',
  manual_resolve: 'Manually resolved',
  invoke_completed: 'Child workflow completed',
  seeded: 'Carried over',
  paused: 'Run paused',
  resumed: 'Run resumed',
  workflow_finished: 'Run finished',
  workflow_cancelled: 'Run cancelled',
}

export const EVENT_TONE: Record<string, string> = {
  succeeded: 'text-emerald-500',
  gate_approved: 'text-emerald-500',
  wait_signaled: 'text-emerald-500',
  workflow_finished: 'text-emerald-500',
  failed: 'text-destructive',
  timed_out: 'text-destructive',
  dispatch_failed: 'text-destructive',
  gate_rejected: 'text-destructive',
  cancelled: 'text-muted-foreground',
  skipped: 'text-muted-foreground',
  workflow_cancelled: 'text-muted-foreground',
  manual_resolve: 'text-amber-500',
  paused: 'text-amber-500',
  seeded: 'text-muted-foreground',
}
