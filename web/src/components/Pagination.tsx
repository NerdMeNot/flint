import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from 'lucide-react'

interface PaginationProps {
  page: number
  totalPages?: number
  hasMore: boolean
  onPageChange: (page: number) => void
  count: number
  pageSize: number
  onPageSizeChange?: (size: number) => void
  pageSizeOptions?: number[]
  label?: string
}

const DEFAULT_PAGE_SIZES = [10, 15, 25, 50]

export function Pagination({
  page,
  totalPages,
  hasMore,
  onPageChange,
  count,
  pageSize,
  onPageSizeChange,
  pageSizeOptions = DEFAULT_PAGE_SIZES,
  label = 'items',
}: PaginationProps) {
  const from = count > 0 ? (page - 1) * pageSize + 1 : 0
  const to = from + count - 1

  // Estimate total pages if not provided
  const estimatedTotal = totalPages ?? (hasMore ? page + 1 : page)
  const lastPage = hasMore ? undefined : page

  // Build page numbers to show
  const pages = buildPageRange(page, estimatedTotal, hasMore)

  return (
    <div className="flex items-center justify-between gap-4 pt-3">
      {/* Left: count + page size */}
      <div className="flex items-center gap-3">
        <span className="text-xs text-muted-foreground">
          {count > 0 ? `${from}–${to}` : 'No'} {label}
        </span>
        {onPageSizeChange && (
          <div className="flex items-center gap-1.5">
            <span className="text-[0.65rem] text-muted-foreground opacity-50">Show</span>
            <select
              value={pageSize}
              onChange={(e) => onPageSizeChange(Number(e.target.value))}
              className="rounded-md border border-border bg-transparent px-1.5 py-0.5 text-[0.65rem] font-medium text-foreground focus:outline-none focus:ring-1 focus:ring-ring"
            >
              {pageSizeOptions.map((size) => (
                <option key={size} value={size}>{size}</option>
              ))}
            </select>
          </div>
        )}
      </div>

      {/* Right: page navigation */}
      <div className="flex items-center gap-0.5">
        {/* First page */}
        <button
          type="button"
          onClick={() => onPageChange(1)}
          disabled={page <= 1}
          title="First page"
          className="flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent disabled:opacity-20 disabled:pointer-events-none transition-colors"
        >
          <ChevronsLeft size={14} />
        </button>

        {/* Previous */}
        <button
          type="button"
          onClick={() => onPageChange(page - 1)}
          disabled={page <= 1}
          title="Previous page"
          className="flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent disabled:opacity-20 disabled:pointer-events-none transition-colors"
        >
          <ChevronLeft size={14} />
        </button>

        {/* Page numbers */}
        {pages.map((p, i) => {
          if (p === '...') {
            return <span key={`ellipsis-${i}`} className="px-1 text-xs text-muted-foreground opacity-30">…</span>
          }
          const pageNum = p as number
          return (
            <button
              key={pageNum}
              type="button"
              onClick={() => onPageChange(pageNum)}
              className={`flex items-center justify-center min-w-[28px] h-7 rounded-md px-1.5 text-xs font-medium transition-colors ${
                pageNum === page
                  ? 'bg-primary/15 text-primary'
                  : 'text-muted-foreground hover:text-foreground hover:bg-accent'
              }`}
            >
              {pageNum}
            </button>
          )
        })}

        {hasMore && !pages.includes(page + 1) && (
          <span className="px-1 text-xs text-muted-foreground opacity-30">…</span>
        )}

        {/* Next */}
        <button
          type="button"
          onClick={() => onPageChange(page + 1)}
          disabled={!hasMore}
          title="Next page"
          className="flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent disabled:opacity-20 disabled:pointer-events-none transition-colors"
        >
          <ChevronRight size={14} />
        </button>

        {/* Last page (only if we know it) */}
        {lastPage && lastPage > 1 && (
          <button
            type="button"
            onClick={() => onPageChange(lastPage)}
            disabled={page === lastPage}
            title="Last page"
            className="flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent disabled:opacity-20 disabled:pointer-events-none transition-colors"
          >
            <ChevronsRight size={14} />
          </button>
        )}
      </div>
    </div>
  )
}

/**
 * Build a range of page numbers to display.
 * Shows: 1 ... (current-1) current (current+1) ... last
 */
function buildPageRange(current: number, estimated: number, hasMore: boolean): (number | '...')[] {
  const last = hasMore ? estimated : estimated
  const pages: (number | '...')[] = []

  if (last <= 7) {
    for (let i = 1; i <= last; i++) pages.push(i)
    return pages
  }

  // Always show first page
  pages.push(1)

  if (current > 3) {
    pages.push('...')
  }

  // Window around current
  const start = Math.max(2, current - 1)
  const end = Math.min(last - 1, current + 1)
  for (let i = start; i <= end; i++) {
    pages.push(i)
  }

  if (current < last - 2) {
    pages.push('...')
  }

  // Always show last page (if known and not already there)
  if (!hasMore && last > 1 && !pages.includes(last)) {
    pages.push(last)
  }

  return pages
}
