import { Link, useNavigate } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { ArrowLeft, ChevronDown, ChevronRight, Check, ShieldCheck, Users, User, Lock, Copy } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { PermissionMatrix, CI_CATALOG } from '#/components/PermissionMatrix'
import { FormSelect } from '#/components/FormSelect'
import { SearchSelect } from '#/components/SearchSelect'
import { ConfirmButton } from '#/components/ConfirmButton'
import { Badge } from '#/components/Badge'
import {
  CAPABILITIES, isCapabilityActive, toggleCapability, describeEffective, permKey,
} from '#/lib/rbac/capabilities'
import type { Role, Permission } from '#/lib/api/types'

export function RoleEditor({ role }: { role?: Role }) {
  const navigate = useNavigate()
  const readOnly = !!role?.isSystem

  const { data: wsData } = useSuspenseQuery(orpc.workspaces.list.queryOptions({ input: {} }))
  const { data: envData } = useSuspenseQuery(orpc.environments.list.queryOptions({ input: {} }))
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const workspaces = wsData.items
  const environments = envData.items
  const templates = rolesData.items.filter((r) => r.id !== role?.id)

  const [name, setName] = useState(role?.name ?? '')
  const [description, setDescription] = useState(role?.description ?? '')
  const [permissions, setPermissions] = useState<Permission[]>(role?.permissions ?? [])
  const [wsSpecific, setWsSpecific] = useState(!!role && role.workspaces.length > 0)
  const [selectedWs, setSelectedWs] = useState<Set<string>>(new Set(role?.workspaces ?? []))
  const [envSpecific, setEnvSpecific] = useState(!!role && role.environments.length > 0)
  const [selectedEnv, setSelectedEnv] = useState<Set<string>>(new Set(role?.environments ?? []))
  const [showAdvanced, setShowAdvanced] = useState(false)

  const permSet = new Set(permissions.map(permKey))
  const slug = role?.slug ?? name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')
  const showScope = permissions.some((p) => p.object in CI_CATALOG)

  const save = useAction(
    (data: { name: string; slug: string; description?: string; permissions: Permission[]; workspaces: string[]; environments: string[] }) =>
      role ? client.roles.update({ id: role.id, ...data }) : client.roles.create(data),
    { invalidate: [orpc.roles.list.key()], onSuccess: () => navigate({ to: '/settings/roles' }) },
  )

  function applyTemplate(roleSlug: string) {
    const t = templates.find((r) => r.slug === roleSlug)
    if (!t) return
    setPermissions(t.permissions.filter((p) => !(p.object === '*' && p.action === '*')))
    setWsSpecific(t.workspaces.length > 0); setSelectedWs(new Set(t.workspaces))
    setEnvSpecific(t.environments.length > 0); setSelectedEnv(new Set(t.environments))
  }

  function setCap(capId: string, on: boolean) {
    const cap = CAPABILITIES.find((c) => c.id === capId)
    if (cap) setPermissions((prev) => toggleCapability(cap, prev, on))
  }

  function toggleScope(set: Set<string>, setFn: (s: Set<string>) => void, k: string) {
    setFn(new Set(set.has(k) ? [...set].filter((x) => x !== k) : [...set, k]))
  }

  function handleSave() {
    if (readOnly || !name.trim() || permissions.length === 0) return
    save.mutate({
      name: name.trim(), slug,
      description: description.trim() || undefined,
      permissions,
      workspaces: wsSpecific ? [...selectedWs] : [],
      environments: envSpecific ? [...selectedEnv] : [],
    })
  }

  const ciCaps = CAPABILITIES.filter((c) => c.domain === 'CI')
  const adminCaps = CAPABILITIES.filter((c) => c.domain === 'Admin')

  return (
    <div className="space-y-6 rise-in">
      {/* Header */}
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <Link to="/settings/roles" className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors mb-1.5">
            <ArrowLeft size={13} /> Roles
          </Link>
          <h2 className="display-title text-lg text-foreground flex items-center gap-2">
            {role ? role.name : 'New role'}
            {readOnly && <Badge variant="primary">System</Badge>}
          </h2>
          <p className="text-muted-foreground text-xs mt-0.5">
            {readOnly ? 'Built-in role — view only.' : 'Define what this role can do, and where.'}
          </p>
        </div>
        {!readOnly && (
          <div className="flex items-center gap-2 shrink-0">
            <Link to="/settings/roles" className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
              Cancel
            </Link>
            <button
              type="button" onClick={handleSave}
              disabled={save.isPending || !name.trim() || permissions.length === 0}
              className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
            >
              {role ? 'Save changes' : 'Create role'}
            </button>
          </div>
        )}
      </div>

      {/* Quick-start: only when creating — copy a base role, then tweak. */}
      {!role && templates.length > 0 && (
        <div
          className="rounded-xl border border-primary/20 p-4 flex flex-col sm:flex-row sm:items-center gap-3"
          style={{ background: 'color-mix(in oklab, var(--primary) 6%, var(--surface))' }}
        >
          <div className="flex items-center gap-2.5 flex-1 min-w-0">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg shrink-0" style={{ background: 'color-mix(in oklab, var(--primary) 14%, transparent)', color: 'var(--primary)' }}>
              <Copy size={15} />
            </div>
            <div className="min-w-0">
              <p className="text-sm font-medium text-foreground">Start from an existing role</p>
              <p className="text-xs text-muted-foreground">Copy its permissions and scope as a starting point — optional.</p>
            </div>
          </div>
          <div className="sm:w-60 shrink-0">
            <FormSelect
              value=""
              onChange={applyTemplate}
              placeholder="Choose a role to copy…"
              options={templates.map((r) => ({ key: r.slug, label: r.name }))}
            />
          </div>
        </div>
      )}

      <div className="grid lg:grid-cols-[1fr_300px] gap-6 items-start">
        {/* ── Left: the builder ── */}
        <div className="space-y-6 min-w-0">
          {/* Identity */}
          <section className="island-shell p-5 space-y-4">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Name {!readOnly && <span className="text-destructive">*</span>}</label>
              <input
                type="text" value={name} disabled={readOnly}
                onChange={(e) => setName(e.target.value)}
                placeholder="Prod Release Manager"
                className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40 disabled:opacity-60"
              />
              {slug && <p className="text-[11px] text-muted-foreground font-mono">slug: {slug}</p>}
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Description</label>
              <textarea
                value={description} disabled={readOnly}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="What is this role for?" rows={2}
                className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40 resize-none disabled:opacity-60"
              />
            </div>
          </section>

          {/* Capabilities */}
          <section className="island-shell p-5 space-y-4">
            <div>
              <h3 className="text-sm font-semibold text-foreground">Capabilities</h3>
              <p className="text-xs text-muted-foreground mt-0.5">Toggle what this role can do. Need finer control? Use the advanced matrix below.</p>
            </div>
            <CapabilityGroup title="CI" caps={ciCaps} permSet={permSet} readOnly={readOnly} onSet={setCap} />
            <CapabilityGroup title="Admin" caps={adminCaps} permSet={permSet} readOnly={readOnly} onSet={setCap} />
          </section>

          {/* Advanced matrix */}
          <section className="island-shell !p-0 overflow-hidden">
            <button
              type="button" onClick={() => setShowAdvanced(!showAdvanced)}
              className="w-full flex items-center gap-2 px-5 py-3 text-left hover:bg-accent/30 transition-colors"
            >
              {showAdvanced ? <ChevronDown size={14} className="text-muted-foreground" /> : <ChevronRight size={14} className="text-muted-foreground" />}
              <span className="text-sm font-semibold text-foreground">Advanced</span>
              <span className="text-xs text-muted-foreground">per-resource permission matrix</span>
            </button>
            {showAdvanced && (
              <div className="px-5 pb-5 pt-1 border-t border-border/50">
                <PermissionMatrix permissions={permissions} editable={!readOnly} onChange={readOnly ? undefined : setPermissions} />
              </div>
            )}
          </section>

          {/* Scope */}
          {showScope && (
            <section className="island-shell p-5 space-y-4">
              <div>
                <h3 className="text-sm font-semibold text-foreground">CI scope</h3>
                <p className="text-xs text-muted-foreground mt-0.5">Limit CI permissions to specific workspaces and/or environments. Admin permissions are always platform-wide.</p>
              </div>
              <ScopePicker
                label="Workspaces" specific={wsSpecific} setSpecific={setWsSpecific} readOnly={readOnly}
                options={workspaces.map((w) => ({ key: w.slug, label: w.name }))}
                selected={selectedWs} onToggle={(k) => toggleScope(selectedWs, setSelectedWs, k)} variant="primary"
              />
              <ScopePicker
                label="Environments" specific={envSpecific} setSpecific={setEnvSpecific} readOnly={readOnly}
                options={environments.map((e) => ({ key: e.name, label: e.name }))}
                selected={selectedEnv} onToggle={(k) => toggleScope(selectedEnv, setSelectedEnv, k)} variant="success"
              />
            </section>
          )}

          {/* Members (edit mode only) */}
          {role && <MembersSection roleSlug={role.slug} />}
        </div>

        {/* ── Right: live effective access ── */}
        <aside className="lg:sticky lg:top-20">
          <EffectivePanel
            permissions={permissions}
            wsSpecific={wsSpecific} selectedWs={[...selectedWs]}
            envSpecific={envSpecific} selectedEnv={[...selectedEnv]}
          />
        </aside>
      </div>
    </div>
  )
}

