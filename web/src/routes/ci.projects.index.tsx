import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import { CheckCircle, XCircle, Loader2, Clock, GitBranch, ExternalLink, Search, X, Tag, AlertTriangle, LayoutGrid, List } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { useScope } from '#/lib/scope-context'
import { Pagination } from '#/components/Pagination'
import { FilterPill } from '#/components/FilterPill'
import { TagChip } from '#/components/TagChip'
import type { TagKey, Project } from '#/lib/api/types'
import { useCursorPagination } from '#/hooks/use-cursor-pagination'

type ProjectView = 'grid' | 'list'

const PROJECT_PAGE_SIZE = 12

export const Route = createFileRoute('/ci/projects/')({
  component: ProjectsPage,
})

function ProjectsPage() {
  const { workspaces } = useScope()
  const { page, cursor, goToPage, reset } = useCursorPagination()
  const [search, setSearch] = useState('')
  const [selectedTags, setSelectedTags] = useState<string[]>([])
  const [needsGrouping, setNeedsGrouping] = useState(false)
  const [view, setView] = useState<ProjectView>('grid')

  // Persist the grid/list preference (SSR-safe).
  useEffect(() => {
    if (typeof window === 'undefined') return
    const saved = localStorage.getItem('flint-projects-view')
    if (saved === 'grid' || saved === 'list') setView(saved)
  }, [])
  useEffect(() => {
    if (typeof window === 'undefined') return
    localStorage.setItem('flint-projects-view', view)
  }, [view])

  const wsScope = workspaces.length > 0 ? workspaces : undefined

  const { data: projectsData } = useSuspenseQuery(
    orpc.projects.list.queryOptions({
      input: {
        workspace: wsScope,
        tags: selectedTags.length > 0 ? selectedTags : undefined,
        needsGrouping: needsGrouping || undefined,
        limit: PROJECT_PAGE_SIZE,
        cursor,
      },
    }),
  )

  // Unfiltered-by-tag, workspace-scoped fetch supplies the stable universe of
  // tag options so the pill doesn't collapse as the user narrows the filter.
  const { data: tagUniverse } = useSuspenseQuery(
    orpc.projects.list.queryOptions({
      input: { workspace: wsScope, limit: 100 },
    }),
  )
  const allTags = [...new Set(tagUniverse.items.flatMap((p) => p.tags ?? []))].sort()

  // Registry → structured, colored tag chips.
  const { data: regData } = useSuspenseQuery(orpc.tags.registry.list.queryOptions({ input: {} }))
  const registry = new Map(regData.items.map((k) => [k.key, k]))
  // Stable count of projects that need grouping: workspace inferred (not
  // declared) or no tags — derived from the unfiltered, workspace-scoped set.
  const needsGroupingCount = tagUniverse.items.filter((p) => p.inferred || (p.tags ?? []).length === 0).length

  const toggleTag = (t: string) => {
    setSelectedTags((prev) => (prev.includes(t) ? prev.filter((x) => x !== t) : [...prev, t]))
    reset()
  }

  const query = search.toLowerCase().trim()
  const projects = query
    ? projectsData.items.filter(
        (p) =>
          p.name.toLowerCase().includes(query) ||
          p.repo.toLowerCase().includes(query) ||
          p.tags.some((t) => t.toLowerCase().includes(query)),
      )
    : projectsData.items
  const hasMore = !!projectsData.nextCursor

  // Subline: "in production", "in production, staging", "in production +2"
  const wsLabel = workspaces.length === 0
    ? ' connected'
    : workspaces.length === 1
      ? ` in ${workspaces[0]}`
      : workspaces.length === 2
        ? ` in ${workspaces[0]}, ${workspaces[1]}`
        : ` in ${workspaces[0]} +${workspaces.length - 1}`

  return (
    <div className="space-y-6 rise-in">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="display-title text-3xl lg:text-4xl text-foreground">Projects</h1>
          <p className="text-muted-foreground text-sm lg:text-base mt-2">
            {projects.length} repositories{wsLabel}
          </p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <div className="relative w-56 lg:w-72">
            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
            <input
              type="text"
              value={search}
              onChange={(e) => { setSearch(e.target.value); reset() }}
              placeholder="Search projects..."
              className="w-full pl-8 pr-8 py-2 text-sm rounded-lg border border-border bg-transparent text-foreground placeholder:text-muted-foreground/60 focus:outline-none focus:ring-1 focus:ring-ring/40 transition-colors"
            />
            {search && (
              <button
                type="button"
                onClick={() => setSearch('')}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
              >
                <X size={13} />
              </button>
            )}
          </div>
          <div className="inline-flex items-center rounded-lg border border-border p-0.5">
            {([['grid', LayoutGrid, 'Grid view'], ['list', List, 'List view']] as const).map(([v, Icon, label]) => (
              <button
                key={v}
                type="button"
                onClick={() => setView(v)}
                title={label}
                aria-label={label}
                aria-pressed={view === v}
                className={`flex items-center justify-center w-7 h-7 rounded-md transition-colors ${
                  view === v ? 'bg-accent text-foreground' : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                <Icon size={15} />
              </button>
            ))}
          </div>
        </div>
      </div>

      {(allTags.length > 0 || needsGroupingCount > 0) && (
        <div className="flex flex-wrap items-center gap-1.5">
          {needsGroupingCount > 0 && (
            <button
              type="button"
              onClick={() => { setNeedsGrouping((v) => !v); reset() }}
              title="Projects whose workspace was inferred (not declared) or that have no tags"
              className={`flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-xs font-medium transition-colors ${
                needsGrouping ? 'border-warning/40 bg-warning/10 text-warning' : 'border-border text-muted-foreground hover:text-foreground'
              }`}
            >
              <AlertTriangle size={12} />
              Needs grouping
              <span className="rounded-full bg-warning/20 text-warning text-[10px] font-bold leading-none px-1.5 py-0.5">{needsGroupingCount}</span>
            </button>
          )}
          {allTags.length > 0 && (
          <FilterPill
            icon={<Tag size={12} />}
            label={selectedTags.length > 0 ? `${selectedTags.length} tag${selectedTags.length > 1 ? 's' : ''}` : 'Tags'}
            active={selectedTags.length > 0}
            onClear={() => { setSelectedTags([]); reset() }}
            items={allTags.map((t) => ({ key: t, label: t, active: selectedTags.includes(t) }))}
            onSelect={toggleTag}
          />
          )}
          {selectedTags.map((t) => (
            <button
              key={t}
              type="button"
              onClick={() => toggleTag(t)}
              className="flex items-center gap-1 rounded-md bg-primary/5 border border-primary/30 px-2 py-1 text-xs font-medium text-primary hover:bg-primary/10 transition-colors"
            >
              {t}
              <X size={11} />
            </button>
          ))}
        </div>
      )}

      {view === 'grid' ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
          {projects.map((project, i) => (
            <ProjectCard key={project.id} project={project} registry={registry} index={i} />
          ))}
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          {projects.map((project, i) => (
            <ProjectRow key={project.id} project={project} registry={registry} index={i} />
          ))}
        </div>
      )}

      {projects.length > 0 && (
        <Pagination
          page={page}
          hasMore={hasMore}
          onPageChange={(p) => goToPage(p, projectsData.nextCursor)}
          count={projects.length}
          pageSize={PROJECT_PAGE_SIZE}
          label="projects"
        />
      )}
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

// Bounded tag display for index surfaces: at most `max` chips + a "+N" badge, so
// an open-ended tag set can never drive card/row geometry. Full set lives on the
// project detail page.
function TagSummary({ tags, registry, max }: { tags: string[]; registry: Map<string, TagKey>; max: number }) {
  if (tags.length === 0) {
    return (
      <span className="inline-flex items-center gap-1 text-[12px] text-muted-foreground/60">
        <Tag size={11} /> untagged
      </span>
    )
  }
  const overflow = tags.length - max
  return (
    <>
      {tags.slice(0, max).map((tag) => (
        <TagChip key={tag} tag={tag} registry={registry} />
      ))}
      {overflow > 0 && (
        <span className="inline-flex items-center rounded-md border border-border px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground">
          +{overflow}
        </span>
      )}
    </>
  )
}

function ProjectCard({ project, registry, index }: { project: Project; registry: Map<string, TagKey>; index: number }) {
  return (
    <div className="feature-card rise-in overflow-hidden flex flex-col" style={{ animationDelay: `${index * 50 + 30}ms` }}>
      <Link
        to="/ci/projects/$id"
        params={{ id: project.id }}
        className="block p-5 space-y-3 hover:bg-accent/50 transition-colors group flex-1"
      >
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-2.5 min-w-0">
            <div className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: project.colour }} />
            <div className="min-w-0">
              <h3 className="font-semibold text-sm text-foreground group-hover:text-primary transition-colors truncate">{project.name}</h3>
              <p className="text-xs text-muted-foreground font-mono truncate">{project.repo}</p>
            </div>
          </div>
          <span className="island-kicker !text-[11px] shrink-0 ml-2">{project.workspace}</span>
        </div>

        {/* Fixed-height tag row keeps every card the same height regardless of tag count. */}
        <div className="flex items-center gap-1.5 h-6 overflow-hidden">
          <TagSummary tags={project.tags} registry={registry} max={2} />
        </div>
      </Link>

      {project.lastRun ? (
        <Link
          to="/ci/runs/$id"
          params={{ id: project.lastRun.id }}
          className="block px-5 py-2.5 border-t border-border hover:bg-accent transition-colors group"
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
        <div className="px-5 py-2.5 border-t border-border">
          <span className="text-xs text-muted-foreground opacity-40">No runs yet</span>
        </div>
      )}
    </div>
  )
}

function ProjectRow({ project, registry, index }: { project: Project; registry: Map<string, TagKey>; index: number }) {
  return (
    <div className="feature-card rise-in flex items-center overflow-hidden" style={{ animationDelay: `${index * 25 + 20}ms` }}>
      <Link
        to="/ci/projects/$id"
        params={{ id: project.id }}
        className="flex items-center gap-3 min-w-0 flex-1 px-4 py-3 hover:bg-accent/50 transition-colors group"
      >
        <div className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: project.colour }} />
        <h3 className="font-semibold text-sm text-foreground group-hover:text-primary transition-colors truncate shrink-0 max-w-[14rem]">{project.name}</h3>
        <span className="text-xs text-muted-foreground font-mono truncate hidden sm:inline">{project.repo}</span>
        <span className="island-kicker !text-[11px] shrink-0 ml-auto">{project.workspace}</span>
        <div className="hidden lg:flex items-center gap-1.5 shrink-0 overflow-hidden max-w-[22rem]">
          <TagSummary tags={project.tags} registry={registry} max={3} />
        </div>
      </Link>

      {project.lastRun ? (
        <Link
          to="/ci/runs/$id"
          params={{ id: project.lastRun.id }}
          className="flex items-center gap-2 shrink-0 px-4 py-3 border-l border-border text-xs text-muted-foreground hover:bg-accent hover:text-foreground transition-colors group"
        >
          <StatusIcon status={project.lastRun.status} />
          <span className="hidden md:flex items-center gap-1">
            <GitBranch size={11} />
            <span className="font-mono">{project.lastRun.branch}</span>
          </span>
          <span className="opacity-50">{project.lastRun.startedAt}</span>
          <ExternalLink size={11} className="opacity-0 group-hover:opacity-50 transition-opacity" />
        </Link>
      ) : (
        <span className="shrink-0 px-4 py-3 border-l border-border text-xs text-muted-foreground opacity-40">No runs yet</span>
      )}
    </div>
  )
}
