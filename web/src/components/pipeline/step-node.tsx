import { Handle, Position, type NodeProps } from '@xyflow/react'
import {
  CheckCircle,
  XCircle,
  Loader2,
  Clock,
  Pause,
  SkipForward,
  Ban,
  Circle,
  ShieldCheck,
} from 'lucide-react'
import type { PipelineStep } from '#/lib/api/types'

// Accent colour drives the whole node (chip, left bar, border tint). Pending uses
// the brand accent rather than grey so the *definition* graph (every step
// pending) still reads as designed, not a black-and-white wireframe.
function accentVar(status: PipelineStep['status'], isGate: boolean): string {
  if (isGate) return 'var(--warning)'
  switch (status) {
    case 'succeeded': return 'var(--success)'
    case 'failed': return 'var(--destructive)'
    case 'running': return 'var(--ring)'
    case 'waiting': return 'var(--warning)'
    case 'queued': return '#a855f7'
    case 'skipped':
    case 'cancelled': return 'var(--muted-foreground)'
    default: return 'var(--ring)' // pending
  }
}

export function StepNode({ data, sourcePosition, targetPosition }: NodeProps) {
  const step = data as PipelineStep
  const isGate = step.execType === 'gate'
  const accent = accentVar(step.status, isGate)
  const isRunning = step.status === 'running'

  const sublabel = isGate
    ? 'Approval gate'
    : `Wave ${step.wave + 1}${step.dependsOn && step.dependsOn.length > 0 ? ` · needs ${step.dependsOn.length}` : ''}`

  return (
    <>
      <Handle type="target" position={targetPosition ?? Position.Left} className="!w-1 !h-1 !min-w-0 !border-0 !bg-transparent" />

      <div
        className="relative flex items-center gap-3 w-[300px] rounded-xl pl-4 pr-3.5 py-3 overflow-hidden transition-all"
        style={{
          background: `linear-gradient(160deg, color-mix(in oklab, ${accent} 6%, var(--card)), var(--card) 70%)`,
          border: `1px solid color-mix(in oklab, ${accent} 28%, var(--border))`,
          boxShadow: isRunning
            ? `0 1px 2px rgb(0 0 0 / 0.18), 0 0 0 3px color-mix(in oklab, ${accent} 18%, transparent)`
            : '0 1px 2px rgb(0 0 0 / 0.18)',
        }}
      >
        {/* Left status accent bar */}
        <span className="absolute left-0 top-0 bottom-0 w-[3px]" style={{ background: accent }} />

        {/* Icon chip */}
        <span
          className="flex items-center justify-center w-9 h-9 rounded-lg shrink-0"
          style={{ background: `color-mix(in oklab, ${accent} 16%, transparent)`, color: accent }}
        >
          {isGate ? <ShieldCheck size={18} /> : <StatusGlyph status={step.status} />}
        </span>

        {/* Text */}
        <div className="min-w-0 flex-1">
          <p className="text-[15px] font-semibold truncate text-foreground leading-tight">{step.name}</p>
          <p className="text-xs text-muted-foreground truncate mt-0.5">{sublabel}</p>
        </div>
      </div>

      <Handle type="source" position={sourcePosition ?? Position.Right} className="!w-1 !h-1 !min-w-0 !border-0 !bg-transparent" />
    </>
  )
}

// Status glyph inherits the chip's colour (currentColor) so it matches the accent.
function StatusGlyph({ status }: { status: PipelineStep['status'] }) {
  const size = 18
  switch (status) {
    case 'succeeded': return <CheckCircle size={size} />
    case 'failed': return <XCircle size={size} />
    case 'running': return <Loader2 size={size} className="animate-spin" />
    case 'waiting': return <Pause size={size} />
    case 'queued': return <Clock size={size} />
    case 'skipped': return <SkipForward size={size} />
    case 'cancelled': return <Ban size={size} />
    default: return <Circle size={size} />
  }
}
