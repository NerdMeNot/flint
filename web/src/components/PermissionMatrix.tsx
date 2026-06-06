import { Check, Lock } from 'lucide-react'

// ---------------------------------------------------------------------------
// Permission catalog — two domains
// ---------------------------------------------------------------------------

export const ADMIN_CATALOG: Record<string, string[]> = {
  workspace: ['read', 'manage'],
  team: ['read', 'manage'],
  environment: ['read', 'manage'],
  runner: ['read', 'manage'],
  connection: ['read', 'manage'],
  apikey: ['read', 'manage'],
  secret: ['read', 'manage'],
  role: ['read', 'manage'],
  audit: ['read'],
}

export const CI_CATALOG: Record<string, string[]> = {
  project: ['read', 'write'],
  run: ['read', 'trigger', 'cancel'],
  gate: ['approve', 'reject'],
}

export const ADMIN_OBJECTS = Object.keys(ADMIN_CATALOG)
export const ADMIN_ACTIONS = ['read', 'manage']
export const CI_OBJECTS = Object.keys(CI_CATALOG)
export const CI_ACTIONS = ['read', 'write', 'trigger', 'cancel', 'approve', 'reject']

// ---------------------------------------------------------------------------
// Implication rules — higher permissions imply lower ones
// ---------------------------------------------------------------------------

const IMPLICATIONS: Record<string, string[]> = {
  'project:write': ['project:read'],
  'run:trigger': ['project:read'],
  'run:cancel': ['run:read', 'project:read'],
  'gate:approve': ['run:read', 'project:read'],
  'gate:reject': ['run:read', 'project:read'],
  // Admin: manage implies read
  'workspace:manage': ['workspace:read'],
  'team:manage': ['team:read'],
  'environment:manage': ['environment:read'],
  'runner:manage': ['runner:read'],
  'connection:manage': ['connection:read'],
  'apikey:manage': ['apikey:read'],
  'secret:manage': ['secret:read'],
  'role:manage': ['role:read'],
}

