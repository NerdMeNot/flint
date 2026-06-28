import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Shield, Key, AlertCircle, Check, Loader2, Eye, EyeOff } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { MFASetupWizard } from '#/components/auth/MFASetupWizard'
import { PageHeader } from '#/components/PageHeader'

export const Route = createFileRoute('/profile/security')({
  component: SecurityTab,
})

function SecurityTab() {
  return (
    <div className="max-w-2xl space-y-6">
      <PageHeader title="Security" subtitle="Manage your password and two-factor authentication" />
      <ChangePasswordSection />
      <MFASection />
    </div>
  )
}

function Alert({ kind, children }: { kind: 'error' | 'success'; children: React.ReactNode }) {
  const cls = kind === 'error'
    ? 'bg-destructive/10 border-destructive/25 text-destructive'
    : 'bg-success/10 border-success/30 text-success'
  return (
    <div className={`flex items-center gap-2 rounded-lg border p-3 text-sm ${cls}`}>
      {kind === 'error' ? <AlertCircle size={14} /> : <Check size={14} />}
      <span>{children}</span>
    </div>
  )
}

function ChangePasswordSection() {
  const [current, setCurrent] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirm, setConfirm] = useState('')
  const [showCurrent, setShowCurrent] = useState(false)
  const [localError, setLocalError] = useState('')

  const change = useAction(
    (input: { currentPassword: string; newPassword: string }) => client.auth.changePassword(input),
    { onSuccess: () => { setCurrent(''); setNewPw(''); setConfirm('') } },
  )

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLocalError('')
    if (newPw.length < 8) { setLocalError('Password must be at least 8 characters'); return }
    if (newPw !== confirm) { setLocalError('Passwords do not match'); return }
    change.mutate({ currentPassword: current, newPassword: newPw })
  }

  const error = localError || (change.isError ? (change.error as Error).message : '')

  return (
    <section className="island-shell p-5">
      <div className="flex items-center gap-2 mb-4">
        <Key size={16} className="text-primary" />
        <h2 className="text-sm font-semibold text-foreground">Password</h2>
      </div>

      <div className="space-y-3">
        {error && <Alert kind="error">{error}</Alert>}
        {change.isSuccess && <Alert kind="success">Password changed successfully</Alert>}

        <form onSubmit={handleSubmit} className="space-y-3">
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-foreground">Current password</label>
            <div className="relative">
              <input
                type={showCurrent ? 'text' : 'password'}
                value={current}
                onChange={(e) => setCurrent(e.target.value)}
                required
                className="w-full rounded-lg border border-border bg-transparent px-3 py-2 pr-10 text-sm text-foreground outline-none focus:ring-2 focus:ring-ring/40 transition-shadow"
              />
              <button type="button" onClick={() => setShowCurrent(!showCurrent)}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
                {showCurrent ? <EyeOff size={14} /> : <Eye size={14} />}
              </button>
            </div>
          </div>
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-foreground">New password</label>
            <input type="password" value={newPw} onChange={(e) => setNewPw(e.target.value)}
              placeholder="At least 8 characters" required
              className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow" />
          </div>
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-foreground">Confirm new password</label>
            <input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required
              className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground outline-none focus:ring-2 focus:ring-ring/40 transition-shadow" />
          </div>
          <button type="submit" disabled={change.isPending || !current || !newPw || !confirm}
            className="flex items-center gap-2 rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
            style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
            {change.isPending ? <Loader2 size={13} className="animate-spin" /> : <Key size={13} />}
            Change password
          </button>
        </form>
      </div>
    </section>
  )
}

