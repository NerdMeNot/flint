import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import { CheckCircle, XCircle, Loader2, Clock, GitBranch, ExternalLink, Search, X, Tag, AlertTriangle, LayoutGrid, List, ArrowDownUp } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { useScope } from '#/lib/scope-context'
import { Pagination } from '#/components/Pagination'
import { FilterPill } from '#/components/FilterPill'
import { TagChip } from '#/components/TagChip'
import { TagManagerModal, type TagGroup } from '#/components/TagManagerModal'
import { ProjectHealthBar } from '#/components/ProjectHealth'
import { SaveViewButton } from '#/components/SaveViewButton'
import { needsAttention } from '#/lib/project-health'
import type { TagKey, Project } from '#/lib/api/types'
import { useCursorPagination } from '#/hooks/use-cursor-pagination'
import { relativeToMinutes } from '#/lib/run-feed'
import { formatTimelineCompactISO, formatExactISO } from '#/lib/format-time'

type ProjectView = 'grid' | 'list'
export type ProjectSort = 'recent' | 'failing' | 'flaky' | 'name'

const SORTS: { key: ProjectSort; label: string }[] = [
  { key: 'recent', label: 'Recent activity' },
  { key: 'failing', label: 'Failing first' },
  { key: 'flaky', label: 'Flakiest' },
  { key: 'name', label: 'Name' },
]

const PROJECT_PAGE_SIZE = 12

// Filters + sort live in the URL so they survive navigation (e.g. into a project
// and back) and are shareable/bookmarkable. View is a personal preference, kept
// in localStorage rather than the URL.
interface ProjectSearch {
  q?: string
  tags?: string[]
  needsGrouping?: boolean
  attention?: boolean
  sort?: ProjectSort
}

export const Route = createFileRoute('/ci/projects/')({
  validateSearch: (s: Record<string, unknown>): ProjectSearch => ({
    q: typeof s.q === 'string' && s.q ? s.q : undefined,
    tags: Array.isArray(s.tags) ? s.tags.filter((t): t is string => typeof t === 'string') : undefined,
    needsGrouping: s.needsGrouping === true || s.needsGrouping === 'true' ? true : undefined,
    attention: s.attention === true || s.attention === 'true' ? true : undefined,
    sort: SORTS.some((o) => o.key === s.sort) ? (s.sort as ProjectSort) : undefined,
  }),
  component: ProjectsPage,
})

// Number of pass→fail / fail→pass transitions in a run window (flakiness proxy).
function flakiness(runs: { recentRuns: string[] } | undefined): number {
  const r = runs?.recentRuns
  if (!r || r.length < 2) return 0
  let t = 0
  for (let i = 1; i < r.length; i++) if (r[i] !== r[i - 1]) t++
  return t
}

export function sortProjects(items: Project[], sort: ProjectSort): Project[] {
  const copy = [...items]
  switch (sort) {
    case 'name':
      return copy.sort((a, b) => a.name.localeCompare(b.name))
    case 'failing':
      // Failing-now first, then worst pass rate.
      return copy.sort((a, b) =>
        Number(b.health?.failingNow ?? false) - Number(a.health?.failingNow ?? false) ||
        (a.health?.passRate ?? 101) - (b.health?.passRate ?? 101))
    case 'flaky':
      return copy.sort((a, b) => flakiness(b.health) - flakiness(a.health) || (a.health?.passRate ?? 101) - (b.health?.passRate ?? 101))
    case 'recent':
    default:
      // Most recent last-run activity first; projects with no runs sink.
      return copy.sort((a, b) =>
        relativeToMinutes(a.lastRun?.startedAt ?? '') - relativeToMinutes(b.lastRun?.startedAt ?? ''))
  }
}

