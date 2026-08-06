import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Shield, GitBranch, Clock, User, CheckCircle, XCircle, Ban, Server, Loader2 } from 'lucide-react'
import { client, orpc } from '#/lib/orpc'
import { formatTime } from '#/lib/format-time'
import { useScope } from '#/lib/scope-context'
import { useAction } from '#/hooks/use-action'
import { FilterPill } from '#/components/FilterPill'
import { Pagination } from '#/components/Pagination'
import { useCursorPagination } from '#/hooks/use-cursor-pagination'
import { BranchLabel } from '#/components/GitRef'

type GateFilter = 'pending' | 'approved' | 'rejected'

// Filters live in the URL so they survive navigating into a run and back.
interface GatesSearch {
  status?: GateFilter
  env?: string
}

export const Route = createFileRoute('/ci/gates')({
  validateSearch: (s: Record<string, unknown>): GatesSearch => ({
    status: s.status === 'approved' || s.status === 'rejected' ? s.status : undefined,
    env: typeof s.env === 'string' && s.env ? s.env : undefined,
  }),
  component: GatesPage,
})

const statusTabs = [
  { key: 'pending' as const, label: 'Pending' },
  { key: 'approved' as const, label: 'Approved' },
  { key: 'rejected' as const, label: 'Rejected' },
] as const

const GATE_PAGE_SIZE = 12

