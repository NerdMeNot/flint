import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Sun, Moon, Monitor, Check, ShieldCheck, KeyRound, Monitor as MonitorIcon, Key, ChevronRight, Pencil } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import {
  themes,
  themeModes,
  setThemeMode,
  setColorTheme,
  getStoredMode,
  getStoredColorTheme,
  DEFAULT_MODE,
  DEFAULT_COLOR,
  type ThemeMode,
} from '#/lib/appearance'

export const Route = createFileRoute('/profile')({
  component: ProfilePage,
})

const MODE_ICON = { light: Sun, dark: Moon, auto: Monitor } as const

function ProfilePage() {
  // Plain useQuery (not suspense): auth.me is a shared query read non-suspensively
  // across the app (e.g. the sidebar). Suspending on it here would change its SSR
  // resolution and surface a hydration mismatch in those other readers.
  const { data: user } = useQuery(orpc.auth.me.queryOptions({}))

  if (!user) {
    return (
      <div className="space-y-6 rise-in max-w-3xl">
        <div className="h-9 w-40 rounded bg-muted/40 animate-pulse" />
        <div className="island-shell h-40 animate-pulse" />
      </div>
    )
  }

  const initials = (user.name ?? user.email ?? '?')
    .split(/[\s@]+/)
    .slice(0, 2)
    .map((s) => s[0]?.toUpperCase() ?? '')
    .join('')

  return (
    <div className="space-y-6 rise-in max-w-3xl">
      <div>
        <h1 className="display-title text-3xl lg:text-4xl text-foreground">Profile</h1>
        <p className="text-muted-foreground text-sm lg:text-base mt-2">
          Your identity, appearance, and security settings
        </p>
      </div>

      <IdentityCard user={user} initials={initials} />
      <AppearanceCard />
      <SecurityCard />
    </div>
  )
}

// ── Identity ────────────────────────────────────────────────

function IdentityCard({ user, initials }: {
  user: { name: string; email: string; role: string; provider?: string; avatarUrl?: string; groups: string[] }
  initials: string
}) {
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(user.name)
  const [avatarUrl, setAvatarUrl] = useState(user.avatarUrl ?? '')

  const save = useAction(
    (input: { name: string; avatarUrl: string }) => client.auth.updateProfile(input),
    { invalidate: [orpc.auth.me.key()], onSuccess: () => setEditing(false) },
  )

  return (
    <section className="island-shell p-5 space-y-4">
      <div className="flex items-start justify-between gap-3">
        <h2 className="text-sm font-semibold text-foreground">Identity</h2>
        {!editing && (
          <button
            type="button"
            onClick={() => { setName(user.name); setAvatarUrl(user.avatarUrl ?? ''); setEditing(true) }}
            className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
          >
            <Pencil size={12} /> Edit
          </button>
        )}
      </div>

      <div className="flex items-center gap-4">
        {avatarUrl || user.avatarUrl ? (
          <img
            src={editing ? avatarUrl : user.avatarUrl}
            alt={user.name}
            className="h-14 w-14 rounded-full object-cover border border-border shrink-0"
          />
        ) : (
          <div
            className="flex h-14 w-14 items-center justify-center rounded-full text-base font-bold shrink-0"
            style={{ background: 'color-mix(in oklab, var(--primary) 25%, var(--muted))', color: 'var(--foreground)' }}
          >
            {initials}
          </div>
        )}
        <div className="min-w-0">
          <p className="text-base font-semibold text-foreground truncate">{user.name || user.email}</p>
          <p className="text-sm text-muted-foreground truncate">{user.email}</p>
        </div>
      </div>

      {editing ? (
        <div className="space-y-3">
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-foreground">Display name</label>
            <input
              type="text" value={name} onChange={(e) => setName(e.target.value)}
              className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring/40"
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-foreground">Avatar URL</label>
            <input
              type="url" value={avatarUrl} onChange={(e) => setAvatarUrl(e.target.value)}
              placeholder="https://…"
              className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm font-mono text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
            />
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={save.isPending || !name.trim()}
              onClick={() => save.mutate({ name: name.trim(), avatarUrl: avatarUrl.trim() })}
              className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
            >
              Save changes
            </button>
            <button
              type="button" onClick={() => setEditing(false)}
              className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
            >
              Cancel
            </button>
          </div>
        </div>
      ) : (
        <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm">
          <Field label="Role" value={user.role || '—'} />
          <Field label="Sign-in" value={user.provider || '—'} />
          <div className="col-span-2">
            <dt className="text-xs font-medium text-muted-foreground mb-1">Groups</dt>
            <dd className="flex flex-wrap gap-1.5">
              {user.groups.length > 0 ? user.groups.map((g) => (
                <span key={g} className="rounded-md bg-secondary border border-border px-2 py-0.5 text-[12px] font-medium text-muted-foreground">{g}</span>
              )) : <span className="text-muted-foreground">—</span>}
            </dd>
          </div>
        </dl>
      )}
    </section>
  )
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs font-medium text-muted-foreground">{label}</dt>
      <dd className="text-sm text-foreground mt-0.5 capitalize">{value}</dd>
    </div>
  )
}

