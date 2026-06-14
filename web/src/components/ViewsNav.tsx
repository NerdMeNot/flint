import { Link, useRouterState } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Bookmark, X } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { SMART_VIEWS } from '#/lib/views'

const activeStyle = { background: 'color-mix(in oklab, var(--ring), black 35%)' }

function rowClass(active: boolean) {
  return `group flex items-center gap-2.5 rounded-lg py-1.5 px-3 text-sm font-medium transition-all duration-150 ${
    active ? 'text-white shadow-sm' : 'text-muted-foreground hover:text-foreground hover:bg-[var(--link-bg-hover)]'
  }`
}

export function ViewsNav({ collapsed }: { collapsed: boolean }) {
  const { data } = useQuery(orpc.views.list.queryOptions({}))
  const saved = data?.items ?? []
  const currentPath = useRouterState({ select: (s) => s.location.pathname })
  const del = useAction((id: string) => client.views.delete({ id }), { invalidate: [orpc.views.list.key()] })

  // Views need their labels — hidden while the sidebar is collapsed.
  if (collapsed) return null

  const isActive = (id: string) => currentPath === `/ci/views/${id}`

  return (
    <div className="pt-3 mt-2 border-t border-border/60 space-y-0.5">
      <p className="px-1 pb-1 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground/45">Views</p>

      {SMART_VIEWS.map((v) => {
        const active = isActive(v.id)
        const Icon = v.icon ?? Bookmark
        return (
          <Link
            key={v.id}
            to="/ci/views/$id"
            params={{ id: v.id }}
            className={rowClass(active)}
            style={active ? activeStyle : undefined}
          >
            <Icon size={15} strokeWidth={active ? 2.2 : 1.8} className="shrink-0" />
            <span className="truncate">{v.name}</span>
          </Link>
        )
      })}

      {saved.length > 0 && <div className="h-px bg-border/40 my-1.5 mx-2" />}

      {/* First-run nudge: tells people where saved views come from. */}
      {saved.length === 0 && (
        <p className="px-3 pt-1 text-[11px] leading-snug text-muted-foreground/45">
          Filter a list, then <span className="font-medium text-muted-foreground/70">Save view</span> to pin it here.
        </p>
      )}

      {saved.map((v) => {
        const active = isActive(v.id)
        return (
          <div key={v.id} className="relative group/view">
            <Link
              to="/ci/views/$id"
              params={{ id: v.id }}
              className={`${rowClass(active)} pr-7`}
              style={active ? activeStyle : undefined}
            >
              <Bookmark size={15} strokeWidth={active ? 2.2 : 1.8} className="shrink-0" />
              <span className="truncate">{v.name}</span>
            </Link>
            <button
              type="button"
              onClick={() => del.mutate(v.id)}
              title="Delete view"
              className="absolute right-1.5 top-1/2 -translate-y-1/2 flex items-center justify-center w-5 h-5 rounded text-muted-foreground/50 opacity-0 group-hover/view:opacity-100 hover:text-destructive hover:bg-destructive/10 transition-all"
            >
              <X size={12} />
            </button>
          </div>
        )
      })}
    </div>
  )
}
