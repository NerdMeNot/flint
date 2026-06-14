import { useCallback, useEffect, useMemo } from 'react'
import {
  ReactFlow,
  type Node,
  type Edge,
  Background,
  BackgroundVariant,
  Controls,
  MarkerType,
  useNodesState,
  useEdgesState,
  Position,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { StepNode } from './step-node'

import type { PipelineStep } from '#/lib/api/types'
export type { PipelineStep } from '#/lib/api/types'

export type DagDirection = 'RIGHT' | 'DOWN'

interface DagViewProps {
  steps: PipelineStep[]
  direction?: DagDirection
  onStepClick?: (stepName: string) => void
}

const nodeTypes = { step: StepNode }

// ---------------------------------------------------------------------------
// Layout constants
// ---------------------------------------------------------------------------

const NODE_WIDTH = 300
const NODE_HEIGHT = 88
const GAP_X = 130 // gap between waves (along the flow axis)
const GAP_Y = 64 // gap between siblings inside a wave
const PAD = 70

// ---------------------------------------------------------------------------
// Simple layered layout using wave numbers (no ELK needed)
// ---------------------------------------------------------------------------

function layoutGraph(
  rawSteps: PipelineStep[],
  direction: DagDirection = 'RIGHT',
): { nodes: Node[]; edges: Edge[] } {
  const steps = buildDependencies(rawSteps)

  // Group by wave
  const waves = new Map<number, PipelineStep[]>()
  for (const step of steps) {
    const group = waves.get(step.wave) ?? []
    group.push(step)
    waves.set(step.wave, group)
  }
  const sortedWaves = [...waves.entries()].sort((a, b) => a[0] - b[0])
  const isHorizontal = direction === 'RIGHT'

  // Center each wave on the perpendicular axis so parallel siblings
  // sit symmetrically around the canvas midline (and under their
  // parent's center) instead of left-aligning into a stair-step.
  const perpDim = isHorizontal ? NODE_HEIGHT : NODE_WIDTH
  const waveExtent = (n: number) => n * perpDim + Math.max(n - 1, 0) * GAP_Y
  const maxExtent = Math.max(
    ...sortedWaves.map(([, ws]) => waveExtent(ws.length)),
  )

  const nodes: Node[] = []
  for (let wi = 0; wi < sortedWaves.length; wi++) {
    const [, waveSteps] = sortedWaves[wi]
    const offset = (maxExtent - waveExtent(waveSteps.length)) / 2

    for (let si = 0; si < waveSteps.length; si++) {
      const step = waveSteps[si]
      const x = isHorizontal
        ? PAD + wi * (NODE_WIDTH + GAP_X)
        : PAD + offset + si * (NODE_WIDTH + GAP_Y)
      const y = isHorizontal
        ? PAD + offset + si * (NODE_HEIGHT + GAP_Y)
        : PAD + wi * (NODE_HEIGHT + GAP_X)

      nodes.push({
        id: step.name,
        type: 'step',
        position: { x, y },
        data: step,
        sourcePosition: isHorizontal ? Position.Right : Position.Bottom,
        targetPosition: isHorizontal ? Position.Left : Position.Top,
      })
    }
  }

  const edges: Edge[] = steps.flatMap((step) =>
    (step.dependsOn ?? []).map((dep) => {
      const color = edgeColor(step.status)
      return {
        id: `${dep}->${step.name}`,
        source: dep,
        target: step.name,
        // smoothstep = orthogonal routing with rounded corners. Reads as a
        // proper flowchart: lines run cleanly along the axis and turn at
        // 90° with a gentle radius, no dramatic bezier swoops.
        type: 'smoothstep',
        pathOptions: { borderRadius: 18, offset: 24 },
        animated: step.status === 'running',
        style: { stroke: color, strokeWidth: 1.75 },
        markerEnd: {
          type: MarkerType.ArrowClosed,
          width: 16,
          height: 16,
          color,
        },
      }
    }),
  )

  return { nodes, edges }
}

function edgeColor(status: PipelineStep['status']): string {
  switch (status) {
    case 'succeeded': return '#22c55e'
    case 'failed': return '#ef4444'
    case 'running': return '#3b82f6'
    case 'waiting': return '#eab308'
    case 'skipped':
    case 'cancelled': return '#64748b'
    default: return '#475569'
  }
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function DagView({ steps, direction = 'RIGHT', onStepClick }: DagViewProps) {
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([])

  const layout = useMemo(
    () => (steps.length > 0 ? layoutGraph(buildDependencies(steps), direction) : null),
    [steps, direction],
  )

  useEffect(() => {
    if (!layout) return
    setNodes(layout.nodes)
    setEdges(layout.edges)
  }, [layout, setNodes, setEdges])

  const handleNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      onStepClick?.(node.id)
    },
    [onStepClick],
  )

  // The DAG is a read-only diagram: nodes can't be dragged or connected, and
  // scroll doesn't hijack the page (zoom via the controls). Pan by dragging the
  // canvas is kept for graphs larger than the viewport. Clicking a node only
  // does something when a handler is provided (e.g. the run page).
  const clickable = !!onStepClick

  return (
    <div
      className={`h-full w-full bg-card min-h-[300px] ${
        clickable ? '[&_.react-flow__node]:cursor-pointer' : '[&_.react-flow__node]:cursor-default'
      }`}
    >
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={handleNodeClick}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.18, maxZoom: 1.1 }}
        proOptions={{ hideAttribution: true }}
        minZoom={0.4}
        maxZoom={1.6}
        nodesDraggable={false}
        nodesConnectable={false}
        nodesFocusable={clickable}
        elementsSelectable={clickable}
        edgesFocusable={false}
        zoomOnScroll={false}
        zoomOnDoubleClick={false}
        panOnScroll={false}
      >
        <Background
          variant={BackgroundVariant.Dots}
          color="var(--border)"
          gap={18}
          size={1}
        />
        <Controls
          showInteractive={false}
          className="!shadow-none [&>button]:!bg-card [&>button]:!border-border [&>button]:!text-muted-foreground [&>button:hover]:!text-foreground"
        />
      </ReactFlow>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Build dependency edges from wave numbers when explicit dependsOn is missing
// ---------------------------------------------------------------------------

function buildDependencies(steps: PipelineStep[]): PipelineStep[] {
  const hasDeps = steps.some((s) => s.dependsOn && s.dependsOn.length > 0)
  if (hasDeps) return steps

  const byWave = new Map<number, string[]>()
  for (const step of steps) {
    const names = byWave.get(step.wave) ?? []
    names.push(step.name)
    byWave.set(step.wave, names)
  }

  return steps.map((step) => ({
    ...step,
    dependsOn: byWave.get(step.wave - 1) ?? [],
  }))
}