function ProjectsPage() {
  const { workspaces } = useScope()
  const { page, cursor, goToPage, reset } = useCursorPagination()
  const sp = Route.useSearch()
  const navigate = Route.useNavigate()
  const search = sp.q ?? ''
  const selectedTags = sp.tags ?? []
  const needsGrouping = sp.needsGrouping ?? false
  const attention = sp.attention ?? false
  const sort = sp.sort ?? 'recent'
  const [tagFilterOpen, setTagFilterOpen] = useState(false)
  const [view, setView] = useState<ProjectView>('grid')

  // Filter mutations write to the URL (replace: keystrokes/toggles don't each
  // add a history entry) and reset pagination.
  const setFilters = (patch: Partial<ProjectSearch>) => {
    navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true })
    reset()
  }
  const setSearch = (q: string) => setFilters({ q: q || undefined })
  const setSelectedTags = (tags: string[]) => setFilters({ tags: tags.length > 0 ? tags : undefined })
  const setNeedsGrouping = (on: boolean) => setFilters({ needsGrouping: on || undefined })
  const setAttention = (on: boolean) => setFilters({ attention: on || undefined })
  const setSort = (s: ProjectSort) => setFilters({ sort: s === 'recent' ? undefined : s })

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
        attention: attention || undefined,
        limit: PROJECT_PAGE_SIZE,
        cursor,
      },
    }),
  )

  // Unfiltered-by-tag, workspace-scoped fetch supplies the stable universe of
  // tag options so the filter doesn't collapse as the user narrows it.
  const { data: tagUniverse } = useSuspenseQuery(
    orpc.projects.list.queryOptions({
      input: { workspace: wsScope, limit: 100 },
    }),
  )

  // Registry → structured, colored tag chips.
  const { data: regData } = useSuspenseQuery(orpc.tags.registry.list.queryOptions({ input: {} }))
  const registry = new Map(regData.items.map((k) => [k.key, k]))

  // Tag filter: the full declared registry, grouped by key, with a count of how
  // many in-scope projects carry each tag (0 for unused — they still show).
  const tagCounts = new Map<string, number>()
  for (const p of tagUniverse.items) for (const t of p.tags ?? []) tagCounts.set(t, (tagCounts.get(t) ?? 0) + 1)
  const filterGroups: TagGroup[] = regData.items
    .map((k) => ({ key: k.key, label: k.label, color: k.color, values: k.allowedValues }))
    .filter((g) => g.values.length > 0)

  // Stable counts (from the unfiltered, workspace-scoped set) for the filter
  // pills: projects needing grouping, and projects needing attention.
  const needsGroupingCount = tagUniverse.items.filter((p) => p.inferred || (p.tags ?? []).length === 0).length
  const attentionCount = tagUniverse.items.filter((p) => needsAttention(p.health)).length

  const toggleTag = (t: string) => {
    setSelectedTags(selectedTags.includes(t) ? selectedTags.filter((x) => x !== t) : [...selectedTags, t])
  }

  const query = search.toLowerCase().trim()
  const filtered = query
    ? projectsData.items.filter(
        (p) =>
          p.name.toLowerCase().includes(query) ||
          p.repo.toLowerCase().includes(query) ||
          p.tags.some((t) => t.toLowerCase().includes(query)),
      )
    : projectsData.items
  const projects = sortProjects(filtered, sort)
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
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
        <div>
          <h1 className="display-title text-3xl lg:text-4xl text-foreground">Projects</h1>
          <p className="text-muted-foreground text-sm lg:text-base mt-2">
            {projects.length} repositories{wsLabel}
          </p>
        </div>
        <div className="flex items-center gap-2 sm:shrink-0">
          <div className="relative flex-1 sm:w-56 sm:flex-none lg:w-72">
            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
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
          <FilterPill
            icon={<ArrowDownUp size={12} />}
            label={SORTS.find((o) => o.key === sort)!.label}
            active={sort !== 'recent'}
            onClear={() => setSort('recent')}
            items={SORTS.map((o) => ({ key: o.key, label: o.label, active: o.key === sort }))}
            onSelect={(key) => setSort(key as ProjectSort)}
          />
          <SaveViewButton route="/ci/projects" search={sp} />
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

      {(filterGroups.length > 0 || needsGroupingCount > 0 || attentionCount > 0) && (
        <div className="flex flex-wrap items-center gap-1.5">
          {attentionCount > 0 && (
            <button
              type="button"
              onClick={() => setAttention(!attention)}
              title="Projects that are currently red or have a low pass rate"
              className={`flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-xs font-medium transition-colors ${
                attention ? 'border-destructive bg-destructive-subtle text-destructive' : 'border-border text-muted-foreground hover:text-foreground'
              }`}
            >
              <AlertTriangle size={12} />
              Needs attention
              <span className="rounded-full bg-destructive-subtle text-destructive text-[10px] font-bold leading-none px-1.5 py-0.5">{attentionCount}</span>
            </button>
          )}
          {needsGroupingCount > 0 && (
            <button
              type="button"
              onClick={() => setNeedsGrouping(!needsGrouping)}
              title="Projects whose workspace was inferred (not declared) or that have no tags"
              className={`flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-xs font-medium transition-colors ${
                needsGrouping ? 'border-warning bg-warning-subtle text-warning' : 'border-border text-muted-foreground hover:text-foreground'
              }`}
            >
              <AlertTriangle size={12} />
              Needs grouping
              <span className="rounded-full bg-warning-subtle text-warning text-[10px] font-bold leading-none px-1.5 py-0.5">{needsGroupingCount}</span>
            </button>
          )}
          {filterGroups.length > 0 && (
            <button
              type="button"
              onClick={() => setTagFilterOpen(true)}
              className={`flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-xs font-medium transition-colors ${
                selectedTags.length > 0 ? 'border-primary bg-accent text-primary' : 'border-border text-muted-foreground hover:text-foreground'
              }`}
            >
              <Tag size={12} />
              {selectedTags.length > 0 ? `${selectedTags.length} tag${selectedTags.length > 1 ? 's' : ''}` : 'Filter tags'}
            </button>
          )}
          {selectedTags.map((t) => (
            <TagChip key={t} tag={t} registry={registry} onRemove={() => toggleTag(t)} />
          ))}
          {selectedTags.length > 0 && (
            <button
              type="button"
              onClick={() => setSelectedTags([])}
              className="text-xs font-medium text-muted-foreground hover:text-foreground underline underline-offset-2 transition-colors px-1"
            >
              Clear
            </button>
          )}
        </div>
      )}

      <TagManagerModal
        open={tagFilterOpen}
        onClose={() => setTagFilterOpen(false)}
        groups={filterGroups}
        applied={new Set(selectedTags)}
        onToggle={toggleTag}
        onClear={() => setSelectedTags([])}
        counts={tagCounts}
        title="Filter by tags"
        subtitle="Show projects matching any selected tag"
      />

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

