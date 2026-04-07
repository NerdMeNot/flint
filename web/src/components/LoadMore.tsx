import { Loader2 } from 'lucide-react'

interface LoadMoreProps {
  hasMore: boolean
  loading?: boolean
  onClick: () => void
  count: number
  label?: string
}

export function LoadMore({ hasMore, loading, onClick, count, label = 'items' }: LoadMoreProps) {
  return (
    <div className="flex items-center justify-between pt-2">
      <span className="text-xs text-muted-foreground">
        {count} {label}
      </span>
      {hasMore && (
        <button
          type="button"
          onClick={onClick}
          disabled={loading}
          className="flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent disabled:opacity-50 transition-colors"
        >
          {loading && <Loader2 size={12} className="animate-spin" />}
          Load more
        </button>
      )}
    </div>
  )
}
