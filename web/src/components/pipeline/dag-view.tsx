import { useCallback, useEffect, useState } from 'react'
import {
  ReactFlow,
  type Node,
  type Edge,
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  Position,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { StepNode } from './step-node'

// elkjs/lib/elk.bundled.js doesn't use web workers — safe for SSR.
let elkPromise: Promise<any> | null = null
function getElk() {
  if (!elkPromise) {
    elkPromise = import('elkjs/lib/elk.bundled.js')
  }
  return elkPromise
}

export interface PipelineStep {
  name: string
  status:
    | 'pending'
    | 'queued'
    | 'running'
    | 'waiting'
    | 'succeeded'
    | 'failed'
    | 'skipped'
    | 'cancelled'
  execType: string
  wave: number
  dependsOn?: string[]
  startedAt?: string
  finishedAt?: string
}

interface DagViewProps {
  steps: PipelineStep[]
  onStepClick?: (stepName: string) => void
}

const nodeTypes = { step: StepNode }

const ELK_OPTIONS = {
  'elk.algorithm': 'layered',
  'elk.direction': 'RIGHT',
  'elk.spacing.nodeNode': '60',
  'elk.layered.spacing.nodeNodeBetweenLayers': '80',
  'elk.layered.mergeEdges': 'true',
}

const NODE_WIDTH = 200
const NODE_HEIGHT = 64

async function layoutGraph(
  steps: PipelineStep[],
): Promise<{ nodes: Node[]; edges: Edge[] }> {
  const ELK = (await getElk()).default
  const elk = new ELK()

  const elkNodes = steps.map((step) => ({
    id: step.name,
    width: NODE_WIDTH,
    height: NODE_HEIGHT,
  }))

  const elkEdges = steps.flatMap((step) =>
    (step.dependsOn ?? []).map((dep) => ({
      id: `${dep}->${step.name}`,
      sources: [dep],
      targets: [step.name],
    })),
  )

  const graph = await elk.layout({
    id: 'root',
    layoutOptions: ELK_OPTIONS,
    children: elkNodes,
    edges: elkEdges,
  })

  const nodes: Node[] = (graph.children ?? []).map((node) => {
    const step = steps.find((s) => s.name === node.id)!
    return {
      id: node.id,
      type: 'step',
      position: { x: node.x ?? 0, y: node.y ?? 0 },
      data: step,
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
    }
  })

  const edges: Edge[] = steps.flatMap((step) =>
    (step.dependsOn ?? []).map((dep) => ({
      id: `${dep}->${step.name}`,
      source: dep,
      target: step.name,
      animated: step.status === 'running',
      style: { stroke: edgeColor(step.status) },
    })),
  )

  return { nodes, edges }
}

function edgeColor(status: PipelineStep['status']): string {
  switch (status) {
    case 'succeeded':
      return '#22c55e'
    case 'failed':
      return '#ef4444'
    case 'running':
      return '#3b82f6'
    case 'waiting':
      return '#eab308'
    case 'skipped':
    case 'cancelled':
      return '#64748b'
    default:
      return '#475569'
  }
}

export function DagView({ steps, onStepClick }: DagViewProps) {
  const [nodes, setNodes, onNodesChange] = useNodesState([])
  const [edges, setEdges, onEdgesChange] = useEdgesState([])

  useEffect(() => {
    if (steps.length === 0) return

    const stepsWithDeps = buildDependencies(steps)

    layoutGraph(stepsWithDeps).then(({ nodes: n, edges: e }) => {
      setNodes(n)
      setEdges(e)
    })
  }, [steps, setNodes, setEdges])

  const handleNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      onStepClick?.(node.id)
    },
    [onStepClick],
  )

  return (
    <div className="h-[400px] w-full rounded-xl border border-border bg-card">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={handleNodeClick}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.2 }}
        proOptions={{ hideAttribution: true }}
        minZoom={0.3}
        maxZoom={1.5}
      >
        <Background
          variant={BackgroundVariant.Dots}
          color="#334155"
          gap={16}
          size={1}
        />
        <Controls className="!bg-card !border-border !text-foreground [&>button]:!bg-card [&>button]:!border-border [&>button]:!text-foreground" />
        <MiniMap
          className="!bg-card !border-border"
          nodeColor={(node) =>
            statusColor((node.data as PipelineStep).status)
          }
          maskColor="rgba(15, 23, 42, 0.7)"
        />
      </ReactFlow>
    </div>
  )
}

function statusColor(status: PipelineStep['status']): string {
  switch (status) {
    case 'succeeded':
      return '#22c55e'
    case 'failed':
      return '#ef4444'
    case 'running':
      return '#3b82f6'
    case 'waiting':
      return '#eab308'
    case 'queued':
      return '#8b5cf6'
    case 'skipped':
    case 'cancelled':
      return '#64748b'
    default:
      return '#475569'
  }
}

/** Build dependency edges from wave numbers when explicit dependsOn is missing. */
function buildDependencies(steps: PipelineStep[]): PipelineStep[] {
  const hasDeps = steps.some((s) => s.dependsOn && s.dependsOn.length > 0)
  if (hasDeps) return steps

  const byWave = new Map<number, string[]>()
  for (const step of steps) {
    const names = byWave.get(step.wave) ?? []
    names.push(step.name)
    byWave.set(step.wave, names)
  }

  return steps.map((step) => {
    const prevWave = byWave.get(step.wave - 1)
    return {
      ...step,
      dependsOn: prevWave ?? [],
    }
  })
}
