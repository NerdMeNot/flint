import { useEffect, useState } from 'react'
import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { Plus, Trash2, Check, Loader2, ArrowRight, Users } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { FormSelect } from '#/components/FormSelect'
import { Switch } from '#/components/ui/switch'

interface Row {
  groupName: string
  roleId: string
}

const inputClass =
  'w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow'

/**
 * Org-level mapping of IdP group names to Flint roles, applied at each user's
 * login (GitLab "SAML Group Links" style). Strict mode makes group membership
 * the sole source of access (deny-by-default).
 */
export function GroupRoleMappings() {
  const { data } = useQuery(orpc.auth.providers.groupMappings.get.queryOptions({ input: {} }))
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items

  const [rows, setRows] = useState<Row[]>([])
  const [strict, setStrict] = useState(false)
  const [seeded, setSeeded] = useState(false)

  // Seed local editing state once the server data arrives.
  useEffect(() => {
    if (data && !seeded) {
      setRows(data.mappings.map((m) => ({ groupName: m.groupName, roleId: m.roleId })))
      setStrict(data.strict)
      setSeeded(true)
    }
  }, [data, seeded])

  const save = useAction(
    () =>
      client.auth.providers.groupMappings.save({
        strict,
        mappings: rows.filter((r) => r.groupName.trim() && r.roleId),
      }),
    { invalidate: [orpc.auth.providers.groupMappings.get.key()] },
  )

  const roleOptions = roles.map((r) => ({ key: r.id, label: r.name }))
  const setRow = (i: number, patch: Partial<Row>) =>
    setRows((rs) => rs.map((r, idx) => (idx === i ? { ...r, ...patch } : r)))

  return (
    <div className="island-shell p-5 space-y-4">
      <div className="flex items-center gap-2">
        <Users size={16} className="text-muted-foreground" />
        <h2 className="text-base font-semibold text-foreground">Group → role mapping</h2>
      </div>
      <p className="text-sm text-muted-foreground -mt-1">
        Grant Flint roles based on a user's IdP group membership. Applied on each login; manual role
        assignments are never affected.
      </p>

      {rows.length === 0 ? (
        <p className="text-sm text-muted-foreground rounded-lg border border-dashed border-border px-3 py-4 text-center">
          No mappings yet. Add one to grant a role to everyone in an IdP group.
        </p>
      ) : (
        <div className="space-y-2">
          {rows.map((row, i) => (
            <div key={i} className="flex items-center gap-2">
              <input
                className={inputClass}
                placeholder="IdP group name (exact)"
                value={row.groupName}
                onChange={(e) => setRow(i, { groupName: e.target.value })}
              />
              <ArrowRight size={14} className="shrink-0 text-muted-foreground" />
              <div className="w-44 shrink-0">
                <FormSelect
                  value={row.roleId}
                  onChange={(v) => setRow(i, { roleId: v })}
                  options={roleOptions}
                  placeholder="Select role"
                />
              </div>
              <button
                type="button"
                onClick={() => setRows((rs) => rs.filter((_, idx) => idx !== i))}
                aria-label="Remove mapping"
                className="shrink-0 flex h-8 w-8 items-center justify-center rounded text-muted-foreground/60 hover:text-destructive hover:bg-destructive-subtle transition-colors"
              >
                <Trash2 size={14} />
              </button>
            </div>
          ))}
        </div>
      )}

      <button
        type="button"
        onClick={() => setRows((rs) => [...rs, { groupName: '', roleId: '' }])}
        className="flex items-center gap-1.5 text-sm font-medium text-primary hover:underline"
      >
        <Plus size={14} /> Add mapping
      </button>

      <div className="flex items-start gap-3 pt-3 border-t border-border">
        <Switch checked={strict} onCheckedChange={setStrict} className="mt-0.5" />
        <div className="text-sm cursor-pointer" onClick={() => setStrict((s) => !s)}>
          <span className="font-medium text-foreground">Strict mode</span>
          <span className="block text-muted-foreground">
            Only grant access to users whose IdP groups map to a role. SSO users with no matching
            group get no default role (deny-by-default).
          </span>
        </div>
      </div>

      <div className="flex items-center gap-3 pt-1">
        <button
          onClick={() => save.mutate(undefined)}
          disabled={save.isPending}
          className="flex items-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm px-4 py-2 hover:opacity-90 disabled:opacity-50 transition-opacity"
        >
          {save.isPending ? <Loader2 size={14} className="animate-spin" /> : <Check size={14} />}
          Save mappings
        </button>
        {save.isSuccess && <span className="text-sm text-[var(--success)]">Saved</span>}
        {save.isError && <span className="text-sm text-destructive">{save.error.message || 'Save failed'}</span>}
      </div>
    </div>
  )
}
