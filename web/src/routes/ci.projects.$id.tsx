import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState, useEffect, useRef } from 'react'
import {
  CheckCircle,
  XCircle,
  Loader2,
  Clock,
  Ban,
  Network,
  FileCode,
  ExternalLink,
  AlertTriangle,
  Play,
  ChevronDown,
  Plus,
  Timer,
  Layers,
  Shield,
  ArrowRight,
  ScrollText,
} from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import type { PipelineDefinition, PipelineRun } from '#/lib/api/types'
import { Modal } from '#/components/Modal'
import { DagView } from '#/components/pipeline/dag-view'
import { RunRow } from '#/components/RunRow'
import { TagChip } from '#/components/TagChip'
import { TagManagerModal, type TagGroup } from '#/components/TagManagerModal'
import { BackLink } from '#/components/BackLink'
import { ProjectHealthBar } from '#/components/ProjectHealth'
import { parseDurationToSeconds, median, groupByBucket } from '#/lib/run-feed'

export const Route = createFileRoute('/ci/projects/$id')({
  component: ProjectDetailPage,
})

type Tab = 'runs' | 'pipeline' | 'yaml'

function fmtDuration(secs: number): string {
  if (!secs) return '—'
  const m = Math.floor(secs / 60)
  const s = Math.round(secs % 60)
  return m ? `${m}m ${s}s` : `${s}s`
}

