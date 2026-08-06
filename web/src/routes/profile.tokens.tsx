import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Key, Plus, Copy, Check } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { PageHeader } from '#/components/PageHeader'
import { EmptyState } from '#/components/EmptyState'
import { Badge } from '#/components/Badge'
import { ConfirmButton } from '#/components/ConfirmButton'
import { formatTime } from '#/lib/format-time'
import type { PersonalToken } from '#/lib/api/types'

export const Route = createFileRoute('/profile/tokens')({
  component: TokensTab,
})

function TokensTab() {
  const { data: user } = useQuery(orpc.auth.me.queryOptions({}))

  if (!user) {
    return (
      <div className="max-w-2xl space-y-6">
        <div className="h-7 w-44 rounded bg-muted animate-pulse" />
        <div className="island-shell h-32 animate-pulse" />
      </div>
    )
  }
  return <TokensList userId={user.userId} />
}

function TokensList({ userId }: { userId: string }) {
  const { data } = useQuery(orpc.personalTokens.list.queryOptions({ input: { userId } }))
  const tokens = data?.items ?? []
  const [creating, setCreating] = useState(false)
  const [newToken, setNewToken] = useState<string | null>(null)

  const del = useAction((id: string) => client.personalTokens.delete({ id }), {
    invalidate: [orpc.personalTokens.list.key()],
  })

  return (
    <div className="max-w-2xl space-y-5">
      <PageHeader
        title="Access tokens"
        subtitle="Personal tokens authenticate the API and CLI as you."
        action={
          <button
            type="button"
            onClick={() => { setCreating(true); setNewToken(null) }}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors"
            style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
          >
            <Plus size={12} /> New token
          </button>
        }
      />

      {newToken && <TokenReveal token={newToken} onDismiss={() => setNewToken(null)} />}

      {creating && (
        <CreateRow
          userId={userId}
          onClose={() => setCreating(false)}
          onCreated={(token) => { setCreating(false); setNewToken(token) }}
        />
      )}

      {tokens.length === 0 && !creating ? (
        <EmptyState icon={Key} message="No personal access tokens yet." />
      ) : tokens.length > 0 ? (
        <div className="island-shell !p-0 overflow-hidden divide-y divide-border">
          {tokens.map((t) => <TokenRow key={t.id} token={t} onDelete={() => del.mutate(t.id)} />)}
        </div>
      ) : null}
    </div>
  )
}

function TokenRow({ token, onDelete }: { token: PersonalToken; onDelete: () => void }) {
  const expired = token.expiresAt ? new Date(token.expiresAt).getTime() < Date.now() : false
  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <Key size={15} className="text-muted-foreground shrink-0" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-foreground truncate min-w-0">{token.name}</span>
          {expired && <Badge variant="danger">Expired</Badge>}
        </div>
        <p className="text-xs text-muted-foreground mt-0.5">
          Created {formatTime(token.createdAt)}
          {token.lastUsedAt ? ` · last used ${formatTime(token.lastUsedAt)}` : ' · never used'}
          {token.expiresAt && !expired ? ` · expires ${formatTime(token.expiresAt)}` : ''}
        </p>
      </div>
      <ConfirmButton onConfirm={onDelete} title="Revoke token" />
    </div>
  )
}

// One-time reveal of the freshly-minted secret.
function TokenReveal({ token, onDismiss }: { token: string; onDismiss: () => void }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="island-shell p-4 border-success space-y-2" style={{ borderColor: 'color-mix(in oklab, var(--success) 30%, var(--border))' }}>
      <p className="text-xs font-medium text-foreground">Copy your new token now — you won't be able to see it again.</p>
      <div className="flex items-center gap-2">
        <code className="flex-1 min-w-0 truncate rounded-md bg-secondary border border-border px-2.5 py-1.5 text-xs font-mono text-foreground">{token}</code>
        <button
          type="button"
          onClick={() => { void navigator.clipboard?.writeText(token); setCopied(true) }}
          className="flex items-center gap-1.5 rounded-md border border-border px-2.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors shrink-0"
        >
          {copied ? <Check size={13} className="text-success" /> : <Copy size={13} />}
          {copied ? 'Copied' : 'Copy'}
        </button>
        <button type="button" onClick={onDismiss} className="text-xs font-medium text-muted-foreground hover:text-foreground transition-colors shrink-0">Done</button>
      </div>
    </div>
  )
}

function CreateRow({ userId, onClose, onCreated }: {
  userId: string
  onClose: () => void
  onCreated: (token: string) => void
}) {
  const [name, setName] = useState('')
  const create = useAction(
    (input: { userId: string; name: string }) => client.personalTokens.create(input),
    {
      invalidate: [orpc.personalTokens.list.key()],
      onSuccess: (result) => onCreated((result as { token: string }).token),
    },
  )

  function submit() {
    if (!name.trim()) return
    create.mutate({ userId, name: name.trim() })
  }

  return (
    <div className="island-shell p-4 flex flex-wrap items-end gap-3">
      <div className="space-y-1 flex-1 min-w-[160px]">
        <label className="text-[11px] font-medium text-muted-foreground">Token name</label>
        <input
          type="text" autoFocus value={name}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') submit(); if (e.key === 'Escape') onClose() }}
          placeholder="e.g. laptop-cli"
          className="w-full rounded-lg border border-border bg-transparent px-3 py-1.5 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
        />
      </div>
      <div className="flex items-center gap-1.5">
        <button
          type="button" onClick={submit}
          disabled={create.isPending || !name.trim()}
          className="rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
          style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
        >
          Create
        </button>
        <button
          type="button" onClick={onClose}
          className="rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
        >
          Cancel
        </button>
      </div>
    </div>
  )
}
