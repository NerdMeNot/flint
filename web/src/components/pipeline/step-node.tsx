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

export function StepNode({ data, sourcePosition, targetPosition }: NodeProps) {
  const step = data as PipelineStep
  const isGate = step.execType === 'gate'

  return (
    <>
      <Handle
        type="target"
        position={targetPosition ?? Position.Left}
        className="!bg-border !w-2 !h-2"
      />
      {isGate ? (
        <div
          className="flex items-center gap-3 rounded-lg border-[1.5px] border-dashed border-warning px-5 py-4 w-[300px] shadow-sm"
          style={{
            background: 'color-mix(in oklab, var(--warning) 8%, var(--card))',
            color: 'var(--card-foreground)',
          }}
        >
          <ShieldCheck size={20} className="text-warning shrink-0" />
          <span className="text-base font-semibold truncate text-warning">{step.name}</span>
        </div>
      ) : (
        <div
          className={`flex items-center gap-3 rounded-lg border px-5 py-4 w-[300px] shadow-sm transition-colors ${borderClass(step.status)}`}
          style={{ background: 'var(--card)', color: 'var(--card-foreground)' }}
        >
          <StatusIcon status={step.status} />
          <span className="text-base font-medium truncate">{step.name}</span>
        </div>
      )}
      <Handle
        type="source"
        position={sourcePosition ?? Position.Right}
        className="!bg-border !w-2 !h-2"
      />
    </>
  )
}

function StatusIcon({ status }: { status: PipelineStep['status'] }) {
  const size = 20
  switch (status) {
    case 'succeeded':
      return <CheckCircle size={size} className="text-success shrink-0" />
    case 'failed':
      return <XCircle size={size} className="text-destructive shrink-0" />
    case 'running':
      return (
        <Loader2 size={size} className="text-primary shrink-0 animate-spin" />
      )
    case 'waiting':
      return <Pause size={size} className="text-warning shrink-0" />
    case 'queued':
      return <Clock size={size} className="text-purple-400 shrink-0" />
    case 'skipped':
      return (
        <SkipForward size={size} className="text-muted-foreground shrink-0" />
      )
    case 'cancelled':
      return <Ban size={size} className="text-muted-foreground shrink-0" />
    default:
      return <Circle size={size} className="text-muted-foreground shrink-0" />
  }
}

function borderClass(status: PipelineStep['status']): string {
  switch (status) {
    case 'succeeded':
      return 'border-success/30'
    case 'failed':
      return 'border-destructive/30'
    case 'running':
      return 'border-primary/50'
    case 'waiting':
      return 'border-warning/30'
    default:
      return 'border-border'
  }
}
