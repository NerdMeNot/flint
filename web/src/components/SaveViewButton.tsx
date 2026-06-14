import { useState, useRef, useEffect } from 'react'
import { BookmarkPlus } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'

// Captures the current route + active URL filters as a named saved view.
// Disabled until at least one filter is applied (an unfiltered view just
// duplicates the nav item).
export function SaveViewButton({ route, search }: { route: string; search: Record<string, unknown> }) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const ref = useRef<HTMLDivElement>(null)

  const cleaned = Object.fromEntries(
    Object.entries(search).filter(([, v]) => v !== undefined && v !== '' && !(Array.isArray(v) && v.length === 0)),
  )
  const hasFilters = Object.keys(cleaned).length > 0

  const create = useAction(
    (input: { name: string; route: string; search: Record<string, unknown> }) => client.views.create(input),
    { invalidate: [orpc.views.list.key()], onSuccess: () => { setOpen(false); setName('') } },
  )

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  function submit() {
    const n = name.trim()
    if (!n) return
    create.mutate({ name: n, route, search: cleaned })
  }

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => hasFilters && setOpen((v) => !v)}
        disabled={!hasFilters}
        title={hasFilters ? 'Save these filters as a view' : 'Apply a filter to save a view'}
        className={`flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-xs font-medium transition-colors ${
          open ? 'border-primary/40 text-primary' : 'border-border text-muted-foreground hover:text-foreground'
        } disabled:opacity-40 disabled:cursor-not-allowed`}
      >
        <BookmarkPlus size={13} />
        <span className="hidden sm:inline">Save view</span>
      </button>

      {open && (
        <div
          className="absolute right-0 top-full mt-1 z-50 w-64 rounded-lg border border-border p-3 shadow-xl"
          style={{ background: 'var(--surface-strong)' }}
        >
          <label className="block text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60 mb-1.5">
            Save current view
          </label>
          <input
            type="text"
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); submit() } if (e.key === 'Escape') setOpen(false) }}
            placeholder="View name"
            className="w-full rounded-md border border-border bg-transparent px-2.5 py-1.5 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
          />
          <div className="flex justify-end gap-2 mt-2.5">
            <button
              type="button"
              onClick={() => setOpen(false)}
              className="rounded-md border border-border px-2.5 py-1 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={submit}
              disabled={!name.trim() || create.isPending}
              className="rounded-md bg-primary px-2.5 py-1 text-xs font-semibold text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-40"
            >
              Save
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
