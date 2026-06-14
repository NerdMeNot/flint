import { Link, useRouterState } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { XCircle, Loader2, Shield, AlertTriangle, Bookmark, X } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'

const activeStyle = { background: 'color-mix(in oklab, var(--ring), black 35%)' }

// Built-in, zero-setup views — client-side selectors over the URL filters the
// list pages already understand.
const SMART_VIEWS: { icon: typeof XCircle; label: string; route: string; search: Record<string, unknown> }[] = [
  { icon: XCircle, label: 'Failing runs', route: '/ci/runs', search: { status: 'failed' } },
  { icon: Loader2, label: 'Running now', route: '/ci/runs', search: { status: 'running' } },
  { icon: Shield, label: 'Needs approval', route: '/ci/gates', search: {} },
  { icon: AlertTriangle, label: 'Failing projects', route: '/ci/projects', search: { sort: 'failing' } },
]

function rowClass(active: boolean) {
  return `group flex items-center gap-2.5 rounded-lg py-1.5 px-3 text-sm font-medium transition-all duration-150 ${
    active ? 'text-white shadow-sm' : 'text-muted-foreground hover:text-foreground hover:bg-[var(--link-bg-hover)]'
  }`
}

export function ViewsNav({ collapsed }: { collapsed: boolean }) {
  const { data } = useQuery(orpc.views.list.queryOptions({}))
  const saved = data?.items ?? []
  const loc = useRouterState({ select: (s) => s.location })
  const del = useAction((id: string) => client.views.delete({ id }), { invalidate: [orpc.views.list.key()] })

  // Views need their labels — hidden while the sidebar is collapsed.
  if (collapsed) return null

  const isActive = (route: string, search: Record<string, unknown>) =>
    loc.pathname === route &&
    Object.entries(search).every(([k, v]) => JSON.stringify((loc.search as Record<string, unknown>)?.[k]) === JSON.stringify(v))

  return (
    <div className="pt-3 mt-2 border-t border-border/60 space-y-0.5">
      <p className="px-1 pb-1 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground/45">Views</p>

      {SMART_VIEWS.map((v) => {
        const active = isActive(v.route, v.search)
        return (
          <Link
            key={v.label}
            to={v.route as never}
            search={v.search as never}
            className={rowClass(active)}
            style={active ? activeStyle : undefined}
          >
            <v.icon size={15} strokeWidth={active ? 2.2 : 1.8} className="shrink-0" />
            <span className="truncate">{v.label}</span>
          </Link>
        )
      })}

      {saved.length > 0 && <div className="h-px bg-border/40 my-1.5 mx-2" />}

      {saved.map((v) => {
        const active = isActive(v.route, v.search)
        return (
          <div key={v.id} className="relative group/view">
            <Link
              to={v.route as never}
              search={v.search as never}
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
