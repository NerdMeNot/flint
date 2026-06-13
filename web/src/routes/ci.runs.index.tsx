import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Ban, FolderGit2, RotateCcw, CheckCircle, Server } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import type { RunStatusValue } from '#/lib/api/types'
import { RunRow } from '#/components/RunRow'
import { useScope } from '#/lib/scope-context'
import { useAction } from '#/hooks/use-action'
import { Pagination } from '#/components/Pagination'
import { FilterPill } from '#/components/FilterPill'
import { useCursorPagination } from '#/hooks/use-cursor-pagination'
import { groupByBucket, parseDurationToSeconds, median } from '#/lib/run-feed'

const DEFAULT_PAGE_SIZE = 15

// Filters live in the URL so they survive navigating into a run and back.
interface RunsSearch {
  status?: RunStatusValue
  project?: string
  env?: string
}

export const Route = createFileRoute('/ci/runs/')({
  validateSearch: (s: Record<string, unknown>): RunsSearch => ({
    status: typeof s.status === 'string' && s.status ? (s.status as RunStatusValue) : undefined,
    project: typeof s.project === 'string' && s.project ? s.project : undefined,
    env: typeof s.env === 'string' && s.env ? s.env : undefined,
  }),
  component: RunsListPage,
})

function RunsListPage() {
  const { workspaces } = useScope()
  const sp = Route.useSearch()
  const navigate = Route.useNavigate()
  const statusFilter = sp.status
  const projectFilter = sp.project
  const envFilter = sp.env
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const { page, cursor, goToPage, reset } = useCursorPagination()

  const { data } = useSuspenseQuery(
    orpc.runs.list.queryOptions({
      input: {
        status: statusFilter,
        projectId: projectFilter,
        limit: pageSize,
        cursor,
      },
    }),
  )

  function changePageSize(size: number) {
    setPageSize(size)
    reset()
  }

  // Filter mutations write to the URL (replace, so toggles don't stack history)
  // and reset pagination.
  const setFilters = (patch: Partial<RunsSearch>) => {
    navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true })
    reset()
  }
  const setStatusAndReset = (v: RunStatusValue | undefined) => setFilters({ status: v })
  const setProjectAndReset = (v: string | undefined) => setFilters({ project: v })
  const setEnvAndReset = (v: string | undefined) => setFilters({ env: v })

  const { data: projectsData } = useSuspenseQuery(
    orpc.projects.list.queryOptions({ input: {} }),
  )
  const allProjects = projectsData.items

  // Scope filtering
  const wsSet = workspaces.length > 0 ? new Set(workspaces) : null
  const projectsInScope = wsSet
    ? allProjects.filter((p) => wsSet.has(p.workspace))
    : allProjects
  const projectIds = wsSet ? new Set(projectsInScope.map((p) => p.id)) : null

  const filteredItems = data.items.filter((r) => {
    if (projectIds && !projectIds.has(r.projectId)) return false
    if (envFilter && r.environment !== envFilter) return false
    return true
  })

  const projects = projectsInScope

  // Environment is a local run filter (not a global scope). Options come from the
  // environments present on the runs in view.
  const environments = [...new Set(
    data.items.map((r) => r.environment).filter((e): e is string => !!e),
  )].sort()

  // Per-project median duration (from finished runs on this page) for the
  // slower/faster indicator on each row.
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
    for (const [id, secs] of buckets) out.set(id, median(secs))
    return out
  })()

  const grouped = groupByBucket(filteredItems)

  return (
    <div className="space-y-6 rise-in">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="display-title text-3xl lg:text-4xl text-foreground">Runs</h1>
          <p className="text-muted-foreground text-sm lg:text-base mt-2">
            All pipeline runs across projects
          </p>
        </div>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap items-center gap-1.5">
        <FilterPill<RunStatusValue>
          icon={<CheckCircle size={12} />}
          label={statusFilter ?? 'Status'}
          active={!!statusFilter}
          onClear={() => setStatusAndReset(undefined)}
          items={[
            { key: 'succeeded', label: 'Succeeded', active: statusFilter === 'succeeded' },
            { key: 'failed', label: 'Failed', active: statusFilter === 'failed' },
            { key: 'running', label: 'Running', active: statusFilter === 'running' },
            { key: 'pending', label: 'Pending', active: statusFilter === 'pending' },
            { key: 'cancelled', label: 'Cancelled', active: statusFilter === 'cancelled' },
          ]}
          onSelect={(key) => setStatusAndReset(key === statusFilter ? undefined : key)}
        />
        <FilterPill
          icon={<FolderGit2 size={12} />}
          label={projectFilter
            ? projects.find((p) => p.id === projectFilter)?.name ?? 'Project'
            : 'Project'}
          active={!!projectFilter}
          onClear={() => setProjectAndReset(undefined)}
          items={projects.map((p) => ({
            key: p.id,
            label: p.name,
            active: projectFilter === p.id,
          }))}
          onSelect={(key) => setProjectAndReset(key === projectFilter ? undefined : key)}
        />
        {environments.length > 0 && (
          <FilterPill
            icon={<Server size={12} />}
            label={envFilter ?? 'Environment'}
            active={!!envFilter}
            onClear={() => setEnvAndReset(undefined)}
            items={environments.map((e) => ({ key: e, label: e, active: envFilter === e }))}
            onSelect={(key) => setEnvAndReset(key === envFilter ? undefined : key)}
          />
        )}
        <span className="text-xs text-muted-foreground ml-auto">{filteredItems.length} runs</span>
      </div>

      {/* Runs feed — grouped by time bucket */}
      <div className="island-shell !p-0 overflow-hidden">
        {filteredItems.length === 0 ? (
          <div className="p-12 text-center text-sm text-muted-foreground">
            No runs match the current filters.
          </div>
        ) : (
          grouped.map(({ bucket, runs }) => (
            <div key={bucket}>
              <div className="flex items-center gap-2 px-4 lg:px-5 py-2 bg-accent/20 border-b border-border text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/70">
                {bucket}
                <span className="rounded-full bg-border/70 px-1.5 py-0.5 text-[10px] font-bold leading-none text-muted-foreground">{runs.length}</span>
              </div>
              <div className="divide-y divide-border">
                {runs.map((run) => (
                  <RunRow
                    key={run.id}
                    run={run}
                    baselineSecs={baselineByProject.get(run.projectId)}
                    action={<RunAction status={run.status} runId={run.id} />}
                  />
                ))}
              </div>
            </div>
          ))
        )}
      </div>

      <Pagination
        page={page}
        hasMore={!!data.nextCursor}
        onPageChange={(p) => goToPage(p, data.nextCursor)}
        count={filteredItems.length}
        pageSize={pageSize}
        onPageSizeChange={changePageSize}
        label="runs"
      />
    </div>
  )
}

function RunAction({ status, runId }: { status: string; runId: string }) {
  const cancel = useAction((id: string) => client.runs.cancel({ runId: id }), {
    invalidate: [orpc.runs.list.key()],
  })
  const retry = useAction((id: string) => client.runs.retry({ runId: id }), {
    invalidate: [orpc.runs.list.key()],
  })

  if (status === 'running' || status === 'pending') {
    return (
      <button
        type="button"
        disabled={cancel.isPending}
        onClick={(e) => { e.preventDefault(); e.stopPropagation(); cancel.mutate(runId) }}
        title="Cancel run"
        className="shrink-0 flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/5 transition-colors disabled:opacity-50"
      >
        <Ban size={13} />
      </button>
    )
  }
  if (status === 'failed' || status === 'cancelled') {
    return (
      <button
        type="button"
        disabled={retry.isPending}
        onClick={(e) => { e.preventDefault(); e.stopPropagation(); retry.mutate(runId) }}
        title="Retry run"
        className="shrink-0 flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-primary hover:bg-primary/5 transition-colors disabled:opacity-50"
      >
        <RotateCcw size={13} />
      </button>
    )
  }
  return null
}

