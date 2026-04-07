import { createFileRoute, Link } from '@tanstack/react-router'
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

export const Route = createFileRoute('/')({
  component: DashboardPage,
})

function DashboardPage() {
  const { workspace, environment } = useScope()
  const { data: stats } = useSuspenseQuery(orpc.stats.get.queryOptions())
  const { data: runsData } = useSuspenseQuery(
    orpc.runs.list.queryOptions({ input: { limit: 20 } }),
  )

  // Client-side scope filtering (real API would accept these as params)
  const runs = runsData.items.filter((r) => {
    if (workspace) {
      const wsMap: Record<string, string> = { 'p-1': 'production', 'p-2': 'production', 'p-3': 'staging', 'p-4': 'platform', 'p-5': 'production', 'p-6': 'platform' }
      if (wsMap[r.projectId] !== workspace) return false
    }
    if (environment && r.environment !== environment) return false
    return true
  }).slice(0, 10)

  return (
    <div className="space-y-8 rise-in">
      <div>
        <h1 className="display-title text-2xl text-foreground">
          Dashboard
        </h1>
        <p className="text-muted-foreground text-sm mt-1">
          Overview of your CI pipelines
        </p>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-3">
        <StatCard icon={<Activity size={16} />} label="Total Runs" value={stats.totalRuns.toLocaleString()} />
        <StatCard icon={<TrendingUp size={16} />} label="Success Rate" value={`${stats.successRate}%`} accent />
        <StatCard icon={<Shield size={16} />} label="Pending Gates" value={String(stats.pendingGates)} warning={stats.pendingGates > 0} />
        <StatCard icon={<FolderGit2 size={16} />} label="Active Projects" value={String(stats.activeProjects)} />
        <StatCard icon={<Clock size={16} />} label="Runs Today" value={String(stats.runsToday)} />
        <StatCard icon={<Timer size={16} />} label="Avg Duration" value={stats.avgDuration} />
      </div>

      {/* Recent Runs */}
      <div className="island-shell !p-0 overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3.5 border-b border-border">
          <h2 className="font-semibold text-foreground text-sm">Recent Pipeline Runs</h2>
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
    <div className="island-shell !p-4 space-y-2">
      <div className="flex items-center gap-2">
        <span className={warning ? 'text-warning' : accent ? 'text-success' : 'text-muted-foreground'}>{icon}</span>
        <span className="island-kicker !text-[0.6rem]">{label}</span>
      </div>
      <span className={`text-xl font-bold tracking-tight ${warning ? 'text-warning' : accent ? 'text-success' : 'text-foreground'}`}>
        {value}
      </span>
    </div>
  )
}

