import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Users, Plus } from 'lucide-react'
import { useState } from 'react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { Modal } from '#/components/Modal'
import { Badge } from '#/components/Badge'

export const Route = createFileRoute('/settings/teams/')({
  component: TeamsPage,
})

const slugify = (s: string) =>
  s.toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')

function NewTeamModal({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [slugTouched, setSlugTouched] = useState(false)
  const effectiveSlug = slugTouched ? slug : slugify(name)

  const create = useAction(client.teams.create, {
    invalidate: [orpc.teams.list.key()],
    onSuccess: onClose,
  })

  const inputClass =
    'w-full rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring/40'

  return (
    <Modal open onClose={onClose} title="New team" subtitle="Create an internal team">
      <div className="px-5 py-4 space-y-4">
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-foreground">Name</label>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Platform Team"
            className={inputClass}
            autoFocus
          />
        </div>
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-foreground">Slug</label>
          <input
            value={effectiveSlug}
            onChange={(e) => { setSlugTouched(true); setSlug(e.target.value) }}
            placeholder="platform-team"
            className={`${inputClass} font-mono`}
          />
        </div>
        {create.isError && (
          <p className="text-xs text-red-500">Could not create team — the name or slug may already be taken.</p>
        )}
      </div>
      <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border">
        <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
          Cancel
        </button>
        <button
          type="button"
          disabled={!name.trim() || !effectiveSlug || create.isPending}
          onClick={() => create.mutate({ name: name.trim(), slug: effectiveSlug })}
          className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
          style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
        >
          {create.isPending ? 'Creating…' : 'Create team'}
        </button>
      </div>
    </Modal>
  )
}

function TeamsPage() {
  const [showNew, setShowNew] = useState(false)
  const { data: teamsData } = useSuspenseQuery(orpc.teams.list.queryOptions({ input: {} }))
  const teams = teamsData.items
  const { data: assignmentsData } = useSuspenseQuery(orpc.roles.assignments.list.queryOptions({ input: {} }))
  const assignments = assignmentsData.items
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items

  const roleMap = new Map(roles.map((r) => [r.slug, r]))
  const teamRoles = new Map<string, typeof roles>()
  for (const a of assignments) {
    if (!a.subject.startsWith('team:')) continue
    const slug = a.subject.replace('team:', '')
    const role = roleMap.get(a.role)
    if (role) {
      const list = teamRoles.get(slug) ?? []
      list.push(role)
      teamRoles.set(slug, list)
    }
  }

  return (
    <div className="space-y-5">
      {showNew && <NewTeamModal onClose={() => setShowNew(false)} />}
      <div className="flex items-start justify-between">
        <div>
          <h2 className="display-title text-lg text-foreground">Teams</h2>
          <p className="text-muted-foreground text-xs mt-0.5">
            {teams.length} {teams.length === 1 ? 'team' : 'teams'}
          </p>
        </div>
        <button
          type="button"
          onClick={() => setShowNew(true)}
          className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors"
          style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
        >
          <Plus size={14} />
          New team
        </button>
      </div>

      {teams.length === 0 ? (
        <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
          <Users size={32} strokeWidth={1.2} />
          <span className="text-sm">No teams configured yet.</span>
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {teams.map((team, i) => (
            <Link
              key={team.id}
              to="/settings/teams/$id"
              params={{ id: team.id }}
              className="feature-card rise-in p-5 space-y-3 flex flex-col hover:bg-accent/50 transition-colors group"
              style={{ animationDelay: `${i * 50 + 30}ms` }}
            >
              <div className="flex items-start justify-between">
                <div className="min-w-0">
                  <h3 className="font-semibold text-sm text-foreground group-hover:text-primary transition-colors truncate">{team.name}</h3>
                  <p className="text-xs text-muted-foreground font-mono truncate">{team.slug}</p>
                </div>
                <Badge variant={team.source === 'idp' ? 'primary' : 'neutral'}>
                  {team.source === 'idp' ? 'IdP' : 'Internal'}
                </Badge>
              </div>

              {team.idpGroup && (
                <p className="text-[12px] text-muted-foreground opacity-60 font-mono truncate">
                  group: {team.idpGroup}
                </p>
              )}

              {(() => {
                const tRoles = teamRoles.get(team.slug) ?? []
                return tRoles.length > 0 ? (
                  <div className="flex flex-wrap gap-1">
                    {tRoles.map((r) => (
                      <span key={r.slug} className="rounded-md px-1.5 py-0.5 text-[11px] font-medium bg-secondary text-foreground border border-border">
                        {r.name}
                      </span>
                    ))}
                  </div>
                ) : null
              })()}

              <div className="flex items-center gap-1.5 text-xs text-muted-foreground mt-auto">
                <Users size={12} />
                {team.memberCount} {team.memberCount === 1 ? 'member' : 'members'}
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}
