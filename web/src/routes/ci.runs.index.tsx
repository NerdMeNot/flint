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

const DEFAULT_PAGE_SIZE = 15

export const Route = createFileRoute('/ci/runs/')({
  component: RunsListPage,
})

function RunsListPage() {
  const { workspaces } = useScope()
  const [statusFilter, setStatusFilter] = useState<RunStatusValue | undefined>()
  const [projectFilter, setProjectFilter] = useState<string | undefined>()
  const [envFilter, setEnvFilter] = useState<string | undefined>()
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

  const setStatusAndReset = (v: RunStatusValue | undefined) => { setStatusFilter(v); reset() }
  const setProjectAndReset = (v: string | undefined) => { setProjectFilter(v); reset() }
  const setEnvAndReset = (v: string | undefined) => { setEnvFilter(v); reset() }

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

      {/* Runs list */}
      <div className="island-shell !p-0 overflow-hidden">
        {filteredItems.length === 0 ? (
          <div className="p-12 text-center text-sm text-muted-foreground">
            No runs match the current filters.
          </div>
        ) : (
          <div className="divide-y divide-border">
            {filteredItems.map((run) => (
              <RunRow
                key={run.id}
                run={run}
                action={<RunAction status={run.status} runId={run.id} />}
              />
            ))}
          </div>
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

