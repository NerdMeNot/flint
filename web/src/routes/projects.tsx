import { createFileRoute, Link } from '@tanstack/react-router'
import { CheckCircle, XCircle, Loader2, Clock, GitBranch, ExternalLink } from 'lucide-react'
import { mockProjects } from '#/lib/mock-data'

export const Route = createFileRoute('/projects')({
  component: ProjectsPage,
})

function ProjectsPage() {
  return (
    <div className="space-y-6 max-w-6xl rise-in">
      <div>
        <h1 className="display-title text-2xl text-foreground">Projects</h1>
        <p className="text-muted-foreground text-sm mt-1">
          {mockProjects.length} repositories connected
        </p>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {mockProjects.map((project, i) => (
          <div
            key={project.id}
            className="feature-card rise-in p-5 space-y-3"
            style={{ animationDelay: `${i * 50 + 30}ms` }}
          >
            <div className="flex items-start justify-between">
              <div className="flex items-center gap-2.5 min-w-0">
                <div className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: project.colour }} />
                <div className="min-w-0">
                  <h3 className="font-semibold text-sm text-foreground truncate">{project.name}</h3>
                  <p className="text-xs text-muted-foreground font-mono truncate">{project.repo}</p>
                </div>
              </div>
              <span className="island-kicker !text-[0.55rem] shrink-0 ml-2">{project.workspace}</span>
            </div>

            <div className="flex flex-wrap gap-1.5">
              {project.tags.map((tag) => (
                <span key={tag} className="rounded-md bg-secondary border border-border px-2 py-0.5 text-[0.65rem] font-medium text-muted-foreground">
                  {tag}
                </span>
              ))}
            </div>

            {project.lastRun ? (
              <Link
                to="/runs/$id"
                params={{ id: project.lastRun.id }}
                className="block -mx-5 -mb-5 px-5 py-2.5 border-t border-border hover:bg-accent transition-colors group"
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <StatusIcon status={project.lastRun.status} />
                    <span className="flex items-center gap-1 text-xs text-muted-foreground">
                      <GitBranch size={11} />
                      <span className="font-mono">{project.lastRun.branch}</span>
                    </span>
                  </div>
                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span>{project.lastRun.duration}</span>
                    <span className="opacity-50">{project.lastRun.startedAt}</span>
                    <ExternalLink size={11} className="opacity-0 group-hover:opacity-50 transition-opacity" />
                  </div>
                </div>
              </Link>
            ) : (
              <div className="-mx-5 -mb-5 px-5 py-2.5 border-t border-border">
                <span className="text-xs text-muted-foreground opacity-40">No runs yet</span>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

function StatusIcon({ status }: { status: string }) {
  switch (status) {
    case 'succeeded': return <CheckCircle size={13} className="text-success" />
    case 'failed': return <XCircle size={13} className="text-destructive" />
    case 'running': return <Loader2 size={13} className="text-primary animate-spin" />
    default: return <Clock size={13} className="text-muted-foreground" />
  }
}
