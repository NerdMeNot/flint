import { XCircle, Loader2, AlertTriangle } from 'lucide-react'

export interface ViewDef {
  id: string
  name: string
  route: string
  search: Record<string, unknown>
  icon?: typeof XCircle
}

// Built-in, zero-setup views — resolved client-side. They share the same
// /ci/views/$id route as saved views, so every view is its own destination.
export const SMART_VIEWS: ViewDef[] = [
  { id: 'failing-runs', name: 'Failing runs', route: '/ci/runs', search: { status: 'failed' }, icon: XCircle },
  { id: 'running-now', name: 'Running now', route: '/ci/runs', search: { status: 'running' }, icon: Loader2 },
  { id: 'failing-projects', name: 'Failing projects', route: '/ci/projects', search: { sort: 'failing' }, icon: AlertTriangle },
]

export function smartViewById(id: string): ViewDef | undefined {
  return SMART_VIEWS.find((v) => v.id === id)
}

export type ViewSource = 'runs' | 'projects' | 'other'

export function sourceOf(route: string): ViewSource {
  if (route.startsWith('/ci/runs')) return 'runs'
  if (route.startsWith('/ci/projects')) return 'projects'
  return 'other'
}

// Human-readable filter summary for a view's selector.
export function describeSelector(search: Record<string, unknown>): { key: string; label: string }[] {
  const out: { key: string; label: string }[] = []
  for (const [k, v] of Object.entries(search)) {
    if (v === undefined || v === '' || (Array.isArray(v) && v.length === 0)) continue
    const value = Array.isArray(v) ? v.join(', ') : String(v)
    out.push({ key: k, label: `${k}: ${value}` })
  }
  return out
}
