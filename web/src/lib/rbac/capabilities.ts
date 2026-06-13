import type { Permission } from '#/lib/api/types'

// Capability bundles — friendly, named groupings of low-level permissions. They
// are a *view* over the permission array (the real source of truth), so the
// capability toggles and the advanced object×action matrix always stay in sync.
export interface Capability {
  id: string
  label: string
  description: string
  domain: 'CI' | 'Admin'
  grants: Permission[]
}

const p = (object: string, action: string): Permission => ({ object, action })

export const CAPABILITIES: Capability[] = [
  // ── CI ──
  { id: 'ci-view', label: 'View CI', description: 'See projects, runs, and gate status', domain: 'CI', grants: [p('project', 'read'), p('run', 'read')] },
  { id: 'ci-trigger', label: 'Trigger runs', description: 'Start pipeline runs', domain: 'CI', grants: [p('run', 'trigger')] },
  { id: 'ci-cancel', label: 'Cancel runs', description: 'Stop in-progress runs', domain: 'CI', grants: [p('run', 'cancel')] },
  { id: 'ci-gates', label: 'Approve & reject deploys', description: 'Make gate decisions on protected environments', domain: 'CI', grants: [p('gate', 'approve'), p('gate', 'reject')] },
  { id: 'ci-projects', label: 'Manage projects', description: 'Edit project configuration', domain: 'CI', grants: [p('project', 'write')] },
  // ── Admin ──
  { id: 'adm-secrets', label: 'Manage secrets & variables', description: 'Create and edit environment variables and secrets', domain: 'Admin', grants: [p('secret', 'manage')] },
  { id: 'adm-envs', label: 'Manage environments', description: 'Create environments and protection rules', domain: 'Admin', grants: [p('environment', 'manage')] },
  { id: 'adm-workspaces', label: 'Manage workspaces', description: 'Create and delete workspaces', domain: 'Admin', grants: [p('workspace', 'manage')] },
  { id: 'adm-teams', label: 'Manage teams', description: 'Create teams and edit membership', domain: 'Admin', grants: [p('team', 'manage')] },
  { id: 'adm-roles', label: 'Manage roles & access', description: 'Create roles and assign them', domain: 'Admin', grants: [p('role', 'manage')] },
  { id: 'adm-tags', label: 'Manage tags', description: 'Curate the project tag registry', domain: 'Admin', grants: [p('tag', 'manage')] },
  { id: 'adm-runners', label: 'Manage runners', description: 'Configure runner pools', domain: 'Admin', grants: [p('runner', 'manage')] },
  { id: 'adm-connections', label: 'Manage connections', description: 'Link and remove forge connections', domain: 'Admin', grants: [p('connection', 'manage')] },
  { id: 'adm-apikeys', label: 'Manage API keys', description: 'Issue and revoke org API keys', domain: 'Admin', grants: [p('apikey', 'manage')] },
  { id: 'adm-audit', label: 'View audit log', description: 'Read the org audit trail', domain: 'Admin', grants: [p('audit', 'read')] },
]

export const permKey = (perm: Permission): string => `${perm.object}:${perm.action}`

export function isCapabilityActive(cap: Capability, permSet: Set<string>): boolean {
  if (permSet.has('*:*')) return true
  return cap.grants.every((g) => permSet.has(permKey(g)))
}

// Toggle a capability on/off against the permission array (union / difference).
export function toggleCapability(cap: Capability, perms: Permission[], on: boolean): Permission[] {
  const map = new Map(perms.map((perm) => [permKey(perm), perm]))
  for (const g of cap.grants) {
    if (on) map.set(permKey(g), g)
    else map.delete(permKey(g))
  }
  return [...map.values()]
}

// Plain-English summary of what a permission set grants: the active capability
// labels, plus a count of any granular permissions not covered by a bundle.
export function describeEffective(perms: Permission[]): { lines: string[]; extra: number; wildcard: boolean } {
  const set = new Set(perms.map(permKey))
  if (set.has('*:*')) return { lines: ['Full access — every permission'], extra: 0, wildcard: true }

  const covered = new Set<string>()
  const lines: string[] = []
  for (const cap of CAPABILITIES) {
    if (isCapabilityActive(cap, set)) {
      lines.push(cap.label)
      cap.grants.forEach((g) => covered.add(permKey(g)))
    }
  }
  const extra = [...set].filter((k) => !covered.has(k)).length
  return { lines, extra, wildcard: false }
}