export function ProjectCard({ project, registry, index }: { project: Project; registry: Map<string, TagKey>; index: number }) {
  return (
    <div className="feature-card rise-in overflow-hidden flex flex-col" style={{ animationDelay: `${index * 50 + 30}ms` }}>
      <Link
        to="/ci/projects/$id"
        params={{ id: project.id }}
        className="block p-5 space-y-3 hover:bg-accent transition-colors group flex-1"
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

        {/* Health: recent-run sparkline + pass rate + failing flag. */}
        <div className="h-5 flex items-center">
          <ProjectHealthBar health={project.health} />
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
          {/* min-w-0 on both sides is load-bearing: a flex child defaults to
              min-width:auto, so `truncate` on the branch does nothing without
              it and the name wraps instead. The body sets overflow-wrap:anywhere,
              which breaks mid-token — so the metadata is explicitly nowrap. */}
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-2 min-w-0 basis-0 grow">
              <StatusIcon status={project.lastRun.status} />
              <span className="flex items-center gap-1 text-xs text-muted-foreground min-w-[5rem]">
                <GitBranch size={11} className="shrink-0" />
                <span className="font-mono truncate" title={project.lastRun.branch}>
                  {project.lastRun.branch}
                </span>
              </span>
            </div>
            {/* Compact label here, not formatTimeline: this group is nowrap and
                shrink-0, so every character it gains is taken straight off the
                branch. "Jul 20 '25" caps it at 10ch; the exact time lives in the
                title. The branch keeps a 5rem floor so a long name degrades to a
                readable prefix rather than a single ellipsis. */}
            <div className="flex items-center gap-2 text-xs text-muted-foreground shrink-0 whitespace-nowrap">
              <span className="tabular-nums">{project.lastRun.duration}</span>
              <span className="opacity-50 tabular-nums" title={formatExactISO(project.lastRun.startedAt)}>
                {formatTimelineCompactISO(project.lastRun.startedAt)}
              </span>
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
    <div className="feature-card rise-in flex items-stretch overflow-hidden" style={{ animationDelay: `${index * 25 + 20}ms` }}>
      <Link
        to="/ci/projects/$id"
        params={{ id: project.id }}
        className="flex flex-col justify-center gap-1 min-w-0 flex-1 px-4 py-2.5 hover:bg-accent transition-colors group"
      >
        {/* Line 1: name + health + workspace */}
        <div className="flex items-center gap-2.5 min-w-0">
          <div className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: project.colour }} />
          <h3 className="font-semibold text-sm text-foreground group-hover:text-primary transition-colors truncate">{project.name}</h3>
          <div className="hidden md:flex ml-auto shrink-0">
            <ProjectHealthBar health={project.health} />
          </div>
          <span className="island-kicker !text-[11px] shrink-0 md:ml-2 ml-auto">{project.workspace}</span>
        </div>
        {/* Line 2: repo + tags */}
        <div className="flex items-center gap-2 min-w-0 pl-[18px]">
          <span className="text-xs text-muted-foreground font-mono truncate shrink-0 max-w-[16rem]">{project.repo}</span>
          <div className="hidden sm:flex items-center gap-1.5 min-w-0 overflow-hidden">
            <TagSummary tags={project.tags} registry={registry} max={4} />
          </div>
        </div>
      </Link>

      {project.lastRun ? (
        <Link
          to="/ci/runs/$id"
          params={{ id: project.lastRun.id }}
          className="flex items-center gap-2 shrink-0 px-4 border-l border-border text-xs text-muted-foreground hover:bg-accent hover:text-foreground transition-colors group"
        >
          <StatusIcon status={project.lastRun.status} />
          <span className="hidden md:flex items-center gap-1 min-w-0 max-w-[14rem]">
            <GitBranch size={11} className="shrink-0" />
            <span className="font-mono truncate" title={project.lastRun.branch}>
              {project.lastRun.branch}
            </span>
          </span>
          <span className="opacity-50 whitespace-nowrap tabular-nums" title={formatExactISO(project.lastRun.startedAt)}>
            {formatTimelineCompactISO(project.lastRun.startedAt)}
          </span>
          <ExternalLink size={11} className="opacity-0 group-hover:opacity-50 transition-opacity" />
        </Link>
      ) : (
        <span className="flex items-center shrink-0 px-4 border-l border-border text-xs text-muted-foreground opacity-40">No runs yet</span>
      )}
    </div>
  )
}