/** Compute the full set of implied permissions from explicit selections */
function computeImplied(explicit: Set<string>): Set<string> {
  const implied = new Set<string>()
  for (const perm of explicit) {
    const deps = IMPLICATIONS[perm]
    if (deps) {
      for (const dep of deps) {
        if (!explicit.has(dep)) implied.add(dep)
      }
    }
  }
  return implied
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface Permission {
  object: string
  action: string
}

interface PermissionMatrixProps {
  permissions: Permission[]
  editable?: boolean
  onChange?: (permissions: Permission[]) => void
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function PermissionMatrix({ permissions, editable, onChange }: PermissionMatrixProps) {
  const permSet = new Set(permissions.map((p) => `${p.object}:${p.action}`))
  const isWildcard = permSet.has('*:*')
  const impliedSet = editable ? computeImplied(permSet) : new Set<string>()

  function toggle(object: string, action: string) {
    if (!editable || !onChange) return
    const key = `${object}:${action}`

    // Can't uncheck an implied permission
    if (impliedSet.has(key)) return

    if (permSet.has(key)) {
      // Unchecking — also remove any permissions that would be orphaned
      let next = permissions.filter((p) => !(p.object === object && p.action === action))
      // Recompute: if removing this perm means something that implied others is gone,
      // the implied set shrinks naturally on next render
      onChange(next)
    } else {
      onChange([...permissions, { object, action }])
    }
  }

  const hasAnyAdmin = ADMIN_OBJECTS.some((obj) =>
    ADMIN_CATALOG[obj]!.some((act) => isWildcard || permSet.has(`${obj}:${act}`) || impliedSet.has(`${obj}:${act}`)),
  )
  const hasAnyCI = CI_OBJECTS.some((obj) =>
    CI_CATALOG[obj]!.some((act) => isWildcard || permSet.has(`${obj}:${act}`) || impliedSet.has(`${obj}:${act}`)),
  )

  const showAdmin = editable || isWildcard || hasAnyAdmin
  const showCI = editable || isWildcard || hasAnyCI

  return (
    <div className="space-y-4">
      {showAdmin && (
        <div>
          <h4 className="text-[12px] font-semibold text-muted-foreground uppercase tracking-wider mb-2">
            Admin Resources
          </h4>
          <PermissionGrid
            catalog={ADMIN_CATALOG}
            actions={ADMIN_ACTIONS}
            permSet={permSet}
            impliedSet={impliedSet}
            isWildcard={isWildcard}
            editable={editable}
            onToggle={toggle}
          />
        </div>
      )}
      {showCI && (
        <div>
          <h4 className="text-[12px] font-semibold text-muted-foreground uppercase tracking-wider mb-2">
            CI Resources
          </h4>
          <PermissionGrid
            catalog={CI_CATALOG}
            actions={CI_ACTIONS}
            permSet={permSet}
            impliedSet={impliedSet}
            isWildcard={isWildcard}
            editable={editable}
            onToggle={toggle}
          />
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Grid renderer
// ---------------------------------------------------------------------------

function PermissionGrid({
  catalog,
  actions,
  permSet,
  impliedSet,
  isWildcard,
  editable,
  onToggle,
}: {
  catalog: Record<string, string[]>
  actions: string[]
  permSet: Set<string>
  impliedSet: Set<string>
  isWildcard: boolean
  editable?: boolean
  onToggle: (object: string, action: string) => void
}) {
  const objects = Object.keys(catalog)

  return (
    <table className="w-full text-xs">
      <thead>
        <tr>
          <th className="text-left py-1.5 pr-3 font-medium text-muted-foreground/60 w-[90px]" />
          {actions.map((action) => (
            <th key={action} className="text-center py-1.5 px-1.5 font-medium text-muted-foreground/60 min-w-[52px]">
              {action}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {objects.map((object) => (
          <tr key={object} className="border-t border-border/30">
            <td className="py-1.5 pr-3 font-mono font-medium text-foreground text-[12px]">{object}</td>
            {actions.map((action) => {
              const key = `${object}:${action}`
              const valid = catalog[object]?.includes(action) ?? false
              const explicit = permSet.has(key)
              const implied = impliedSet.has(key)
              const granted = isWildcard || explicit || implied

              if (!valid) {
                return (
                  <td key={action} className="text-center py-1.5 px-1.5">
                    <span className="text-muted-foreground/15">—</span>
                  </td>
                )
              }

              if (editable) {
                // Implied: shown as checked but locked — can't uncheck
                if (implied && !explicit) {
                  return (
                    <td key={action} className="text-center py-1.5 px-1.5">
                      <div
                        title="Required by another permission"
                        className="w-[18px] h-[18px] rounded border flex items-center justify-center mx-auto bg-primary/30 border-primary/40 text-primary/60 cursor-not-allowed"
                      >
                        <Lock size={9} />
                      </div>
                    </td>
                  )
                }

                return (
                  <td key={action} className="text-center py-1.5 px-1.5">
                    <button
                      type="button"
                      onClick={() => onToggle(object, action)}
                      className={`w-[18px] h-[18px] rounded border flex items-center justify-center mx-auto transition-colors ${
                        granted
                          ? 'bg-primary border-primary text-white'
                          : 'border-border/60 hover:border-muted-foreground'
                      }`}
                    >
                      {granted && <Check size={11} />}
                    </button>
                  </td>
                )
              }

              // Read-only mode
              return (
                <td key={action} className="text-center py-1.5 px-1.5">
                  {granted ? (
                    <span className={`inline-block w-2 h-2 rounded-full mx-auto ${
                      implied && !explicit ? 'bg-primary/40' : 'bg-primary'
                    }`} />
                  ) : (
                    <span className="text-muted-foreground/15">·</span>
                  )}
                </td>
              )
            })}
          </tr>
        ))}
      </tbody>
    </table>
  )
}