function MFASection() {
  const { data: user } = useQuery(orpc.auth.me.queryOptions({}))
  const mfaEnabled = !!user?.mfaEnabled
  const [showSetup, setShowSetup] = useState(false)
  const [disableCode, setDisableCode] = useState('')
  const [showDisable, setShowDisable] = useState(false)
  const [showRegen, setShowRegen] = useState(false)
  const [regenCode, setRegenCode] = useState('')
  const [newCodes, setNewCodes] = useState<string[]>([])

  const disable = useAction((code: string) => client.auth.mfa.disable({ code }), {
    invalidate: [orpc.auth.me.key()],
    onSuccess: () => { setShowDisable(false); setDisableCode('') },
  })

  const regen = useAction(
    (code: string) => client.auth.mfa.regenerateRecoveryCodes({ code }),
    { onSuccess: (res: { recoveryCodes: string[] }) => { setNewCodes(res.recoveryCodes); setRegenCode('') } },
  )

  return (
    <section className="island-shell p-5">
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <Shield size={16} className="text-primary" />
          <h2 className="text-sm font-semibold text-foreground">Two-factor authentication</h2>
        </div>
        {mfaEnabled && (
          <span className="flex items-center gap-1 text-xs font-medium text-success">
            <Check size={12} /> Enabled
          </span>
        )}
      </div>

      {disable.isError && <div className="mb-4"><Alert kind="error">{(disable.error as Error).message}</Alert></div>}

      {showSetup ? (
        <MFASetupWizard
          onComplete={() => setShowSetup(false)}
          onCancel={() => setShowSetup(false)}
        />
      ) : showDisable ? (
        <div className="space-y-3">
          <p className="text-sm text-muted-foreground">Enter your current authenticator code to disable MFA.</p>
          <input type="text" inputMode="numeric" value={disableCode}
            onChange={(e) => setDisableCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
            placeholder="000000" maxLength={6} autoFocus
            className="w-48 text-center rounded-lg border border-border bg-transparent px-3 py-2 text-sm font-mono text-foreground tracking-[0.3em] outline-none focus:ring-2 focus:ring-ring/40" />
          <div className="flex gap-2">
            <button onClick={() => disable.mutate(disableCode)} disabled={disable.isPending || disableCode.length !== 6}
              className="flex items-center gap-2 rounded-lg bg-destructive text-white font-medium text-xs px-3.5 py-1.5 hover:opacity-90 disabled:opacity-50">
              {disable.isPending ? <Loader2 size={13} className="animate-spin" /> : null}
              Disable MFA
            </button>
            <button onClick={() => { setShowDisable(false); setDisableCode('') }}
              className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-foreground hover:bg-accent/50">
              Cancel
            </button>
          </div>
        </div>
      ) : showRegen ? (
        <div className="space-y-3">
          {newCodes.length > 0 ? (
            <>
              <p className="text-sm text-muted-foreground">
                Your new recovery codes. Store them safely — each is single-use and the old codes no
                longer work.
              </p>
              <div className="grid grid-cols-2 gap-1.5 rounded-lg border border-border bg-muted/20 p-3 font-mono text-xs text-foreground">
                {newCodes.map((c, i) => <span key={i}>{c}</span>)}
              </div>
              <button onClick={() => { setShowRegen(false); setNewCodes([]) }}
                className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-foreground hover:bg-accent/50">
                Done
              </button>
            </>
          ) : (
            <>
              <p className="text-sm text-muted-foreground">
                Enter your current authenticator code to generate a fresh set of recovery codes. This
                invalidates your existing codes.
              </p>
              {regen.isError && <Alert kind="error">{(regen.error as Error).message}</Alert>}
              <input type="text" inputMode="numeric" value={regenCode}
                onChange={(e) => setRegenCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                placeholder="000000" maxLength={6} autoFocus
                className="w-48 text-center rounded-lg border border-border bg-transparent px-3 py-2 text-sm font-mono text-foreground tracking-[0.3em] outline-none focus:ring-2 focus:ring-ring/40" />
              <div className="flex gap-2">
                <button onClick={() => regen.mutate(regenCode)} disabled={regen.isPending || regenCode.length !== 6}
                  className="flex items-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-xs px-3.5 py-1.5 hover:opacity-90 disabled:opacity-50">
                  {regen.isPending ? <Loader2 size={13} className="animate-spin" /> : null}
                  Generate new codes
                </button>
                <button onClick={() => { setShowRegen(false); setRegenCode('') }}
                  className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-foreground hover:bg-accent/50">
                  Cancel
                </button>
              </div>
            </>
          )}
        </div>
      ) : (
        <div>
          <p className="text-sm text-muted-foreground mb-4">
            {mfaEnabled
              ? 'Your account is protected with TOTP two-factor authentication.'
              : 'Add an extra layer of security by enabling two-factor authentication with an authenticator app.'}
          </p>
          {mfaEnabled ? (
            <div className="flex gap-2">
              <button onClick={() => setShowRegen(true)}
                className="rounded-lg border border-border font-medium text-xs px-3.5 py-1.5 text-foreground hover:bg-accent/50 transition-colors">
                Regenerate recovery codes
              </button>
              <button onClick={() => setShowDisable(true)}
                className="rounded-lg border border-destructive/30 text-destructive font-medium text-xs px-3.5 py-1.5 hover:bg-destructive/5 transition-colors">
                Disable MFA
              </button>
            </div>
          ) : (
            <button onClick={() => setShowSetup(true)}
              className="flex items-center gap-2 rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
              <Shield size={13} />
              Enable MFA
            </button>
          )}
        </div>
      )}
    </section>
  )
}
