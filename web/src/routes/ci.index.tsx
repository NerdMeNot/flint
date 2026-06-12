import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import {
  Activity,
  TrendingUp,
  Shield,
  FolderGit2,
  Clock,
  Timer,
} from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { RunRow } from '#/components/RunRow'
import { useScope } from '#/lib/scope-context'

export const Route = createFileRoute('/ci/')({
  component: DashboardPage,
})

function DashboardPage() {
  const { workspaceMatches, environmentMatches } = useScope()
  const { data: stats } = useSuspenseQuery(orpc.stats.get.queryOptions())
  const { data: runsData } = useSuspenseQuery(
    orpc.runs.list.queryOptions({ input: { limit: 20 } }),
  )
  const { data: projectsData } = useSuspenseQuery(
    orpc.projects.list.queryOptions({ input: { limit: 100 } }),
  )

  // Resolve each run's workspace from its project (runs carry no workspace of
  // their own), then apply the active workspace/environment scope. Server-side
  // filtering would make this unnecessary, but the join is real data, not a map.
  const projectWorkspace = new Map(projectsData.items.map((p) => [p.id, p.workspace]))
  const runs = runsData.items.filter((r) => {
    if (!workspaceMatches(projectWorkspace.get(r.projectId))) return false
    if (!environmentMatches(r.environment)) return false
    return true
  }).slice(0, 10)

  return (
    <div className="space-y-8 rise-in">
      <div>
        <h1 className="display-title text-3xl lg:text-4xl text-foreground">
          Dashboard
        </h1>
        <p className="text-muted-foreground text-sm lg:text-base mt-2">
          Overview of your CI pipelines
        </p>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-3 lg:gap-4">
        <StatCard icon={<Activity size={16} />} label="Total Runs" value={stats.totalRuns.toLocaleString()} />
        <StatCard icon={<TrendingUp size={16} />} label="Success Rate" value={`${stats.successRate}%`} accent />
        <StatCard icon={<Shield size={16} />} label="Pending Gates" value={String(stats.pendingGates)} warning={stats.pendingGates > 0} />
        <StatCard icon={<FolderGit2 size={16} />} label="Active Projects" value={String(stats.activeProjects)} />
        <StatCard icon={<Clock size={16} />} label="Runs Today" value={String(stats.runsToday)} />
        <StatCard icon={<Timer size={16} />} label="Avg Duration" value={stats.avgDuration} />
      </div>

      {/* Recent Runs */}
      <div className="island-shell !p-0 overflow-hidden">
        <div className="flex items-center justify-between px-5 lg:px-6 py-3.5 lg:py-4 border-b border-border">
          <h2 className="font-semibold text-foreground text-base lg:text-lg">Recent Pipeline Runs</h2>
          <span className="island-kicker">Live</span>
        </div>

        <div className="divide-y divide-border">
          {runs.map((run) => (
            <RunRow key={run.id} run={run} />
          ))}
        </div>
      </div>
    </div>
  )
}

function StatCard({
  icon, label, value, accent, warning,
}: {
  icon: React.ReactNode; label: string; value: string; accent?: boolean; warning?: boolean
}) {
  return (
    <div className="island-shell !p-4 lg:!p-5 space-y-2 lg:space-y-3">
      <div className="flex items-center gap-2">
        <span className={warning ? 'text-warning' : accent ? 'text-success' : 'text-muted-foreground'}>{icon}</span>
        <span className="island-kicker !text-[11px]">{label}</span>
      </div>
      <span className={`block text-2xl lg:text-3xl xl:text-4xl font-bold tracking-tight ${warning ? 'text-warning' : accent ? 'text-success' : 'text-foreground'}`}>
        {value}
      </span>
    </div>
  )
}

