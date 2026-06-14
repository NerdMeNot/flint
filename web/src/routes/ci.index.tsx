import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import {
  Loader2,
  XCircle,
  Shield,
  AlertTriangle,
  TrendingUp,
  Clock,
  ArrowRight,
  CheckCircle2,
} from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { RunRow } from '#/components/RunRow'
import { ProjectHealthBar, RunSparkline } from '#/components/ProjectHealth'
import { useScope } from '#/lib/scope-context'
import { bucketOf } from '#/lib/run-feed'
import { needsAttention } from '#/lib/project-health'
import type { Project, RunStatusValue } from '#/lib/api/types'

export const Route = createFileRoute('/ci/')({
  component: DashboardPage,
})

function DashboardPage() {
  const { workspaceMatches } = useScope()
  const { data: stats } = useSuspenseQuery(orpc.stats.get.queryOptions())
  const { data: runsData } = useSuspenseQuery(
    orpc.runs.list.queryOptions({ input: { limit: 20 } }),
  )
  const { data: projectsData } = useSuspenseQuery(
    orpc.projects.list.queryOptions({ input: { limit: 100 } }),
  )

  // Runs carry no workspace of their own — resolve it via their project, then
  // apply the active workspace scope (environment stays a local run filter).
  const projectWorkspace = new Map(projectsData.items.map((p) => [p.id, p.workspace]))
  const runs = runsData.items.filter((r) => workspaceMatches(projectWorkspace.get(r.projectId)))
  const projects = projectsData.items.filter((p) => workspaceMatches(p.workspace))

  // "What needs me right now" — derived from recent runs (running/failed are
  // recency-bound) and project health; counts deep-link into the filtered feeds.
  const running = runs.filter((r) => r.status === 'running').length
  const failedToday = runs.filter((r) => r.status === 'failed' && bucketOf(r.startedAt) === 'Today').length
  const attentionProjects = projects.filter((p) => needsAttention(p.health)).length

  // Projects worth watching: those needing attention, worst first.
  const watch = projects
    .filter((p) => needsAttention(p.health))
    .sort((a, b) =>
      Number(b.health!.failingNow) - Number(a.health!.failingNow) ||
      a.health!.passRate - b.health!.passRate)
    .slice(0, 5)

  // Org-wide recent success trend (newest-first → sparkline renders oldest-left).
  const recentStatuses: RunStatusValue[] = runs.slice(0, 16).map((r) => r.status as RunStatusValue)

  return (
    <div className="space-y-6 rise-in">
      <div>
        <h1 className="display-title text-3xl lg:text-4xl text-foreground">Dashboard</h1>
        <p className="text-muted-foreground text-sm lg:text-base mt-2">What needs your attention</p>
      </div>

      {/* Attention strip — actionable, each tile deep-links into the filtered view */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 lg:gap-4">
        <AttentionTile to="/ci/runs" search={{ status: 'running' }} tone="primary"
          icon={<Loader2 size={16} className={running > 0 ? 'animate-spin' : ''} />} label="Running now" count={running} />
        <AttentionTile to="/ci/runs" search={{ status: 'failed' }} tone="danger"
          icon={<XCircle size={16} />} label="Failed today" count={failedToday} />
        <AttentionTile to="/ci/gates" tone="warning"
          icon={<Shield size={16} />} label="Pending gates" count={stats.pendingGates} />
        <AttentionTile to="/ci/projects" search={{ attention: true }} tone="danger"
          icon={<AlertTriangle size={16} />} label="Needs attention" count={attentionProjects} />
      </div>

      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_320px]">
        {/* Recent activity */}
        <div className="island-shell !p-0 overflow-hidden min-w-0">
          <div className="flex items-center justify-between px-5 py-3.5 border-b border-border">
            <h2 className="font-semibold text-foreground">Recent activity</h2>
            <Link to="/ci/runs" className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-primary transition-colors">
              View all <ArrowRight size={11} />
            </Link>
          </div>
          {runs.length === 0 ? (
            <p className="px-5 py-10 text-center text-sm text-muted-foreground">No runs yet.</p>
          ) : (
            <div className="divide-y divide-border">
              {runs.slice(0, 8).map((run) => (
                <RunRow key={run.id} run={run} />
              ))}
            </div>
          )}
        </div>

        {/* Rail: health + projects to watch */}
        <div className="space-y-5">
          <div className="island-shell p-4 lg:p-5 space-y-3">
            <span className="text-xs font-semibold text-foreground">Pipeline health</span>
            <div className="flex items-baseline gap-2">
              <TrendingUp size={16} className="text-success self-center" />
              <span className="text-3xl font-bold tracking-tight text-foreground tabular-nums">{stats.successRate}%</span>
              <span className="text-xs text-muted-foreground">success rate</span>
            </div>
            {recentStatuses.length > 0 && <RunSparkline runs={recentStatuses} barClass="w-1.5 h-4" />}
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground pt-1">
              <Clock size={12} />
              <span className="font-medium text-foreground tabular-nums">{stats.runsToday}</span> runs today
            </div>
          </div>

          <ProjectsToWatch projects={watch} />
        </div>
      </div>
    </div>
  )
}

const TONE: Record<string, string> = {
  primary: 'text-primary',
  danger: 'text-destructive',
  warning: 'text-warning',
}

function AttentionTile({ to, search, icon, label, count, tone }: {
  to: string
  search?: Record<string, unknown>
  icon: React.ReactNode
  label: string
  count: number
  tone: 'primary' | 'danger' | 'warning'
}) {
  const active = count > 0
  const color = active ? TONE[tone] : 'text-muted-foreground'
  return (
    <Link
      to={to as never}
      search={search as never}
      className="island-shell !p-4 lg:!p-5 flex flex-col gap-2 hover:bg-accent/40 transition-colors group"
    >
      <div className="flex items-center gap-2">
        <span className={color}>{icon}</span>
        <span className="island-kicker !text-[11px]">{label}</span>
        <ArrowRight size={13} className="ml-auto text-muted-foreground opacity-0 group-hover:opacity-60 transition-opacity" />
      </div>
      <span className={`text-3xl lg:text-4xl font-bold tracking-tight tabular-nums ${active ? color : 'text-muted-foreground/40'}`}>
        {count}
      </span>
    </Link>
  )
}

function ProjectsToWatch({ projects }: { projects: Project[] }) {
  return (
    <div className="island-shell !p-0 overflow-hidden">
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-border">
        <span className="text-xs font-semibold text-foreground">Projects to watch</span>
        <Link to="/ci/projects" search={{ attention: true } as never} className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-primary transition-colors">
          All projects <ArrowRight size={11} />
        </Link>
      </div>
      {projects.length === 0 ? (
        <div className="flex items-center gap-2 px-4 py-6 text-sm text-muted-foreground justify-center">
          <CheckCircle2 size={16} className="text-success" /> All projects healthy
        </div>
      ) : (
        <div className="divide-y divide-border">
          {projects.map((p) => (
            <Link
              key={p.id}
              to="/ci/projects/$id"
              params={{ id: p.id }}
              className="flex items-center gap-2.5 px-4 py-2.5 hover:bg-accent/50 transition-colors group"
            >
              <span className="w-1.5 h-1.5 rounded-full shrink-0" style={{ backgroundColor: p.colour }} />
              <span className="text-xs font-medium text-foreground truncate group-hover:text-primary transition-colors flex-1 min-w-0">{p.name}</span>
              <ProjectHealthBar health={p.health} />
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}
