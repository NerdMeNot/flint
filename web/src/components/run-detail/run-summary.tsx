import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { GitCommit, Clock, CheckCircle, Loader2, Timer, Ban, RotateCcw, ArrowRight, Pause, Hourglass, Gauge, Server } from 'lucide-react'
import { formatDateTime, formatAgo } from '#/lib/format-time'
import { orpc } from '#/lib/orpc'
import { client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { PipelineProgress } from '#/components/PipelineProgress'
import { PanelHeader } from '#/components/pipeline/run-gantt'
import { fmtDur } from '#/components/pipeline/step-spine'
import { BranchLabel, ShortSha } from '#/components/GitRef'
import { StatusBadge, StatusIcon, TriggerIcon, fmtSecs, triggerLabel } from '#/components/run-detail/run-status'

export function RunHeader({ run, isLive, elapsedSecs }: { run: any; isLive: boolean; elapsedSecs: number }) {
  const invalidate = [orpc.runs.get.key({ input: { id: run.id } }), orpc.runs.list.key()]
  const cancel = useAction((id: string) => client.runs.cancel({ runId: id }), { invalidate })
  const retry = useAction((id: string) => client.runs.retry({ runId: id }), { invalidate })
  const pause = useAction((id: string) => client.runs.pause({ runId: id }), { invalidate })
  const resume = useAction((id: string) => client.runs.resume({ runId: id }), { invalidate })
  const rerunFailed = useAction((id: string) => client.runs.rerunFailed({ runId: id }), { invalidate })
  return (
    <div className="island-shell p-4 sm:p-5 mb-4 lg:mb-5">
      <div className="flex flex-wrap items-center gap-2 sm:gap-3">
        <StatusBadge status={run.status} />
        <Link
          to="/ci/projects/$id"
          params={{ id: run.projectId }}
          className="display-title text-lg sm:text-xl lg:text-2xl font-bold text-foreground hover:text-primary transition-colors truncate"
        >
          {run.projectName}
        </Link>
        <span className="hidden sm:inline text-muted-foreground/30">/</span>
        <p className="text-sm text-muted-foreground flex items-center gap-1.5 min-w-0 flex-1">
          <GitCommit size={13} className="shrink-0 opacity-60" />
          <span className="truncate">{run.commitMessage}</span>
        </p>
        <span className="hidden lg:inline text-xs text-muted-foreground font-mono opacity-50 shrink-0">
          {run.workflowFile}
        </span>
      </div>

      <div className="flex flex-wrap items-center gap-3 sm:gap-5 mt-3 text-xs text-muted-foreground">
        <BranchLabel branch={run.branch} max="22rem" className="text-xs" />
        <span className="flex items-center gap-1.5 shrink-0">
          <GitCommit size={13} />
          <ShortSha sha={run.commitSha} />
        </span>
        <span className="flex items-center gap-1.5 min-w-0 max-w-[12rem]" title={triggerLabel(run.triggerType)}>
          <TriggerIcon type={run.triggerType} />
          <span className="truncate" title={run.triggeredBy}>{run.triggeredBy}</span>
        </span>
        <span className={`flex items-center gap-1.5 ${isLive ? 'text-primary font-medium' : ''}`}>
          <Timer size={13} className={isLive ? 'animate-pulse' : ''} />
          {isLive ? `${fmtSecs(elapsedSecs)} elapsed` : run.duration}
        </span>
        <span className="flex items-center gap-1.5" title="Started">
          <Clock size={13} />
          Started {run.startedAtTs ? formatDateTime(run.startedAtTs) : run.startedAt}
          {run.startedAtTs && <span className="opacity-50">· {formatAgo(run.startedAtTs)}</span>}
        </span>
        {run.finishedAtTs && (
          <span className="flex items-center gap-1.5">
            <CheckCircle size={13} />
            Ended {formatDateTime(run.finishedAtTs)}
          </span>
        )}
        <RunCostChip runId={run.id} live={isLive} />

        <div className="flex items-center gap-2 ml-auto">
          {run.status === 'running' && (
            <button
              type="button"
              onClick={() => pause.mutate(run.id)}
              disabled={pause.isPending}
              className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:border-foreground transition-colors disabled:opacity-50"
            >
              {pause.isPending ? <Loader2 size={12} className="animate-spin" /> : <Pause size={12} />}
              {pause.isPending ? 'Pausing…' : 'Pause'}
            </button>
          )}
          {run.status === 'paused' && (
            <button
              type="button"
              onClick={() => resume.mutate(run.id)}
              disabled={resume.isPending}
              className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-primary hover:border-primary transition-colors disabled:opacity-50"
            >
              {resume.isPending ? <Loader2 size={12} className="animate-spin" /> : <ArrowRight size={12} />}
              {resume.isPending ? 'Resuming…' : 'Resume'}
            </button>
          )}
          {(run.status === 'running' || run.status === 'pending' || run.status === 'paused') && (
            <button
              type="button"
              onClick={() => cancel.mutate(run.id)}
              disabled={cancel.isPending}
              className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-destructive hover:border-destructive transition-colors disabled:opacity-50"
            >
              {cancel.isPending ? <Loader2 size={12} className="animate-spin" /> : <Ban size={12} />}
              {cancel.isPending ? 'Cancelling…' : 'Cancel'}
            </button>
          )}
          {(run.status === 'failed' || run.status === 'cancelled') && (
            <>
              <button
                type="button"
                onClick={() => rerunFailed.mutate(run.id)}
                disabled={rerunFailed.isPending}
                className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-primary hover:border-primary transition-colors disabled:opacity-50"
              >
                {rerunFailed.isPending ? <Loader2 size={12} className="animate-spin" /> : <RotateCcw size={12} />}
                {rerunFailed.isPending ? 'Re-running…' : 'Re-run failed'}
              </button>
              <button
                type="button"
                onClick={() => retry.mutate(run.id)}
                disabled={retry.isPending}
                className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-primary hover:border-primary transition-colors disabled:opacity-50"
              >
                {retry.isPending ? <Loader2 size={12} className="animate-spin" /> : <RotateCcw size={12} />}
                {retry.isPending ? 'Retrying…' : 'Re-run all'}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Run cost — requested compute × wall-clock with a $ estimate. Flint schedules
// the pods, so it knows exactly what each step asked for and for how long.
export function RunCostChip({ runId, live }: { runId: string; live: boolean }) {
  const { data } = useQuery({
    ...orpc.runs.cost.queryOptions({ input: { runId } }),
    refetchInterval: live ? 10_000 : false,
  })
  if (!data || data.totalCoreSecs <= 0) return null
  const usd = data.estimatedUsd
  const money = usd >= 0.01 ? `$${usd.toFixed(2)}` : `$${usd.toFixed(4)}`
  return (
    <span
      className="flex items-center gap-1.5"
      title={`${data.totalCoreSecs.toFixed(0)} core-seconds · ${data.totalGbSecs.toFixed(0)} GB-seconds (requested compute × duration, at $${data.rates.cpuCoreHourUsd}/core-hr)`}
    >
      <span className="font-mono">≈ {money}</span>
      <span className="opacity-50">{data.totalCoreSecs.toFixed(0)} core-s</span>
    </span>
  )
}

// Run progress — segmented bar + live step/elapsed/ETA context while running
// ---------------------------------------------------------------------------

export function Stat({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-muted px-3 py-2.5">
      <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
        {icon}
        <span className="truncate min-w-0">{label}</span>
      </div>
      <p className="mt-1 text-lg font-bold tabular-nums text-foreground">{value}</p>
    </div>
  )
}

export const TERMINAL_STATUSES = ['succeeded', 'failed', 'skipped', 'cancelled']

export function RunProgress({ steps, isLive, elapsedSecs, medianSecs }: {
  steps: any[]
  isLive: boolean
  elapsedSecs: number
  medianSecs: number
}) {
  const total = steps.length
  const done = steps.filter((s) => TERMINAL_STATUSES.includes(s.status)).length
  const current = steps.find((s) => s.status === 'running') ?? steps.find((s) => s.status === 'waiting')
  const pct = total > 0 ? Math.round((done / total) * 100) : 0

  return (
    <div className="space-y-2">
      <PipelineProgress steps={steps} />
      {isLive && total > 0 && (
        <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-xs text-muted-foreground">
          <span className="text-foreground font-medium">Step {Math.min(done + 1, total)} of {total}</span>
          <span className="opacity-40">·</span>
          <span className="tabular-nums">{pct}%</span>
          {current && (
            <>
              <span className="opacity-40">·</span>
              <span className={`flex items-center gap-1 ${current.status === 'waiting' ? 'text-warning' : 'text-primary'}`}>
                {current.status === 'waiting'
                  ? <><Pause size={11} /> Awaiting approval:</>
                  : <><Loader2 size={11} className="animate-spin" /> Running:</>}
                <span className="font-mono">{current.name}</span>
              </span>
            </>
          )}
          <span className="opacity-40">·</span>
          <span className="flex items-center gap-1">
            <Timer size={11} />
            <span className="tabular-nums">{fmtSecs(elapsedSecs)}</span> elapsed
            {medianSecs > 0 && <span className="opacity-70"> · ~{fmtSecs(Math.round(medianSecs))} typical</span>}
          </span>
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Run summary — the detail pane when no step is selected (clean/pending runs)
// ---------------------------------------------------------------------------

export function RunSummary({ run, steps, isLive, now, onStepClick }: {
  run: any
  steps: any[]
  isLive: boolean
  now: number
  onStepClick: (name: string) => void
}) {
  const started = steps.filter((s) => s.startedAt)
  const passed = steps.filter((s) => s.status === 'succeeded').length
  const total = steps.length

  // Per-step run/queue time. Gates are human waits, not slow work, so they're
  // excluded from the "slowest steps" ranking and its scale.
  const timed = started
    .map((s) => {
      const startMs = Date.parse(s.startedAt)
      const schedMs = s.scheduledAt ? Date.parse(s.scheduledAt) : startMs
      const endMs = s.finishedAt ? Date.parse(s.finishedAt) : now
      return { step: s, runMs: Math.max(0, endMs - startMs), waitMs: Math.max(0, startMs - schedMs) }
    })
  const slowest = timed
    .filter((t) => t.step.execType !== 'gate')
    .sort((a, b) => b.runMs - a.runMs)
  const maxRun = Math.max(1, ...slowest.map((t) => t.runMs))
  const totalQueue = timed.reduce((sum, t) => sum + t.waitMs, 0)

  const wallMs = run.startedAtTs && run.finishedAtTs ? run.finishedAtTs - run.startedAtTs : 0

  return (
    <div className="island-shell !p-0 overflow-hidden">
      <PanelHeader icon={<Gauge size={14} className="text-primary" />} title="Run summary" />

      <div className="p-4 sm:p-5 space-y-5">
        {/* Headline stats */}
        <div className="grid grid-cols-3 gap-3">
          <Stat
            icon={<CheckCircle size={14} className={passed === total ? 'text-success' : 'text-muted-foreground'} />}
            label="Steps passed"
            value={`${passed}/${total}`}
          />
          <Stat
            icon={<Timer size={14} className={isLive ? 'text-primary' : 'text-muted-foreground'} />}
            label={isLive ? 'Elapsed' : 'Duration'}
            value={wallMs ? fmtDur(wallMs) : run.duration}
          />
          <Stat
            icon={<Hourglass size={14} className="text-muted-foreground" />}
            label="Queued"
            value={fmtDur(totalQueue)}
          />
        </div>

        {/* Where the time went */}
        {slowest.length > 0 && (
          <div className="space-y-2">
            <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground/60">
              Slowest steps
            </p>
            <div className="space-y-1.5">
              {slowest.slice(0, 5).map(({ step, runMs }) => (
                <button
                  key={step.name}
                  type="button"
                  onClick={() => onStepClick(step.name)}
                  className="group w-full flex items-center gap-3 text-left"
                >
                  <StatusIcon status={step.status} size={13} />
                  <span className="text-[13px] text-foreground/90 group-hover:text-primary transition-colors w-32 sm:w-40 truncate shrink-0">
                    {step.name}
                  </span>
                  <span className="flex-1 h-2 rounded-full bg-muted overflow-hidden">
                    <span
                      className={`block h-full rounded-full ${step.status === 'failed' ? 'bg-destructive' : 'bg-success'}`}
                      style={{ width: `${Math.max((runMs / maxRun) * 100, 4)}%` }}
                    />
                  </span>
                  <span className="text-[11px] font-mono tabular-nums text-muted-foreground w-12 text-right shrink-0">
                    {fmtDur(runMs)}
                  </span>
                </button>
              ))}
            </div>
          </div>
        )}

        <PlacementSection runId={run.id} live={isLive} />

        <p className="flex items-center gap-2 text-xs text-muted-foreground pt-1 border-t border-border">
          <ArrowRight size={12} className="text-muted-foreground/50" />
          Select a step to view its logs
        </p>
      </div>
    </div>
  )
}

// PlacementSection is decision transparency at run scope: which machine ran
// each step, at what price, and how long it queued. Absent for runs with no
// machine steps (http-only, sim).
export function PlacementSection({ runId, live }: { runId: string; live: boolean }) {
  const { data } = useQuery({
    ...orpc.runs.placement.queryOptions({ input: { runId } }),
    refetchInterval: live ? 5000 : false,
  })
  const placements = data?.placements ?? []
  if (placements.length === 0) return null

  return (
    <div className="space-y-2">
      <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground/60">
        Placement
      </p>
      <div className="space-y-1.5">
        {placements.map((p) => (
          <div key={`${p.stepName}-${p.attempt}`} className="flex items-center gap-3 text-[12px]">
            <Server size={12} className="text-muted-foreground/60 shrink-0" />
            <span className="text-foreground/90 w-32 sm:w-40 truncate shrink-0">{p.stepName}</span>
            <span className="font-mono text-muted-foreground truncate flex-1">
              {p.machineId
                ? `${p.machineId.slice(0, 8)}${p.instanceType ? ` · ${p.instanceType}` : ''}${p.capacityType === 'spot' ? ' · spot' : ''}`
                : p.status === 'pending' ? 'waiting for a machine…' : p.status}
            </span>
            {p.queueWaitMs != null && (
              <span className="text-[11px] font-mono tabular-nums text-muted-foreground shrink-0" title="queue wait">
                +{fmtDur(p.queueWaitMs)}
              </span>
            )}
            {p.pricePerHourUsd != null && (
              <span className="text-[11px] font-mono tabular-nums text-muted-foreground w-16 text-right shrink-0">
                ${p.pricePerHourUsd.toFixed(3)}/hr
              </span>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
