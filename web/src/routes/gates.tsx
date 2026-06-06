import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Shield, GitBranch, Clock, User, CheckCircle, XCircle, Ban } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { formatTime } from '#/lib/format-time'
import { useScope } from '#/lib/scope-context'
import { Pagination } from '#/components/Pagination'
import { useCursorPagination } from '#/hooks/use-cursor-pagination'

export const Route = createFileRoute('/gates')({
  component: GatesPage,
})

const statusTabs = [
  { key: 'pending' as const, label: 'Pending' },
  { key: 'approved' as const, label: 'Approved' },
  { key: 'rejected' as const, label: 'Rejected' },
] as const

type GateFilter = 'pending' | 'approved' | 'rejected'

const GATE_PAGE_SIZE = 12

function GatesPage() {
  const { workspaceMatches, environmentMatches } = useScope()
  const [status, setStatus] = useState<GateFilter>('pending')
  const { page, cursor, goToPage, reset } = useCursorPagination()

  const { data } = useSuspenseQuery(
    orpc.gates.list.queryOptions({ input: { status, limit: GATE_PAGE_SIZE, cursor } }),
  )

  const gates = data.items.filter((g: any) => {
    if (!workspaceMatches(g.workspace)) return false
    if (!environmentMatches(g.environment)) return false
    return true
  })

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
            onClick={() => { setStatus(tab.key); reset() }}
            className={`flex items-center gap-1.5 shrink-0 px-3 py-2 text-xs font-medium border-b-2 transition-colors ${
              status === tab.key
                ? 'border-primary text-primary'
                : 'border-transparent text-muted-foreground hover:text-foreground hover:border-border'
            }`}
          >
            {tab.label}
          </button>
        ))}
        <span className="ml-auto text-xs text-muted-foreground">{gates.length} gates</span>
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
                    <span className="font-mono">{gate.branch}</span>
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
                  to="/runs/$id"
                  params={{ id: gate.runId }}
                  className={`flex-1 px-4 py-2.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors text-center ${
                    gate.status === 'pending' ? 'border-r border-border' : ''
                  }`}
                >
                  View Run
                </Link>
                {gate.status === 'pending' && (
                  <>
                    <button
                      type="button"
                      className="flex-1 flex items-center justify-center gap-1.5 px-4 py-2.5 text-xs font-medium text-destructive hover:bg-destructive/5 transition-colors border-r border-border"
                    >
                      <XCircle size={13} />
                      Reject
                    </button>
                    <button
                      type="button"
                      className="flex-1 flex items-center justify-center gap-1.5 px-4 py-2.5 text-xs font-medium text-success hover:bg-success/5 transition-colors"
                    >
                      <CheckCircle size={13} />
                      Approve
                    </button>
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
        <span className="island-kicker !text-[11px] shrink-0 bg-warning/10 text-warning border-warning/20">
          Awaiting
        </span>
      )
    case 'approved':
      return (
        <span className="inline-flex items-center gap-1 island-kicker !text-[11px] shrink-0 bg-success/10 text-success border-success/20">
          <CheckCircle size={10} />
          Approved
        </span>
      )
    case 'rejected':
      return (
        <span className="inline-flex items-center gap-1 island-kicker !text-[11px] shrink-0 bg-destructive/10 text-destructive border-destructive/20">
          <Ban size={10} />
          Rejected
        </span>
      )
    default:
      return null
  }
}
