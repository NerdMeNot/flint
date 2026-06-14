import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Sparkles, Bookmark, Trash2, ArrowRight, ScrollText, FolderGit2 } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useScope } from '#/lib/scope-context'
import { useAction } from '#/hooks/use-action'
import { BackLink } from '#/components/BackLink'
import { RunRow } from '#/components/RunRow'
import { ProjectCard, sortProjects } from '#/routes/ci.projects.index'
import { smartViewById, sourceOf, describeSelector } from '#/lib/views'
import { groupByBucket, parseDurationToSeconds, median } from '#/lib/run-feed'
import type { ProjectSort } from '#/routes/ci.projects.index'

export const Route = createFileRoute('/ci/views/$id')({
  component: ViewPage,
})

function ViewPage() {
  const { id } = Route.useParams()
  const navigate = Route.useNavigate()
  const { data: viewsData } = useSuspenseQuery(orpc.views.list.queryOptions({}))

  const smart = smartViewById(id)
  const saved = viewsData.items.find((v) => v.id === id)
  const view = smart ?? saved
  const isSmart = !!smart

  const del = useAction((vid: string) => client.views.delete({ id: vid }), {
    invalidate: [orpc.views.list.key()],
    onSuccess: () => navigate({ to: '/ci/projects' }),
  })

  if (!view) {
    return (
      <div className="rise-in space-y-5">
        <BackLink fallbackTo="/ci/projects" label="Back" />
        <div className="island-shell p-12 text-center text-sm text-muted-foreground">View not found.</div>
      </div>
    )
  }

  const source = sourceOf(view.route)
  const chips = describeSelector(view.search)

  return (
    <div className="rise-in space-y-5">
      <BackLink fallbackTo="/ci/projects" label="Back" />

      <div className="island-shell p-4 sm:p-5 lg:p-6 space-y-3">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="flex items-center gap-2.5">
              {isSmart
                ? <Sparkles size={18} className="text-primary shrink-0" />
                : <Bookmark size={18} className="text-primary shrink-0" />}
              <h1 className="display-title text-2xl lg:text-3xl font-bold text-foreground truncate">{view.name}</h1>
              <span className="island-kicker !text-[11px] shrink-0">{isSmart ? 'Smart view' : 'Saved view'}</span>
            </div>
            {chips.length > 0 && (
              <div className="flex flex-wrap items-center gap-1.5 mt-2.5">
                {chips.map((c) => (
                  <span key={c.key} className="rounded-md border border-border px-2 py-0.5 text-[11px] font-mono text-muted-foreground">
                    {c.label}
                  </span>
                ))}
              </div>
            )}
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <Link
              to={view.route as never}
              search={view.search as never}
              className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
            >
              {source === 'runs' ? <ScrollText size={12} /> : <FolderGit2 size={12} />}
              Open in {source === 'runs' ? 'Runs' : 'Projects'}
              <ArrowRight size={12} />
            </Link>
            {!isSmart && (
              <button
                type="button"
                onClick={() => del.mutate(view.id)}
                title="Delete view"
                className="flex items-center justify-center w-8 h-8 rounded-lg border border-border text-muted-foreground hover:text-destructive hover:border-destructive/30 transition-colors"
              >
                <Trash2 size={13} />
              </button>
            )}
          </div>
        </div>
      </div>

      {source === 'runs' ? (
        <RunsResults search={view.search} />
      ) : source === 'projects' ? (
        <ProjectsResults search={view.search} />
      ) : (
        <div className="island-shell p-12 text-center text-sm text-muted-foreground">
          Unsupported view source.
        </div>
      )}
    </div>
  )
}

// Mirrors the Runs page's scope/env filtering + time grouping, for a fixed
// selector (no editable filter bar — this is a view, not the live list).
function RunsResults({ search }: { search: Record<string, unknown> }) {
  const { workspaceMatches } = useScope()
  const { data } = useSuspenseQuery(
    orpc.runs.list.queryOptions({
      input: {
        status: search.status as never,
        projectId: search.project as string | undefined,
        limit: 50,
      },
    }),
  )
  const { data: projectsData } = useSuspenseQuery(orpc.projects.list.queryOptions({ input: { limit: 100 } }))
  const projectWorkspace = new Map(projectsData.items.map((p) => [p.id, p.workspace]))
  const env = search.env as string | undefined

  const items = data.items.filter(
    (r) => workspaceMatches(projectWorkspace.get(r.projectId)) && (!env || r.environment === env),
  )

  const baselineByProject = (() => {
    const buckets = new Map<string, number[]>()
    for (const r of data.items) {
      if (r.status !== 'succeeded' && r.status !== 'failed') continue
      const secs = parseDurationToSeconds(r.duration)
      if (secs <= 0) continue
      const arr = buckets.get(r.projectId) ?? []
      arr.push(secs)
      buckets.set(r.projectId, arr)
    }
    const out = new Map<string, number>()
    for (const [pid, secs] of buckets) out.set(pid, median(secs))
    return out
  })()

  const grouped = groupByBucket(items)

  if (items.length === 0) {
    return <div className="island-shell p-12 text-center text-sm text-muted-foreground">No runs match this view.</div>
  }

  return (
    <div className="island-shell !p-0 overflow-hidden">
      {grouped.map(({ bucket, runs }) => (
        <div key={bucket}>
          <div className="flex items-center gap-2 px-4 lg:px-5 py-2 bg-accent/20 border-b border-border text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/70">
            {bucket}
            <span className="rounded-full bg-border/70 px-1.5 py-0.5 text-[10px] font-bold leading-none text-muted-foreground">{runs.length}</span>
          </div>
          <div className="divide-y divide-border">
            {runs.map((run) => (
              <RunRow key={run.id} run={run} baselineSecs={baselineByProject.get(run.projectId)} />
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

// Mirrors the Projects page's tag/needs-grouping filter + search + sort, for a
// fixed selector.
function ProjectsResults({ search }: { search: Record<string, unknown> }) {
  const { workspaces } = useScope()
  const wsScope = workspaces.length > 0 ? workspaces : undefined
  const tags = Array.isArray(search.tags) ? (search.tags as string[]) : undefined

  const { data } = useSuspenseQuery(
    orpc.projects.list.queryOptions({
      input: {
        workspace: wsScope,
        tags,
        needsGrouping: search.needsGrouping ? true : undefined,
        limit: 100,
      },
    }),
  )
  const { data: regData } = useSuspenseQuery(orpc.tags.registry.list.queryOptions({ input: {} }))
  const registry = new Map(regData.items.map((k) => [k.key, k]))

  const q = typeof search.q === 'string' ? search.q.toLowerCase().trim() : ''
  const filtered = q
    ? data.items.filter(
        (p) => p.name.toLowerCase().includes(q) || p.repo.toLowerCase().includes(q) || p.tags.some((t) => t.toLowerCase().includes(q)),
      )
    : data.items
  const projects = sortProjects(filtered, (search.sort as ProjectSort) ?? 'recent')

  if (projects.length === 0) {
    return <div className="island-shell p-12 text-center text-sm text-muted-foreground">No projects match this view.</div>
  }

  return (
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
      {projects.map((project, i) => (
        <ProjectCard key={project.id} project={project} registry={registry} index={i} />
      ))}
    </div>
  )
}
