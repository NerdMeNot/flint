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

export function StepNode({ data }: NodeProps) {
  const step = data as PipelineStep
  const isGate = step.execType === 'gate'

  return (
    <>
      <Handle
        type="target"
        position={Position.Left}
        className="!bg-border !w-2 !h-2"
      />
      {isGate ? (
        <div
          className="flex items-center gap-2 rounded-md px-3 py-2 min-w-[130px] shadow-sm cursor-pointer"
          style={{
            background: 'color-mix(in oklab, var(--warning) 8%, var(--card))',
            color: 'var(--card-foreground)',
            border: '1.5px dashed var(--warning)',
          }}
        >
          <ShieldCheck size={14} className="text-warning shrink-0" />
          <span className="text-xs font-semibold truncate text-warning">{step.name}</span>
        </div>
      ) : (
        <div
          className={`flex items-center gap-2 rounded-md px-3 py-2 min-w-[130px] shadow-sm transition-colors cursor-pointer ${borderClass(step.status)}`}
          style={{ background: 'var(--card)', color: 'var(--card-foreground)', border: '1px solid var(--border)' }}
        >
          <StatusIcon status={step.status} />
          <span className="text-xs font-medium truncate">{step.name}</span>
        </div>
      )}
      <Handle
        type="source"
        position={Position.Right}
        className="!bg-border !w-2 !h-2"
      />
    </>
  )
}

function StatusIcon({ status }: { status: PipelineStep['status'] }) {
  const size = 14
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
