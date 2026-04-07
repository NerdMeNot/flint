import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState, useEffect, useRef } from 'react'
import {
  GitBranch,
  GitCommit,
  CheckCircle,
  XCircle,
  Loader2,
  Clock,
  Ban,
  Network,
  FileCode,
  List,
  ExternalLink,
  AlertTriangle,
  Play,
  X,
  ChevronDown,
} from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import type { PipelineDefinition } from '#/lib/api/types'
import { Modal } from '#/components/Modal'
import { DagView } from '#/components/pipeline/dag-view'

export const Route = createFileRoute('/projects/$id')({
  component: ProjectDetailPage,
})

type Tab = 'runs' | 'dag' | 'yaml'

function ProjectDetailPage() {
  const { id } = Route.useParams()
  const [pipelineIdx, setPipelineIdx] = useState(0)
  const [tab, setTab] = useState<Tab>('dag')
  const [showTrigger, setShowTrigger] = useState(false)

  const { data: project } = useSuspenseQuery(
    orpc.projects.get.queryOptions({ input: { id } }),
  )
  const { data: runsData } = useSuspenseQuery(
    orpc.runs.list.queryOptions({ input: { projectId: id } }),
  )
  const { data: pipelines } = useSuspenseQuery(
    orpc.projects.pipelines.queryOptions({ input: { projectId: id } }),
  )

  const allRuns = runsData.items
  const activePipeline = pipelines[pipelineIdx] ?? pipelines[0]
  const dagSteps = activePipeline
    ? activePipeline.steps.map((s) => ({ ...s, status: 'pending' as const }))
    : []

  // Filter runs to the active pipeline's workflow file
  const filteredRuns = activePipeline
    ? allRuns.filter((r) => r.workflowFile === activePipeline.filename)
    : allRuns

  const tabs = [
    { key: 'dag' as const, icon: Network, label: 'Pipeline' },
    { key: 'yaml' as const, icon: FileCode, label: 'YAML' },
    { key: 'runs' as const, icon: List, label: 'Runs', count: filteredRuns.length },
  ]

  return (
    <div className="rise-in space-y-5">
      {/* Project header */}
      <div className="island-shell p-4 sm:p-5">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1.5 min-w-0">
            <div className="flex items-center gap-2.5">
              <div className="w-2.5 h-2.5 rounded-full shrink-0" style={{ backgroundColor: project.colour }} />
              <h1 className="display-title text-xl font-bold text-foreground truncate">
                {project.name}
              </h1>
              <span className="island-kicker !text-[0.55rem] shrink-0">{project.workspace}</span>
            </div>
            <p className="text-sm text-muted-foreground font-mono">{project.repo}</p>
          </div>

          <div className="flex items-center gap-3 shrink-0">
            {project.lastRun && (
              <Link
                to="/runs/$id"
                params={{ id: project.lastRun.id }}
                className="flex items-center gap-2 text-xs text-muted-foreground hover:text-foreground transition-colors"
              >
                <RunStatusIcon status={project.lastRun.status} size={14} />
                <span>Latest run</span>
                <ExternalLink size={11} />
              </Link>
            )}
            <button
              type="button"
              onClick={() => setShowTrigger(true)}
              className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
            >
              <Play size={12} />
              Run
            </button>
          </div>
        </div>

        {project.tags.length > 0 && (
          <div className="flex flex-wrap gap-1.5 mt-3">
            {project.tags.map((tag) => (
              <span key={tag} className="rounded-md bg-secondary border border-border px-2 py-0.5 text-[0.65rem] font-medium text-muted-foreground">
                {tag}
              </span>
            ))}
          </div>
        )}
      </div>

      {/* Pipeline selector — always visible */}
      <div className="flex flex-wrap gap-2">
        {pipelines.map((p, i) => (
          <button
            key={p.filename}
            type="button"
            onClick={() => setPipelineIdx(i)}
            className={`feature-card flex items-center gap-3 px-4 py-3 transition-all ${
              i === pipelineIdx ? 'ring-2 ring-primary/40' : ''
            } ${p.status === 'invalid' ? 'border-destructive/30' : ''}`}
          >
            <FileCode size={15} className={
              p.status === 'invalid'
                ? 'text-destructive'
                : i === pipelineIdx ? 'text-primary' : 'text-muted-foreground'
            } />
            <div className="text-left">
              <div className="flex items-center gap-1.5">
                <span className={`text-sm font-mono font-medium ${
                  i === pipelineIdx ? 'text-foreground' : 'text-muted-foreground'
                }`}>
                  {p.filename}
                </span>
                {p.status === 'invalid' && (
                  <span className="flex items-center gap-0.5 text-[0.6rem] font-semibold text-destructive">
                    <AlertTriangle size={10} />
                    invalid
                  </span>
                )}
              </div>
              <span className="text-[0.65rem] text-muted-foreground">
                {p.steps.length} steps
              </span>
            </div>
          </button>
        ))}
      </div>

      {/* Error banner for invalid pipelines */}
      {activePipeline?.status === 'invalid' && activePipeline.errors && (
        <PipelineErrorBanner errors={activePipeline.errors} filename={activePipeline.filename} />
      )}

      {/* Tab bar */}
      <div className="flex items-center gap-1 border-b border-border pb-px -mb-px">
        {tabs.map((t) => (
          <button
            key={t.key}
            type="button"
            onClick={() => setTab(t.key)}
            className={`flex items-center gap-1.5 shrink-0 px-3 py-2 text-xs font-medium border-b-2 transition-colors ${
              tab === t.key
                ? 'border-primary text-primary'
                : 'border-transparent text-muted-foreground hover:text-foreground hover:border-border'
            }`}
          >
            <t.icon size={13} />
            {t.label}
            {'count' in t && t.count !== undefined && (
              <span className="ml-0.5 text-[0.6rem] opacity-60">{t.count}</span>
            )}
          </button>
        ))}
      </div>

      {/* Tab content */}
      {tab === 'dag' && activePipeline && <PipelineTab steps={dagSteps} filename={activePipeline.filename} />}
      {tab === 'yaml' && activePipeline && <YamlTab yaml={activePipeline.yaml} />}
      {tab === 'runs' && <RunsTab runs={filteredRuns} />}

      {showTrigger && (
        <TriggerOverlay
          projectId={id}
          pipeline={activePipeline!}
          onClose={() => setShowTrigger(false)}
        />
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Runs tab
// ---------------------------------------------------------------------------

function RunsTab({ runs }: { runs: Array<{ id: string; projectName: string; projectColour: string; status: string; branch: string; commitSha: string; commitMessage: string; triggeredBy: string; triggerType: string; duration: string; startedAt: string; workflowFile: string }> }) {
  if (runs.length === 0) {
    return (
      <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
        <List size={32} strokeWidth={1.2} />
        <span className="text-sm">No runs for this project yet.</span>
      </div>
    )
  }

  return (
    <div className="island-shell !p-0 overflow-hidden">
      <div className="divide-y divide-border">
        {runs.map((run) => (
          <Link
            key={run.id}
            to="/runs/$id"
            params={{ id: run.id }}
            className="flex items-center gap-4 px-5 py-3 hover:bg-accent transition-colors group"
          >
            <RunStatusIcon status={run.status} size={16} />

            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2">
                <StatusBadge status={run.status} />
                <span className="text-xs text-muted-foreground font-mono opacity-60">
                  {run.workflowFile}
                </span>
              </div>
              <div className="flex items-center gap-2 mt-0.5">
                <GitCommit size={12} className="text-muted-foreground shrink-0" />
                <span className="text-xs text-muted-foreground truncate">{run.commitMessage}</span>
              </div>
            </div>

            <div className="hidden sm:flex items-center gap-3 shrink-0 text-xs text-muted-foreground">
              <span className="flex items-center gap-1">
                <GitBranch size={12} />
                <span className="font-mono">{run.branch}</span>
              </span>
              <span className="hidden md:inline font-mono opacity-60">{run.commitSha}</span>
              <span>{run.duration}</span>
              <span className="hidden lg:inline opacity-50">{run.startedAt}</span>
            </div>
          </Link>
        ))}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Trigger overlay
// ---------------------------------------------------------------------------

function TriggerOverlay({
  projectId,
  pipeline,
  onClose,
}: {
  projectId: string
  pipeline: PipelineDefinition
  onClose: () => void
}) {
  const [inputs, setInputs] = useState<Record<string, string>>(() => {
    const defaults: Record<string, string> = {}
    for (const input of pipeline.dispatchInputs ?? []) {
      defaults[input.name] = input.default ?? ''
    }
    return defaults
  })
  const [submitted, setSubmitted] = useState(false)

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!pipeline) return
    client.runs.trigger({
      projectId,
      branch: inputs.ref || inputs.branch || 'main',
    })
    console.log(`[mock] Triggering ${pipeline.filename} with inputs:`, inputs)
    setSubmitted(true)
    setTimeout(onClose, 1500)
  }

  const { data: envData } = useSuspenseQuery(orpc.environments.list.queryOptions({ input: {} }))
  const environments = envData.items
  const [environment, setEnvironment] = useState('')

  return (
    <Modal
      open
      onClose={onClose}
      title="Run pipeline"
      subtitle={pipeline.filename}
      wide
    >
      {submitted ? (
            <div className="px-5 py-8 text-center">
              <p className="text-sm text-success font-medium">Run triggered successfully</p>
            </div>
          ) : (
            <form onSubmit={handleSubmit}>
              <div className="px-5 py-4 space-y-4">
                {/* Environment */}
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-foreground">Environment</label>
                  <FormSelect
                    value={environment}
                    onChange={setEnvironment}
                    placeholder="None (default)"
                    options={environments.map((e) => ({ key: e.name, label: e.name }))}
                  />
                </div>

                {/* Dispatch inputs */}
                {pipeline.dispatchInputs && pipeline.dispatchInputs.length > 0 ? (
                  pipeline.dispatchInputs.map((input) => (
                    <div key={input.name} className="space-y-1.5">
                      <label className="text-xs font-medium text-foreground">
                        {input.name}
                        {input.required && <span className="text-destructive ml-0.5">*</span>}
                      </label>
                      {input.description && (
                        <p className="text-[0.65rem] text-muted-foreground">{input.description}</p>
                      )}
                      {input.type === 'choice' && input.options ? (
                        <FormSelect
                          value={inputs[input.name] ?? ''}
                          onChange={(v) => setInputs((prev) => ({ ...prev, [input.name]: v }))}
                          options={input.options.map((opt) => ({ key: opt, label: opt }))}
                        />
                      ) : input.type === 'boolean' ? (
                        <label className="flex items-center gap-2 cursor-pointer">
                          <input
                            type="checkbox"
                            checked={inputs[input.name] === 'true'}
                            onChange={(e) => setInputs((prev) => ({ ...prev, [input.name]: String(e.target.checked) }))}
                            className="rounded border-border"
                          />
                          <span className="text-sm text-muted-foreground">Enable</span>
                        </label>
                      ) : (
                        <input
                          type="text"
                          required={input.required}
                          value={inputs[input.name] ?? ''}
                          onChange={(e) => setInputs((prev) => ({ ...prev, [input.name]: e.target.value }))}
                          placeholder={input.default || input.name}
                          className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
                        />
                      )}
                    </div>
                  ))
                ) : (
                  <p className="text-xs text-muted-foreground py-2">
                    This pipeline has no configurable inputs. It will run with default settings.
                  </p>
                )}
              </div>

              <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border">
                <button
                  type="button"
                  onClick={onClose}
                  className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="flex items-center gap-1.5 rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors"
                  style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
                >
                  <Play size={12} />
                  Run pipeline
                </button>
              </div>
            </form>
          )}
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// Full-width form dropdown (for modals/forms, not header pills)
// ---------------------------------------------------------------------------

function FormSelect({
  value,
  onChange,
  options,
  placeholder,
}: {
  value: string
  onChange: (v: string) => void
  options: Array<{ key: string; label: string }>
  placeholder?: string
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  const selected = options.find((o) => o.key === value)

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="w-full flex items-center justify-between rounded-lg border border-border px-3 py-2 text-sm transition-colors hover:border-ring/40 focus:outline-none focus:ring-2 focus:ring-ring/40"
      >
        <span className={selected ? 'text-foreground' : 'text-muted-foreground/50'}>
          {selected?.label ?? placeholder ?? 'Select...'}
        </span>
        <ChevronDown size={14} className={`text-muted-foreground transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>

      {open && (
        <div
          className="absolute top-full left-0 right-0 mt-1 rounded-lg border border-border shadow-lg overflow-hidden z-50 max-h-[200px] overflow-y-auto"
          style={{ background: 'var(--surface-strong)' }}
        >
          {placeholder && (
            <button
              type="button"
              onClick={() => { onChange(''); setOpen(false) }}
              className={`w-full text-left px-3 py-2 text-sm transition-colors ${
                !value ? 'text-primary bg-primary/5' : 'text-muted-foreground hover:text-foreground hover:bg-accent'
              }`}
            >
              {placeholder}
            </button>
          )}
          {options.map((opt) => (
            <button
              key={opt.key}
              type="button"
              onClick={() => { onChange(opt.key); setOpen(false) }}
              className={`w-full text-left px-3 py-2 text-sm transition-colors ${
                value === opt.key ? 'text-primary bg-primary/5' : 'text-muted-foreground hover:text-foreground hover:bg-accent'
              }`}
            >
              {opt.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Pipeline error banner
// ---------------------------------------------------------------------------

function PipelineErrorBanner({ errors, filename }: { errors: string[]; filename: string }) {
  return (
    <div className="island-shell !p-0 overflow-hidden border-destructive/30">
      <div className="flex items-center gap-2 px-4 py-2.5 bg-destructive/5 border-b border-destructive/20">
        <AlertTriangle size={14} className="text-destructive shrink-0" />
        <span className="text-xs font-semibold text-destructive">
          {errors.length} {errors.length === 1 ? 'error' : 'errors'} in {filename}
        </span>
      </div>
      <div className="px-4 py-3 space-y-1.5">
        {errors.map((err, i) => (
          <div key={i} className="flex items-start gap-2 text-xs text-destructive/80">
            <span className="shrink-0 font-mono text-destructive/40">{i + 1}.</span>
            <span>{err}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Pipeline tab (DAG view)
// ---------------------------------------------------------------------------

function PipelineTab({ steps, filename }: { steps: Array<{ name: string; status: string; execType: string; wave: number; dependsOn?: string[] }>; filename: string }) {
  return (
    <div className="space-y-3">
      <p className="text-xs text-muted-foreground">
        <span className="font-mono">{filename}</span> — {steps.length} steps across {Math.max(...steps.map((s) => s.wave)) + 1} waves
      </p>

      {/* DAG — full width */}
      <div className="island-shell !p-0 overflow-hidden h-[350px]">
        <DagView steps={steps as any} />
      </div>

      {/* Step list — compact horizontal */}
      <div className="flex flex-wrap gap-1.5">
        {steps.map((step) => (
          <div
            key={step.name}
            className={`flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs ${
              step.execType === 'gate'
                ? 'border-warning/50 bg-warning/5 border-dashed'
                : 'border-border'
            }`}
            style={{ borderWidth: '1px', borderStyle: step.execType === 'gate' ? 'dashed' : 'solid' }}
          >
            <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${step.execType === 'gate' ? 'bg-warning' : 'bg-muted-foreground opacity-40'}`} />
            <span className={`font-mono font-medium ${step.execType === 'gate' ? 'text-warning' : 'text-foreground'}`}>{step.name}</span>
            {step.dependsOn && step.dependsOn.length > 0 && (
              <span className="text-muted-foreground opacity-40 text-[0.6rem] truncate max-w-[200px]">
                ← {step.dependsOn.join(', ')}
              </span>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// YAML tab
// ---------------------------------------------------------------------------

// Singleton highlighter — loads only YAML grammar + one theme (~50 KB vs ~8 MB full bundle)
let highlighterPromise: Promise<import('shiki').Highlighter> | null = null
function getHighlighter() {
  if (!highlighterPromise) {
    highlighterPromise = import('shiki/core').then(({ createHighlighter }) =>
      createHighlighter({
        themes: [import('shiki/themes/github-dark')],
        langs: [import('shiki/langs/yaml')],
      }),
    )
  }
  return highlighterPromise
}

function YamlTab({ yaml }: { yaml: string }) {
  const [html, setHtml] = useState<string>('')
  const lineCount = yaml.split('\n').length

  useEffect(() => {
    let cancelled = false
    getHighlighter()
      .then((highlighter) =>
        highlighter.codeToHtml(yaml, { lang: 'yaml', theme: 'github-dark' }),
      )
      .then((result) => {
        if (!cancelled) setHtml(result)
      })
    return () => { cancelled = true }
  }, [yaml])

  return (
    <div className="island-shell !p-0 overflow-hidden">
      <div className="flex items-center gap-2 px-5 py-3 border-b border-border">
        <FileCode size={13} className="text-primary" />
        <span className="text-xs font-semibold text-foreground">Pipeline Definition</span>
        <span className="text-xs text-muted-foreground opacity-50 ml-auto">{lineCount} lines</span>
      </div>
      {html ? (
        <div
          className="[&_pre]:!bg-[#0d1117] [&_pre]:p-4 [&_pre]:sm:p-5 [&_pre]:max-h-[75vh] [&_pre]:overflow-auto [&_pre]:text-xs [&_pre]:sm:text-[0.82rem] [&_pre]:leading-relaxed [&_code]:font-mono"
          dangerouslySetInnerHTML={{ __html: html }}
        />
      ) : (
        <div className="bg-[#0d1117] p-4 sm:p-5 font-mono text-xs leading-relaxed text-[#c9d1d9] whitespace-pre max-h-[75vh] overflow-auto">
          {yaml}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Shared components
// ---------------------------------------------------------------------------

function StatusBadge({ status }: { status: string }) {
  const styles: Record<string, string> = {
    succeeded: 'bg-success/10 text-success border-success/20',
    failed: 'bg-destructive/10 text-destructive border-destructive/20',
    running: 'bg-primary/10 text-primary border-primary/20',
    pending: 'bg-secondary text-muted-foreground border-border',
    cancelled: 'bg-secondary text-muted-foreground border-border',
  }

  return (
    <span className={`inline-flex rounded-full border px-1.5 py-px text-[0.6rem] font-semibold ${styles[status] ?? styles.pending}`}>
      {status}
    </span>
  )
}

function RunStatusIcon({ status, size = 16 }: { status: string; size?: number }) {
  switch (status) {
    case 'succeeded': return <CheckCircle size={size} className="text-success shrink-0" />
    case 'failed': return <XCircle size={size} className="text-destructive shrink-0" />
    case 'running': return <Loader2 size={size} className="text-primary shrink-0 animate-spin" />
    case 'cancelled': return <Ban size={size} className="text-muted-foreground shrink-0" />
    default: return <Clock size={size} className="text-muted-foreground shrink-0" />
  }
}
