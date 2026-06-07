import { Link, useRouterState } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { GitMerge, Workflow, Gauge } from 'lucide-react'
import type { ComponentType } from 'react'
import { orpc } from '#/lib/orpc'

type Capability = {
  id: string
  name: string
  enabled: boolean
  status: 'enabled' | 'coming_soon' | 'disabled'
}

// Per-product presentation. `to` is `as const` so the typed router accepts it.
const META = {
  ci: { icon: GitMerge, to: '/ci' as const, tagline: 'Pipelines' },
  workflows: { icon: Workflow, to: '/workflows' as const, tagline: 'Automations' },
  loadtest: { icon: Gauge, to: '/loadtest' as const, tagline: 'Performance' },
} satisfies Record<string, { icon: ComponentType<{ size?: number; strokeWidth?: number }>; to: string; tagline: string }>

export function activeCapabilityID(pathname: string): string {
  if (pathname.startsWith('/workflows')) return 'workflows'
  if (pathname.startsWith('/loadtest')) return 'loadtest'
  if (pathname.startsWith('/settings')) return 'admin'
  return 'ci'
}

// CapabilitySwitcher is the unified app's top-level product selector. It renders
// only enabled products (and "coming soon" ones as dimmed teasers), driven by
// GET /api/v1/capabilities.
export function CapabilitySwitcher({ collapsed = false }: { collapsed?: boolean }) {
  const { data } = useQuery(orpc.capabilities.get.queryOptions({}))
  const products = (data as { products: Capability[] } | undefined)?.products
  const path = useRouterState().location.pathname
  const active = activeCapabilityID(path)
  if (!products || products.length === 0) return null

  return (
    <div className={collapsed ? 'px-1.5 py-2 space-y-1' : 'px-3 py-3 space-y-1'}>
      {!collapsed && (
        <p className="px-1 pb-1.5 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground/45">
          Product
        </p>
      )}
      {products.map((p) => {
        const meta = META[p.id as keyof typeof META]
        if (!meta) return null
        const Icon = meta.icon
        const isActive = active === p.id
        const comingSoon = !p.enabled || p.status === 'coming_soon'

        const inner = (
          <>
            <span
              className="flex h-7 w-7 items-center justify-center rounded-md shrink-0 transition-colors"
              style={
                isActive
                  ? { background: 'var(--primary)', color: '#fff' }
                  : { background: 'color-mix(in oklab, var(--primary) 12%, transparent)', color: 'var(--primary)' }
              }
            >
              <Icon size={15} strokeWidth={2} />
            </span>
            {!collapsed && (
              <span className="min-w-0 flex-1">
                <span className="flex items-center gap-1.5">
                  <span className="display-title text-sm font-bold tracking-tight text-foreground truncate">{p.name}</span>
                  {comingSoon && (
                    <span
                      className="rounded-full px-1.5 py-px text-[9px] font-bold uppercase tracking-wider"
                      style={{ background: 'color-mix(in oklab, var(--muted-foreground) 16%, transparent)', color: 'var(--muted-foreground)' }}
                    >
                      Soon
                    </span>
                  )}
                </span>
                <span className="block text-[11px] text-muted-foreground truncate">{meta.tagline}</span>
              </span>
            )}
          </>
        )

        const base = `group relative flex items-center rounded-lg transition-all duration-150 ${
          collapsed ? 'justify-center px-1.5 py-1.5' : 'gap-2.5 px-2 py-1.5'
        }`

        if (comingSoon) {
          return (
            <div key={p.id} title={`${p.name} — coming soon`} className={`${base} opacity-55 cursor-not-allowed`}>
              {inner}
            </div>
          )
        }

        return (
          <Link
            key={p.id}
            to={meta.to}
            title={collapsed ? p.name : undefined}
            className={`${base} ${isActive ? '' : 'hover:bg-[var(--link-bg-hover)]'}`}
            style={
              isActive
                ? {
                    background: 'color-mix(in oklab, var(--primary) 10%, transparent)',
                    boxShadow: 'inset 0 0 0 1px color-mix(in oklab, var(--primary) 25%, transparent)',
                  }
                : undefined
            }
          >
            {isActive && !collapsed && (
              <span className="absolute left-0 top-1/2 h-5 w-[3px] -translate-y-1/2 rounded-r-full" style={{ background: 'var(--primary)' }} />
            )}
            {inner}
          </Link>
        )
      })}
    </div>
  )
}
