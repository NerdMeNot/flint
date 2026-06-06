import { useQuery } from '@tanstack/react-query'
import { Boxes, Server, X } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { useScope } from '#/lib/scope-context'

/**
 * Slim active-scope strip rendered between the global header and main
 * content. Hidden when nothing is selected — costs zero vertical space
 * in the unscoped default. When populated, shows each selection as a
 * removable chip plus a Clear-all link.
 */
export function ScopeChips() {
  const {
    workspaces, toggleWorkspace, setWorkspaces,
    environments, toggleEnvironment, setEnvironments,
  } = useScope()
  const resolveWorkspaceLabel = useWorkspaceNames()

  const hasAny = workspaces.length > 0 || environments.length > 0
  if (!hasAny) return null

  return (
    <div
      className="border-b border-border/60 px-4 sm:px-6 lg:px-8 py-2 flex items-center gap-3 flex-wrap"
      style={{ background: 'color-mix(in oklab, var(--surface) 60%, transparent)' }}
    >
      <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground/60">
        Filtering
      </span>

      {workspaces.length > 0 && (
        <ChipGroup
          icon={<Boxes size={11} />}
          items={workspaces}
          resolveLabel={resolveWorkspaceLabel}
          onRemove={toggleWorkspace}
        />
      )}

      {environments.length > 0 && (
        <ChipGroup
          icon={<Server size={11} />}
          items={environments}
          resolveLabel={(name) => name}
          onRemove={toggleEnvironment}
        />
      )}

      <button
        type="button"
        onClick={() => { setWorkspaces([]); setEnvironments([]) }}
        className="ml-auto text-[11px] font-medium text-muted-foreground/70 hover:text-foreground transition-colors"
      >
        Clear all
      </button>
    </div>
  )
}

function ChipGroup({
  icon, items, resolveLabel, onRemove,
}: {
  icon: React.ReactNode
  items: string[]
  resolveLabel: (key: string) => string
  onRemove: (key: string) => void
}) {
  return (
    <div className="flex items-center gap-1.5 flex-wrap">
      {items.map((key) => (
        <span
          key={key}
          className="inline-flex items-center gap-1 rounded-md border border-primary/25 bg-primary/8 pl-2 pr-1 py-0.5 text-xs font-medium text-primary"
        >
          <span className="opacity-70">{icon}</span>
          <span>{resolveLabel(key)}</span>
          <button
            type="button"
            onClick={() => onRemove(key)}
            className="flex items-center justify-center w-4 h-4 rounded-sm text-primary/50 hover:text-primary hover:bg-primary/15 transition-colors"
            title="Remove"
          >
            <X size={10} />
          </button>
        </span>
      ))}
    </div>
  )
}

// Resolve workspace slugs to their display names. Hook returns a stable
// resolver function for the lifetime of the strip.
function useWorkspaceNames(): (slug: string) => string {
  const { data } = useQuery(orpc.workspaces.list.queryOptions({ input: {} }))
  const items: { slug: string; name: string }[] = (data as any)?.items ?? []
  const map = new Map(items.map((w) => [w.slug, w.name]))
  return (slug: string) => map.get(slug) ?? slug
}