function CapabilityGroup({ title, caps, permSet, readOnly, onSet }: {
  title: string
  caps: typeof CAPABILITIES
  permSet: Set<string>
  readOnly: boolean
  onSet: (id: string, on: boolean) => void
}) {
  return (
    <div className="space-y-2">
      <h4 className="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">{title}</h4>
      <div className="grid sm:grid-cols-2 gap-2">
        {caps.map((cap) => {
          const active = isCapabilityActive(cap, permSet)
          return (
            <button
              key={cap.id}
              type="button"
              disabled={readOnly}
              onClick={() => onSet(cap.id, !active)}
              className={`flex items-start gap-2.5 rounded-lg border p-3 text-left transition-colors disabled:cursor-default ${
                active ? 'border-primary/40 bg-primary/5' : 'border-border hover:border-muted-foreground/40'
              }`}
            >
              <span className={`mt-0.5 w-4 h-4 rounded border flex items-center justify-center shrink-0 ${active ? 'bg-primary border-primary text-white' : 'border-border'}`}>
                {active && <Check size={11} />}
              </span>
              <span className="min-w-0">
                <span className={`block text-xs font-medium ${active ? 'text-foreground' : 'text-foreground'}`}>{cap.label}</span>
                {cap.description && <span className="block text-[11px] text-muted-foreground mt-0.5">{cap.description}</span>}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

function ScopePicker({ label, specific, setSpecific, readOnly, options, selected, onToggle, variant }: {
  label: string
  specific: boolean
  setSpecific: (b: boolean) => void
  readOnly: boolean
  options: { key: string; label: string }[]
  selected: Set<string>
  onToggle: (k: string) => void
  variant: 'primary' | 'success'
}) {
  const onClasses = variant === 'primary'
    ? 'bg-primary/15 text-primary ring-1 ring-primary/20'
    : 'bg-success/10 text-success ring-1 ring-success/20'
  return (
    <div className="space-y-2">
      <label className="text-xs font-medium text-foreground">{label}</label>
      <div className="flex items-center gap-4">
        <label className="flex items-center gap-2 text-xs cursor-pointer">
          <input type="radio" checked={!specific} disabled={readOnly} onChange={() => setSpecific(false)} className="accent-primary" />
          <span className="text-muted-foreground">All {label.toLowerCase()}</span>
        </label>
        <label className="flex items-center gap-2 text-xs cursor-pointer">
          <input type="radio" checked={specific} disabled={readOnly} onChange={() => setSpecific(true)} className="accent-primary" />
          <span className="text-muted-foreground">Specific</span>
        </label>
      </div>
      {specific && (
        <div className="flex flex-wrap gap-1.5 pt-1">
          {options.map((o) => (
            <button
              key={o.key} type="button" disabled={readOnly} onClick={() => onToggle(o.key)}
              className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                selected.has(o.key) ? onClasses : 'text-muted-foreground border border-border hover:text-foreground hover:bg-accent'
              }`}
            >
              {o.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

function EffectivePanel({ permissions, wsSpecific, selectedWs, envSpecific, selectedEnv }: {
  permissions: Permission[]
  wsSpecific: boolean
  selectedWs: string[]
  envSpecific: boolean
  selectedEnv: string[]
}) {
  const eff = describeEffective(permissions)
  return (
    <div className="island-shell p-4 space-y-3">
      <div className="flex items-center gap-2">
        <ShieldCheck size={14} className="text-primary" />
        <h3 className="text-sm font-semibold text-foreground">Effective access</h3>
      </div>

      {permissions.length === 0 ? (
        <p className="text-xs text-muted-foreground">No permissions selected yet.</p>
      ) : (
        <>
          <p className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">This role can</p>
          <ul className="space-y-1">
            {eff.lines.map((line) => (
              <li key={line} className="flex items-start gap-1.5 text-xs text-foreground">
                <Check size={12} className="text-success mt-0.5 shrink-0" /> {line}
              </li>
            ))}
            {eff.extra > 0 && (
              <li className="text-[11px] text-muted-foreground pl-[18px]">+{eff.extra} more granular permission{eff.extra > 1 ? 's' : ''}</li>
            )}
          </ul>
        </>
      )}

      <div className="border-t border-border/50 pt-3 space-y-1.5">
        <p className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">Scope</p>
        <p className="text-xs text-foreground">
          Workspaces: {wsSpecific ? (selectedWs.length ? selectedWs.join(', ') : 'none selected') : 'all'}
        </p>
        <p className="text-xs text-foreground">
          Environments: {envSpecific ? (selectedEnv.length ? selectedEnv.join(', ') : 'none selected') : 'all'}
        </p>
      </div>
    </div>
  )
}

// ── Members: who holds this role ──
function MembersSection({ roleSlug }: { roleSlug: string }) {
  const { data: assignmentsData } = useSuspenseQuery(orpc.roles.assignments.list.queryOptions({ input: {} }))
  const { data: usersData } = useSuspenseQuery(orpc.users.list.queryOptions({ input: {} }))
  const { data: teamsData } = useSuspenseQuery(orpc.teams.list.queryOptions({ input: {} }))
  const members = assignmentsData.items.filter((a) => a.role === roleSlug)

  const [adding, setAdding] = useState(false)
  const [selUsers, setSelUsers] = useState<Set<string>>(new Set())
  const [selTeams, setSelTeams] = useState<Set<string>>(new Set())

  const invalidate = [orpc.roles.assignments.list.key()]
  const assign = useAction((subjects: string[]) => client.roles.assignments.create({ subjects, role: roleSlug }), {
    invalidate, onSuccess: () => { setAdding(false); setSelUsers(new Set()); setSelTeams(new Set()) },
  })
  const remove = useAction((subject: string) => client.roles.assignments.delete({ subject, role: roleSlug }), { invalidate })

  const userItems = usersData.items.map((u) => ({ id: u.email, label: u.name ?? u.email, detail: u.email, icon: <User size={13} className="text-muted-foreground shrink-0" /> }))
  const teamItems = teamsData.items.map((t) => ({ id: `team:${t.slug}`, label: t.name, detail: `${t.memberCount} members`, icon: <Users size={13} className="text-muted-foreground shrink-0" /> }))
  const total = selUsers.size + selTeams.size

  return (
    <section className="island-shell p-5 space-y-3">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-semibold text-foreground">Members</h3>
          <p className="text-xs text-muted-foreground mt-0.5">{members.length} subject{members.length !== 1 ? 's' : ''} with this role</p>
        </div>
        {!adding && (
          <button type="button" onClick={() => setAdding(true)}
            className="rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
            Assign
          </button>
        )}
      </div>

      {adding && (
        <div className="rounded-lg border border-border p-3 space-y-3">
          <SearchSelect items={userItems} selected={selUsers} onChange={setSelUsers} placeholder="Search users…" />
          <SearchSelect items={teamItems} selected={selTeams} onChange={setSelTeams} placeholder="Search teams…" />
          <div className="flex items-center gap-2">
            <button type="button" disabled={total === 0 || assign.isPending} onClick={() => assign.mutate([...selUsers, ...selTeams])}
              className="rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
              Assign {total > 0 ? total : ''}
            </button>
            <button type="button" onClick={() => setAdding(false)} className="rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
              Cancel
            </button>
          </div>
        </div>
      )}

      {members.length === 0 ? (
        <p className="text-xs text-muted-foreground opacity-60 flex items-center gap-1.5"><Lock size={11} /> No one has this role yet.</p>
      ) : (
        <div className="divide-y divide-border">
          {members.map((m) => {
            const isTeam = m.subject.startsWith('team:')
            return (
              <div key={m.subject} className="flex items-center gap-2.5 py-2.5">
                {isTeam ? <Users size={14} className="text-muted-foreground shrink-0" /> : <User size={14} className="text-muted-foreground shrink-0" />}
                <span className="text-sm text-foreground font-mono truncate flex-1">{m.subject}</span>
                <ConfirmButton onConfirm={() => remove.mutate(m.subject)} title="Remove" />
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}
