import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { ScrollText, ChevronDown, ChevronRight, Globe, ListFilter } from 'lucide-react'
import { useState } from 'react'
import { orpc } from '#/lib/orpc'
import { formatTime } from '#/lib/format-time'
import { Pagination } from '#/components/Pagination'
import { useCursorPagination } from '#/hooks/use-cursor-pagination'
import { Badge, type BadgeVariant } from '#/components/Badge'
import { PageHeader } from '#/components/PageHeader'
import { EmptyState } from '#/components/EmptyState'
import { FilterPill } from '#/components/FilterPill'

export const Route = createFileRoute('/settings/audit-log')({
  component: AuditLogPage,
})

function actionVariant(action: string): BadgeVariant {
  const a = action.toLowerCase()
  if (/(create|add|approve)/.test(a)) return 'success'
  if (/(delete|remove|revoke|reject)/.test(a)) return 'danger'
  if (/(update|change)/.test(a)) return 'primary'
  return 'neutral'
}

function MetadataExpander({ metadata }: { metadata: unknown }) {
  const [expanded, setExpanded] = useState(false)

  if (!metadata) return null

  return (
    <div className="mt-2">
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-1 text-[12px] text-muted-foreground hover:text-foreground transition-colors"
      >
        {expanded ? <ChevronDown size={11} /> : <ChevronRight size={11} />}
        metadata
      </button>
      {expanded && (
        <pre className="mt-1.5 text-[11px] leading-relaxed bg-muted border border-border rounded-md p-2.5 overflow-x-auto text-muted-foreground font-mono">
          {JSON.stringify(metadata, null, 2)}
        </pre>
      )}
    </div>
  )
}

const PAGE_SIZE = 10

function AuditLogPage() {
  const { page, cursor, goToPage, reset } = useCursorPagination()
  const [action, setAction] = useState<string | undefined>(undefined)

  const { data } = useSuspenseQuery(
    orpc.auditEntries.list.queryOptions({ input: { action, limit: PAGE_SIZE, cursor } }),
  )
  // Unfiltered sample supplies a stable set of action options for the pill, so
  // selecting one doesn't collapse the choices to just that action.
  const { data: sample } = useSuspenseQuery(
    orpc.auditEntries.list.queryOptions({ input: { limit: 100 } }),
  )
  const actions = [...new Set(sample.items.map((e) => e.action))].sort()

  const entries = data.items

  const setActionAndReset = (a: string | undefined) => { setAction(a); reset() }

  return (
    <div className="space-y-5">
      <PageHeader title="Audit Log" />

      {actions.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <FilterPill
            icon={<ListFilter size={12} />}
            label={action ?? 'Action'}
            active={!!action}
            onClear={() => setActionAndReset(undefined)}
            items={actions.map((a) => ({ key: a, label: a, active: action === a }))}
            onSelect={(key) => setActionAndReset(key === action ? undefined : key)}
          />
        </div>
      )}

      {entries.length === 0 ? (
        <EmptyState icon={ScrollText} message={action ? `No “${action}” entries.` : 'No audit log entries yet.'} />
      ) : (
        <div className="island-shell !p-0 overflow-hidden">
          {/* Header */}
          <div className="hidden sm:grid sm:grid-cols-[140px_1fr_1fr_120px_120px] gap-3 px-4 py-2.5 border-b border-border bg-muted text-[12px] font-medium text-muted-foreground uppercase tracking-wider">
            <span>Timestamp</span>
            <span>Action</span>
            <span>Resource</span>
            <span>Resource ID</span>
            <span>IP Address</span>
          </div>

          {/* Rows */}
          <div className="divide-y divide-border">
            {entries.map((entry, i) => (
              <div
                key={entry.id}
                className="rise-in px-4 py-3 sm:grid sm:grid-cols-[140px_1fr_1fr_120px_120px] sm:gap-3 sm:items-start space-y-2 sm:space-y-0 hover:bg-accent transition-colors"
                style={{ animationDelay: `${i * 20 + 30}ms` }}
              >
                {/* Timestamp */}
                <span className="text-xs text-muted-foreground font-mono">
                  {formatTime(entry.createdAt)}
                </span>

                {/* Action */}
                <div>
                  <Badge variant={actionVariant(entry.action)}>{entry.action}</Badge>
                  <MetadataExpander metadata={entry.metadata} />
                </div>

                {/* Resource Type */}
                <span className="text-xs text-foreground">{entry.resourceType}</span>

                {/* Resource ID */}
                <span className="text-xs text-muted-foreground font-mono truncate">
                  {entry.resourceId || '\u2014'}
                </span>

                {/* IP Address */}
                <span className="text-xs text-muted-foreground flex items-center gap-1">
                  {entry.ipAddress ? (
                    <>
                      <Globe size={11} className="shrink-0" />
                      <span className="font-mono">{entry.ipAddress}</span>
                    </>
                  ) : (
                    '\u2014'
                  )}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      <Pagination
        page={page}
        hasMore={!!data.nextCursor}
        onPageChange={(p) => goToPage(p, data.nextCursor)}
        count={entries.length}
        pageSize={PAGE_SIZE}
        label="entries"
      />
    </div>
  )
}
