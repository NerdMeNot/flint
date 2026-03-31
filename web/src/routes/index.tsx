import { createFileRoute, Link } from '@tanstack/react-router'
import {
  Activity,
  TrendingUp,
  Shield,
  FolderGit2,
  Clock,
  Timer,
  GitBranch,
  CheckCircle,
  XCircle,
  Loader2,
  GitCommit,
  Ban,
} from 'lucide-react'
import { dashboardStats, mockRuns } from '#/lib/mock-data'

export const Route = createFileRoute('/')({
  component: DashboardPage,
})

function DashboardPage() {
  return (
    <div className="space-y-8 max-w-6xl rise-in">
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
        <StatCard icon={<Activity size={16} />} label="Total Runs" value={dashboardStats.totalRuns.toLocaleString()} />
        <StatCard icon={<TrendingUp size={16} />} label="Success Rate" value={`${dashboardStats.successRate}%`} accent />
        <StatCard icon={<Shield size={16} />} label="Pending Gates" value={String(dashboardStats.pendingGates)} warning={dashboardStats.pendingGates > 0} />
        <StatCard icon={<FolderGit2 size={16} />} label="Active Projects" value={String(dashboardStats.activeProjects)} />
        <StatCard icon={<Clock size={16} />} label="Runs Today" value={String(dashboardStats.runsToday)} />
        <StatCard icon={<Timer size={16} />} label="Avg Duration" value={dashboardStats.avgDuration} />
      </div>

      {/* Recent Runs */}
      <div className="island-shell !p-0 overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3.5 border-b border-border">
          <h2 className="font-semibold text-foreground text-sm">Recent Pipeline Runs</h2>
          <span className="island-kicker">Live</span>
        </div>

        <div className="divide-y divide-border">
          {mockRuns.map((run) => (
            <Link
              key={run.id}
              to="/runs/$id"
              params={{ id: run.id }}
              className="flex items-center gap-4 px-5 py-3 hover:bg-accent transition-colors group"
            >
              <RunStatusIcon status={run.status} />

              <div className="w-1 h-7 rounded-full shrink-0 opacity-50" style={{ backgroundColor: run.projectColour }} />

              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-medium text-sm text-foreground group-hover:text-primary transition-colors">{run.projectName}</span>
                  <span className="text-xs text-muted-foreground font-mono opacity-60">{run.workflowFile}</span>
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

function RunStatusIcon({ status }: { status: string }) {
  switch (status) {
    case 'succeeded': return <CheckCircle size={16} className="text-success shrink-0" />
    case 'failed': return <XCircle size={16} className="text-destructive shrink-0" />
    case 'running': return <Loader2 size={16} className="text-primary shrink-0 animate-spin" />
    case 'cancelled': return <Ban size={16} className="text-muted-foreground shrink-0" />
    default: return <Clock size={16} className="text-muted-foreground shrink-0" />
  }
}
