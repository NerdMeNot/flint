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
          className="flex items-center gap-3.5 rounded-xl px-6 py-5 w-[300px] shadow-md cursor-pointer"
          style={{
            background: 'color-mix(in oklab, var(--warning) 8%, var(--card))',
            color: 'var(--card-foreground)',
            border: '1.5px dashed var(--warning)',
          }}
        >
          <ShieldCheck size={22} className="text-warning shrink-0" />
          <span className="text-base lg:text-lg font-semibold truncate text-warning">{step.name}</span>
        </div>
      ) : (
        <div
          className={`flex items-center gap-3.5 rounded-xl px-6 py-5 w-[300px] shadow-md transition-colors cursor-pointer ${borderClass(step.status)}`}
          style={{ background: 'var(--card)', color: 'var(--card-foreground)', border: '1px solid var(--border)' }}
        >
          <StatusIcon status={step.status} />
          <span className="text-base lg:text-lg font-medium truncate">{step.name}</span>
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
  const size = 22
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
