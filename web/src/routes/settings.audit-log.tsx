import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { ScrollText, ChevronDown, ChevronRight, Globe } from 'lucide-react'
import { useState } from 'react'
import { orpc } from '#/lib/orpc'
import { formatTime } from '#/lib/format-time'
import { Pagination } from '#/components/Pagination'
import { useCursorPagination } from '#/hooks/use-cursor-pagination'
import { Badge, type BadgeVariant } from '#/components/Badge'
import { PageHeader } from '#/components/PageHeader'

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
        <pre className="mt-1.5 text-[11px] leading-relaxed bg-muted/50 border border-border rounded-md p-2.5 overflow-x-auto text-muted-foreground font-mono">
          {JSON.stringify(metadata, null, 2)}
        </pre>
      )}
    </div>
  )
}

const PAGE_SIZE = 10

function AuditLogPage() {
  const { page, cursor, goToPage } = useCursorPagination()

  const { data } = useSuspenseQuery(
    orpc.auditEntries.list.queryOptions({ input: { limit: PAGE_SIZE, cursor } }),
  )

  const entries = data.items

  return (
    <div className="space-y-5">
      <PageHeader title="Audit Log" />

      {entries.length === 0 ? (
        <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
          <ScrollText size={32} strokeWidth={1.2} />
          <span className="text-sm">No audit log entries yet.</span>
        </div>
      ) : (
        <div className="island-shell !p-0 overflow-hidden">
          {/* Header */}
          <div className="hidden sm:grid sm:grid-cols-[140px_1fr_1fr_120px_120px] gap-3 px-4 py-2.5 border-b border-border bg-muted/30 text-[12px] font-medium text-muted-foreground uppercase tracking-wider">
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
                className="rise-in px-4 py-3 sm:grid sm:grid-cols-[140px_1fr_1fr_120px_120px] sm:gap-3 sm:items-start space-y-2 sm:space-y-0 hover:bg-accent/30 transition-colors"
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