// ── Appearance ──────────────────────────────────────────────

function AppearanceCard() {
  // Initialize to deterministic defaults so SSR and the first client render
  // match; read the actual stored values after mount (localStorage is
  // client-only) to avoid a hydration mismatch.
  const [mode, setMode] = useState<ThemeMode>(DEFAULT_MODE)
  const [color, setColor] = useState(DEFAULT_COLOR)

  useEffect(() => {
    setMode(getStoredMode())
    setColor(getStoredColorTheme())
  }, [])

  const persist = useAction(
    (input: { themeMode?: ThemeMode; colorTheme?: string }) => client.auth.updateProfile(input),
    { invalidate: [orpc.auth.me.key()] },
  )

  function pickMode(next: ThemeMode) {
    setMode(next)
    setThemeMode(next)          // apply + localStorage
    persist.mutate({ themeMode: next })
  }
  function pickColor(name: string) {
    setColor(name)
    setColorTheme(name)         // apply + localStorage
    persist.mutate({ colorTheme: name })
  }

  return (
    <section className="island-shell p-5 space-y-5">
      <div>
        <h2 className="text-sm font-semibold text-foreground">Appearance</h2>
        <p className="text-xs text-muted-foreground mt-0.5">Saved to your profile and applied on every device.</p>
      </div>

      <div className="space-y-2">
        <label className="text-xs font-medium text-foreground">Mode</label>
        <div className="flex flex-wrap gap-2">
          {themeModes.map(({ value, label }) => {
            const Icon = MODE_ICON[value]
            const active = mode === value
            return (
              <button
                key={value}
                type="button"
                onClick={() => pickMode(value)}
                className={`flex items-center gap-2 rounded-lg border px-3.5 py-2 text-xs font-medium transition-colors ${
                  active ? 'border-primary/40 bg-primary/5 text-primary' : 'border-border text-muted-foreground hover:text-foreground'
                }`}
              >
                <Icon size={14} />
                {label}
                {active && <Check size={12} />}
              </button>
            )
          })}
        </div>
      </div>

      <div className="space-y-2">
        <label className="text-xs font-medium text-foreground">Color theme</label>
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
          {themes.map((t) => {
            const active = color === t.name
            return (
              <button
                key={t.name}
                type="button"
                onClick={() => pickColor(t.name)}
                className={`flex items-center gap-2.5 rounded-lg border px-3 py-2.5 text-sm font-medium transition-colors ${
                  active ? 'border-primary/40 bg-primary/5 text-foreground' : 'border-border text-muted-foreground hover:text-foreground'
                }`}
              >
                <span className="w-4 h-4 rounded-full border border-border shrink-0" style={{ background: t.dot }} />
                {t.label}
                {active && <Check size={13} className="ml-auto text-primary" />}
              </button>
            )
          })}
        </div>
      </div>
    </section>
  )
}

// ── Security & tokens (entry points to existing flows) ──────

function SecurityCard() {
  const links = [
    { to: '/settings/security', icon: ShieldCheck, label: 'Password & MFA', detail: 'Change password, manage two-factor' },
    { to: '/settings/sessions', icon: MonitorIcon, label: 'Active sessions', detail: 'Review and revoke signed-in devices' },
    { to: '/settings/api-keys', icon: Key, label: 'Access tokens', detail: 'Personal tokens for the API and CLI' },
  ] as const

  return (
    <section className="island-shell p-5 space-y-3">
      <div className="flex items-center gap-2">
        <KeyRound size={14} className="text-muted-foreground" />
        <h2 className="text-sm font-semibold text-foreground">Security & access</h2>
      </div>
      <div className="divide-y divide-border">
        {links.map(({ to, icon: Icon, label, detail }) => (
          <Link
            key={to}
            to={to}
            className="flex items-center gap-3 py-3 group first:pt-0 last:pb-0"
          >
            <Icon size={16} className="text-muted-foreground shrink-0" />
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium text-foreground group-hover:text-primary transition-colors">{label}</p>
              <p className="text-xs text-muted-foreground truncate">{detail}</p>
            </div>
            <ChevronRight size={15} className="text-muted-foreground/50 group-hover:text-foreground transition-colors shrink-0" />
          </Link>
        ))}
      </div>
    </section>
  )
}