function ProjectDetailPage() {
  const { id } = Route.useParams()
  const [pipelineIdx, setPipelineIdx] = useState(0)
  const [tab, setTab] = useState<Tab>('runs')
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

  // Filter runs to the active pipeline's workflow file
  const filteredRuns = activePipeline
    ? allRuns.filter((r) => r.workflowFile === activePipeline.filename)
    : allRuns

  // Typical (median) duration across this project's finished runs.
  const medianSecs = median(
    allRuns
      .filter((r) => r.status === 'succeeded' || r.status === 'failed')
      .map((r) => parseDurationToSeconds(r.duration))
      .filter((s) => s > 0),
  )

  const tabs = [
    { key: 'runs' as const, icon: ScrollText, label: 'Runs', count: filteredRuns.length },
    { key: 'pipeline' as const, icon: Network, label: 'Pipeline' },
    { key: 'yaml' as const, icon: FileCode, label: 'YAML' },
  ]

  return (
    <div className="rise-in space-y-5">
      <BackLink fallbackTo="/ci/projects" label="Back to projects" />

      {/* Project header */}
      <div className="island-shell p-4 sm:p-5 lg:p-6 space-y-4">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1.5 lg:space-y-2 min-w-0">
            <div className="flex items-center gap-2.5">
              <div className="w-2.5 h-2.5 lg:w-3 lg:h-3 rounded-full shrink-0" style={{ backgroundColor: project.colour }} />
              <h1 className="display-title text-2xl lg:text-3xl font-bold text-foreground truncate">
                {project.name}
              </h1>
              <span className="island-kicker !text-[11px] shrink-0">{project.workspace}</span>
            </div>
            <p className="text-sm lg:text-base text-muted-foreground font-mono">{project.repo}</p>
          </div>

          <div className="flex items-center gap-3 shrink-0">
            {project.lastRun && (
              <Link
                to="/ci/runs/$id"
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

        <ProjectTags projectId={id} tags={project.tags} />

        {/* Health / activity stat strip */}
        <div className="flex flex-wrap items-start gap-x-8 gap-y-3 pt-4 border-t border-border">
          <StatCell label="Health">
            <ProjectHealthBar health={project.health} />
          </StatCell>
          <StatCell label="Last run">
            {project.lastRun ? (
              <span className="flex items-center gap-1.5">
                <RunStatusIcon status={project.lastRun.status} size={13} />
                <span className="text-muted-foreground">{project.lastRun.startedAt}</span>
              </span>
            ) : <span className="text-muted-foreground">—</span>}
          </StatCell>
          <StatCell label="Typical duration">
            <span className="flex items-center gap-1.5 text-foreground">
              <Timer size={13} className="text-muted-foreground" />
              {fmtDuration(medianSecs)}
            </span>
          </StatCell>
          <StatCell label="Total runs">
            <span className="tabular-nums text-foreground">{allRuns.length}</span>
          </StatCell>
        </div>
      </div>

      {/* Compact pipeline switcher */}
      {pipelines.length > 1 && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60 mr-1">Pipelines</span>
          {pipelines.map((p, i) => (
            <button
              key={p.filename}
              type="button"
              onClick={() => setPipelineIdx(i)}
              className={`flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-xs font-medium transition-colors ${
                i === pipelineIdx
                  ? 'border-primary/40 bg-primary/5 text-foreground'
                  : 'border-border text-muted-foreground hover:text-foreground'
              } ${p.status === 'invalid' ? '!border-destructive/40' : ''}`}
            >
              <FileCode size={13} className={p.status === 'invalid' ? 'text-destructive' : i === pipelineIdx ? 'text-primary' : 'opacity-60'} />
              <span className="font-mono">{p.filename}</span>
              <span className="opacity-50">{p.steps.length}</span>
              {p.status === 'invalid' && <AlertTriangle size={11} className="text-destructive" />}
            </button>
          ))}
        </div>
      )}

      {/* Error banner for invalid pipelines */}
      {activePipeline?.status === 'invalid' && activePipeline.errors && (
        <PipelineErrorBanner errors={activePipeline.errors} filename={activePipeline.filename} />
      )}

      {/* Body — leads with run history; pipeline/YAML are reference tabs. */}
      <div className="space-y-3">
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
                <span className="ml-0.5 text-[11px] opacity-60">{t.count}</span>
              )}
            </button>
          ))}
        </div>

        {tab === 'runs' && <ProjectRunsTab runs={filteredRuns} projectId={id} />}
        {tab === 'pipeline' && activePipeline && <PipelineTab pipeline={activePipeline} />}
        {tab === 'yaml' && activePipeline && <YamlTab yaml={activePipeline.yaml} />}
      </div>

      {showTrigger && (
        <TriggerOverlay
          projectId={id}
          pipeline={activePipeline ?? null}
          onClose={() => setShowTrigger(false)}
        />
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Header stat cell
// ---------------------------------------------------------------------------

function StatCell({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">{label}</span>
      <div className="text-sm">{children}</div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Runs tab — the project's run history as a triage feed (the primary content).
// ---------------------------------------------------------------------------

function ProjectRunsTab({ runs, projectId }: { runs: PipelineRun[]; projectId: string }) {
  if (runs.length === 0) {
    return (
      <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
        <ScrollText size={32} strokeWidth={1.2} />
        <span className="text-sm">No runs for this pipeline yet.</span>
      </div>
    )
  }

  // Single project → one duration baseline shared across rows.
  const baselineSecs = median(
    runs
      .filter((r) => r.status === 'succeeded' || r.status === 'failed')
      .map((r) => parseDurationToSeconds(r.duration))
      .filter((s) => s > 0),
  )
  const grouped = groupByBucket(runs)

  return (
    <div className="island-shell !p-0 overflow-hidden">
      <div className="flex items-center justify-between px-4 lg:px-5 py-2.5 border-b border-border">
        <span className="text-xs font-semibold text-foreground">{runs.length} run{runs.length === 1 ? '' : 's'}</span>
        <Link
          to="/ci/runs"
          search={{ project: projectId }}
          className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-primary transition-colors"
        >
          View in Runs <ArrowRight size={11} />
        </Link>
      </div>
      {grouped.map(({ bucket, runs: bucketRuns }) => (
        <div key={bucket}>
          <div className="flex items-center gap-2 px-4 lg:px-5 py-2 bg-accent/20 border-b border-border text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/70">
            {bucket}
            <span className="rounded-full bg-border/70 px-1.5 py-0.5 text-[10px] font-bold leading-none text-muted-foreground">{bucketRuns.length}</span>
          </div>
          <div className="divide-y divide-border">
            {bucketRuns.map((run) => (
              <RunRow key={run.id} run={run} showProject={false} baselineSecs={baselineSecs} />
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Trigger overlay
// ---------------------------------------------------------------------------

// pipeline is null when the project has no parsed pipeline definition yet —
// triggering still works (the API only needs project + branch); the overlay
// just has no dispatch inputs to offer.
function TriggerOverlay({
  projectId,
  pipeline,
  onClose,
}: {
  projectId: string
  pipeline: PipelineDefinition | null
  onClose: () => void
}) {
  const [inputs, setInputs] = useState<Record<string, string>>(() => {
    const defaults: Record<string, string> = {}
    for (const input of pipeline?.dispatchInputs ?? []) {
      defaults[input.name] = input.default ?? ''
    }
    return defaults
  })
  const [submitted, setSubmitted] = useState(false)

  const trigger = useAction(
    () => client.runs.trigger({ projectId, branch: inputs.ref || inputs.branch || 'main' }),
    {
      invalidate: [orpc.runs.list.key()],
      onSuccess: () => {
        setSubmitted(true)
        setTimeout(onClose, 1500)
      },
    },
  )

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    trigger.mutate(undefined)
  }

  const { data: envData } = useSuspenseQuery(orpc.environments.list.queryOptions({ input: {} }))
  const environments = envData.items
  const [environment, setEnvironment] = useState('')

  return (
    <Modal
      open
      onClose={onClose}
      title="Run pipeline"
      subtitle={pipeline?.filename}
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
                {pipeline?.dispatchInputs && pipeline.dispatchInputs.length > 0 ? (
                  pipeline.dispatchInputs.map((input) => (
                    <div key={input.name} className="space-y-1.5">
                      <label className="text-xs font-medium text-foreground">
                        {input.name}
                        {input.required && <span className="text-destructive ml-0.5">*</span>}
                      </label>
                      {input.description && (
                        <p className="text-[12px] text-muted-foreground">{input.description}</p>
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
                  disabled={trigger.isPending}
                  className="flex items-center gap-1.5 rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-50"
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

function PipelineTab({ pipeline }: { pipeline: PipelineDefinition }) {
  // The definition graph — every step shown "pending" (this is structure, not a
  // run). Per-run status lives on the run detail page.
  const steps = pipeline.steps.map((s) => ({ ...s, status: 'pending' as const }))
  const waves = steps.length > 0 ? Math.max(...steps.map((s) => s.wave)) + 1 : 0
  const gates = steps.filter((s) => s.execType === 'gate').length

  return (
    <div className="space-y-3">
      {/* Compact definition meta (replaces the old details rail + chip list). */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <span className="font-mono text-foreground">{pipeline.filename}</span>
        <span className="flex items-center gap-1"><Network size={12} /> {steps.length} steps</span>
        <span className="flex items-center gap-1"><Layers size={12} /> {waves} {waves === 1 ? 'wave' : 'waves'}</span>
        {gates > 0 && <span className="flex items-center gap-1"><Shield size={12} /> {gates} {gates === 1 ? 'gate' : 'gates'}</span>}
      </div>

      {/* Vertical DAG — reads top → bottom like the pipeline runs. */}
      <div className="island-shell !p-0 overflow-hidden h-[440px] lg:h-[640px]">
        <DagView steps={steps as any} direction="DOWN" />
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// YAML tab
// ---------------------------------------------------------------------------

// Singleton highlighter — loads only YAML grammar + one theme
let highlighterPromise: Promise<any> | null = null
function getHighlighter() {
  if (!highlighterPromise) {
    highlighterPromise = import('shiki').then(({ createHighlighter }) =>
      createHighlighter({
        themes: ['github-dark'],
        langs: ['yaml'],
      }),
    ).catch((err) => {
      console.warn('Shiki failed to load, falling back to plain text:', err)
      highlighterPromise = null
      return null
    })
  }
  return highlighterPromise
}

function YamlTab({ yaml }: { yaml: string }) {
  const [html, setHtml] = useState<string>('')
  const lineCount = yaml.split('\n').length

  useEffect(() => {
    let cancelled = false
    getHighlighter()
      .then((highlighter) => {
        if (!highlighter) return null
        return highlighter.codeToHtml(yaml, { lang: 'yaml', theme: 'github-dark' })
      })
      .then((result) => {
        if (!cancelled && result) setHtml(result)
      })
      .catch(() => {}) // fallback to plain text
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
          className="[&_pre]:!bg-[#0d1117] [&_pre]:p-4 [&_pre]:sm:p-5 [&_pre]:lg:p-6 [&_pre]:max-h-[75vh] [&_pre]:overflow-auto [&_pre]:text-[13px] [&_pre]:lg:text-[14px] [&_pre]:leading-[1.65] [&_code]:font-mono"
          dangerouslySetInnerHTML={{ __html: html }}
        />
      ) : (
        <div className="bg-[#0d1117] p-4 sm:p-5 lg:p-6 font-mono text-[13px] lg:text-[14px] leading-[1.65] text-[#c9d1d9] whitespace-pre max-h-[75vh] overflow-auto">
          {yaml}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Shared components
// ---------------------------------------------------------------------------

function RunStatusIcon({ status, size = 16 }: { status: string; size?: number }) {
  switch (status) {
    case 'succeeded': return <CheckCircle size={size} className="text-success shrink-0" />
    case 'failed': return <XCircle size={size} className="text-destructive shrink-0" />
    case 'running': return <Loader2 size={size} className="text-primary shrink-0 animate-spin" />
    case 'cancelled': return <Ban size={size} className="text-muted-foreground shrink-0" />
    default: return <Clock size={size} className="text-muted-foreground shrink-0" />
  }
}

// ---------------------------------------------------------------------------
// Project tags — UI-managed labels constrained to the curated registry. Chips
// for applied tags (collapsed past a threshold); a roomy grouped modal to browse
// and apply at scale (see TagManagerModal). Optimistic local state keeps fast
// toggles from racing the write.
// ---------------------------------------------------------------------------

const TAG_CHIP_LIMIT = 8

function ProjectTags({ projectId, tags }: { projectId: string; tags: string[] }) {
  const { data: regData } = useSuspenseQuery(orpc.tags.registry.list.queryOptions({ input: {} }))
  const registry = new Map(regData.items.map((k) => [k.key, k]))
  const groups: TagGroup[] = regData.items.map((k) => ({
    key: k.key,
    label: k.label,
    color: k.color,
    values: k.allowedValues,
  }))

  const [optimistic, setOptimistic] = useState<string[] | null>(null)
  const [open, setOpen] = useState(false)
  const current = optimistic ?? tags

  const save = useAction(
    (next: string[]) => client.projects.setTags({ id: projectId, tags: next }),
    { invalidate: [orpc.projects.get.key(), orpc.projects.list.key()], onSuccess: () => setOptimistic(null) },
  )
  const persist = (next: string[]) => { setOptimistic(next); save.mutate(next) }
  const toggle = (t: string) => persist(current.includes(t) ? current.filter((x) => x !== t) : [...current, t])

  const shown = current.slice(0, TAG_CHIP_LIMIT)
  const overflow = current.length - shown.length

  return (
    <div className="mt-3 flex flex-wrap items-center gap-1.5">
      {shown.map((t) => <TagChip key={t} tag={t} registry={registry} onRemove={() => toggle(t)} />)}
      {overflow > 0 && (
        <button
          type="button"
          onClick={() => setOpen(true)}
          className="rounded-md border border-border px-2 py-0.5 text-[12px] font-medium text-muted-foreground hover:text-foreground transition-colors"
        >
          +{overflow} more
        </button>
      )}
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="inline-flex items-center gap-1 rounded-md border border-dashed border-border px-2 py-0.5 text-[12px] font-medium text-muted-foreground hover:text-foreground transition-colors"
      >
        <Plus size={11} /> {current.length > 0 ? 'edit tags' : 'add tags'}
      </button>
      <TagManagerModal
        open={open}
        onClose={() => setOpen(false)}
        groups={groups}
        applied={new Set(current)}
        onToggle={toggle}
        onClear={() => persist([])}
      />
    </div>
  )
}
