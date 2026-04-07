import { useState, useCallback } from 'react'

interface CursorPaginationOptions {
  pageSize?: number
}

interface CursorPaginationResult {
  page: number
  cursor: string | undefined
  goToPage: (page: number, nextCursor?: string) => void
  reset: () => void
}

/**
 * Manages cursor-based pagination state.
 *
 * Usage:
 *   const { page, cursor, goToPage, reset } = useCursorPagination()
 *   const { data } = useSuspenseQuery(orpc.runs.list.queryOptions({ input: { cursor, limit } }))
 *   // call goToPage(page + 1, data.nextCursor) to advance
 *   // call reset() when filters change
 */
export function useCursorPagination(_opts?: CursorPaginationOptions): CursorPaginationResult {
  const [page, setPage] = useState(1)
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined])

  const cursor = cursors[page - 1]

  const goToPage = useCallback(
    (p: number, nextCursor?: string) => {
      if (p < 1) return
      if (p > page && nextCursor) {
        setCursors((prev) => {
          if (prev.length <= page) return [...prev, nextCursor]
          return prev
        })
      }
      setPage(p)
    },
    [page],
  )

  const reset = useCallback(() => {
    setPage(1)
    setCursors([undefined])
  }, [])

  return { page, cursor, goToPage, reset }
}