function GatesPage() {
  const { workspaceMatches } = useScope()
  const sp = Route.useSearch()
  const navigate = Route.useNavigate()
  const status: GateFilter = sp.status ?? 'pending'
  const envFilter = sp.env
  const { page, cursor, goToPage, reset } = useCursorPagination()

  // Filter mutations write to the URL (replace, so toggles don't stack history)
  // and reset pagination. 'pending' is the default, so it drops out of the URL.
  const setFilters = (patch: Partial<GatesSearch>) => {
    navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true })
    reset()
  }
  const setStatus = (v: GateFilter) => setFilters({ status: v === 'pending' ? undefined : v })
  const setEnvFilter = (v: string | undefined) => setFilters({ env: v })

  const { data } = useSuspenseQuery(
    orpc.gates.list.queryOptions({ input: { status, limit: GATE_PAGE_SIZE, cursor } }),
  )

  const approve = useAction(
    (g: { runId: string; stepName: string }) => client.gates.approve({ runId: g.runId, stepName: g.stepName }),
    { invalidate: [orpc.gates.list.key()] },
  )
  const reject = useAction(
    (g: { runId: string; stepName: string }) => client.gates.reject({ runId: g.runId, stepName: g.stepName }),
    { invalidate: [orpc.gates.list.key()] },
  )
  const deciding = approve.isPending || reject.isPending

  const gates = data.items.filter((g) => {
    if (!workspaceMatches(g.workspace)) return false
    if (envFilter && g.environment !== envFilter) return false
    return true
  })

  // Environment is a local filter here, not a global scope.
  const environments = [...new Set(
    data.items.map((g) => g.environment).filter((e): e is string => !!e),
  )].sort()

  return (
    <div className="rise-in space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="display-title text-3xl lg:text-4xl text-foreground">Gates</h1>
          <p className="text-muted-foreground text-sm lg:text-base mt-2">
            Deployment approval gates
          </p>
        </div>
      </div>

      {/* Status tabs */}
      <div className="flex items-center gap-1 border-b border-border pb-px">
        {statusTabs.map((tab) => (
          <button
            key={tab.key}
            type="button"
            onClick={() => setStatus(tab.key)}
            className={`flex items-center gap-1.5 shrink-0 px-3 py-2 text-xs font-medium border-b-2 transition-colors ${
              status === tab.key
                ? 'border-primary text-primary'
                : 'border-transparent text-muted-foreground hover:text-foreground hover:border-border'
            }`}
          >
            {tab.label}
          </button>
        ))}
        <div className="ml-auto flex items-center gap-2">
          {environments.length > 0 && (
            <FilterPill
              icon={<Server size={12} />}
              label={envFilter ?? 'Environment'}
              active={!!envFilter}
              onClear={() => setEnvFilter(undefined)}
              items={environments.map((e) => ({ key: e, label: e, active: envFilter === e }))}
              onSelect={(key) => setEnvFilter(key === envFilter ? undefined : key)}
            />
          )}
          <span className="text-xs text-muted-foreground">{gates.length} gates</span>
        </div>
      </div>

      {gates.length === 0 ? (
        <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
          <Shield size={32} strokeWidth={1.2} />
          <span className="text-sm">
            {status === 'pending'
              ? 'No pending approvals.'
              : status === 'approved'
                ? 'No approved gates to show.'
                : 'No rejected gates to show.'}
          </span>
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {gates.map((gate, i) => (
            <div
              key={`${gate.runId}-${gate.stepName}`}
              className="island-shell !p-0 overflow-hidden rise-in flex flex-col"
              style={{ animationDelay: `${i * 50 + 30}ms` }}
            >
              <div className="p-4 sm:p-5 space-y-3 flex-1">
                <div className="flex items-start justify-between gap-3">
                  <div className="flex items-center gap-2.5 min-w-0">
                    <div className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: gate.projectColour }} />
                    <div className="min-w-0">
                      <h3 className="font-semibold text-sm text-foreground truncate">{gate.projectName}</h3>
                      <p className="text-xs text-muted-foreground font-mono truncate">{gate.stepName}</p>
                    </div>
                  </div>
                  <GateStatusBadge status={gate.status} />
                </div>

                <p className="text-sm text-muted-foreground line-clamp-2" title={gate.message}>{gate.message}</p>

                <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                  <span className="flex items-center gap-1">
                    <GitBranch size={12} />
                    <BranchLabel branch={gate.branch} icon={false} max="14rem" />
                  </span>
                  <span className="flex items-center gap-1">
                    <User size={12} />
                    {gate.triggeredBy}
                  </span>
                  <span className="flex items-center gap-1">
                    <Clock size={12} />
                    {formatTime(gate.createdAt)}
                  </span>
                </div>

                {gate.reviewedBy && (
                  <div className="flex items-center gap-2 text-xs pt-1 border-t border-border">
                    <span className={gate.status === 'approved' ? 'text-success' : 'text-destructive'}>
                      {gate.status === 'approved' ? 'Approved' : 'Rejected'} by
                    </span>
                    <span className="font-medium text-foreground">{gate.reviewedBy}</span>
                    {gate.reviewedAt && (
                      <span className="text-muted-foreground opacity-50">{formatTime(gate.reviewedAt)}</span>
                    )}
                  </div>
                )}
              </div>

              <div className="flex border-t border-border">
                <Link
                  to="/ci/runs/$id"
                  params={{ id: gate.runId }}
                  className={`flex-1 px-4 py-2.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors text-center ${
                    gate.status === 'pending' ? 'border-r border-border' : ''
                  }`}
                >
                  View Run
                </Link>
                {gate.status === 'pending' && (
                  <>
                    {(() => {
                      const rejecting = reject.isPending && reject.variables?.runId === gate.runId && reject.variables?.stepName === gate.stepName
                      const approving = approve.isPending && approve.variables?.runId === gate.runId && approve.variables?.stepName === gate.stepName
                      return (
                        <>
                          <button
                            type="button"
                            disabled={deciding}
                            onClick={() => reject.mutate({ runId: gate.runId, stepName: gate.stepName })}
                            className="flex-1 flex items-center justify-center gap-1.5 px-4 py-2.5 text-xs font-medium text-destructive hover:bg-destructive-subtle transition-colors border-r border-border disabled:opacity-50"
                          >
                            {rejecting ? <Loader2 size={13} className="animate-spin" /> : <XCircle size={13} />}
                            {rejecting ? 'Rejecting…' : 'Reject'}
                          </button>
                          <button
                            type="button"
                            disabled={deciding}
                            onClick={() => approve.mutate({ runId: gate.runId, stepName: gate.stepName })}
                            className="flex-1 flex items-center justify-center gap-1.5 px-4 py-2.5 text-xs font-medium text-success hover:bg-success-subtle transition-colors disabled:opacity-50"
                          >
                            {approving ? <Loader2 size={13} className="animate-spin" /> : <CheckCircle size={13} />}
                            {approving ? 'Approving…' : 'Approve'}
                          </button>
                        </>
                      )
                    })()}
                  </>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {gates.length > 0 && (
        <Pagination
          page={page}
          hasMore={!!data.nextCursor}
          onPageChange={(p) => goToPage(p, data.nextCursor)}
          count={gates.length}
          pageSize={GATE_PAGE_SIZE}
          label="gates"
        />
      )}
    </div>
  )
}

function GateStatusBadge({ status }: { status: string }) {
  switch (status) {
    case 'pending':
      return (
        <span className="island-kicker !text-[11px] shrink-0 bg-warning-subtle text-warning border-warning">
          Awaiting
        </span>
      )
    case 'approved':
      return (
        <span className="inline-flex items-center gap-1 island-kicker !text-[11px] shrink-0 bg-success-subtle text-success border-success">
          <CheckCircle size={10} />
          Approved
        </span>
      )
    case 'rejected':
      return (
        <span className="inline-flex items-center gap-1 island-kicker !text-[11px] shrink-0 bg-destructive-subtle text-destructive border-destructive">
          <Ban size={10} />
          Rejected
        </span>
      )
    default:
      return null
  }
}
