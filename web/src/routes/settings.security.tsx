import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { Shield, Key, AlertCircle, Check, Loader2, Eye, EyeOff } from 'lucide-react'
import { MFASetupWizard } from '#/components/auth/MFASetupWizard'

export const Route = createFileRoute('/settings/security')({
  component: SecuritySettings,
})

function SecuritySettings() {
  return (
    <div className="max-w-2xl space-y-8">
      <div>
        <h1 className="display-title text-xl font-bold text-foreground">Security</h1>
        <p className="text-sm text-muted-foreground mt-1">Manage your password and two-factor authentication</p>
      </div>

      <ChangePasswordSection />
      <div className="h-px bg-border" />
      <MFASection />
    </div>
  )
}

function ChangePasswordSection() {
  const [current, setCurrent] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirm, setConfirm] = useState('')
  const [showCurrent, setShowCurrent] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setSuccess(false)

    if (newPw.length < 8) { setError('Password must be at least 8 characters'); return }
    if (newPw !== confirm) { setError('Passwords do not match'); return }

    setLoading(true)
    try {
      const token = localStorage.getItem('flint_access_token')
      const res = await fetch('/auth/change-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
        body: JSON.stringify({ currentPassword: current, newPassword: newPw }),
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || 'Failed')

      setSuccess(true)
      setCurrent('')
      setNewPw('')
      setConfirm('')
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="island-shell p-5">
      <div className="flex items-center gap-2 mb-4">
        <Key size={18} className="text-primary" />
        <h2 className="text-base font-semibold text-foreground">Password</h2>
      </div>

      {error && (
        <div className="flex items-center gap-2 rounded-lg p-3 mb-4 text-sm"
          style={{ background: 'rgba(220, 38, 38, 0.08)', border: '1px solid rgba(220, 38, 38, 0.2)' }}>
          <AlertCircle size={14} className="text-destructive" />
          <span className="text-destructive">{error}</span>
        </div>
      )}
      {success && (
        <div className="flex items-center gap-2 rounded-lg p-3 mb-4 text-sm"
          style={{ background: 'rgba(5, 150, 105, 0.08)', border: '1px solid rgba(5, 150, 105, 0.2)' }}>
          <Check size={14} className="text-[var(--success)]" />
          <span className="text-[var(--success)]">Password changed successfully</span>
        </div>
      )}

      <form onSubmit={handleSubmit} className="space-y-3">
        <div>
          <label className="block text-sm font-medium text-foreground mb-1">Current password</label>
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
        <div>
          <label className="block text-sm font-medium text-foreground mb-1">New password</label>
          <input type="password" value={newPw} onChange={(e) => setNewPw(e.target.value)}
            placeholder="At least 8 characters" required
            className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow" />
        </div>
        <div>
          <label className="block text-sm font-medium text-foreground mb-1">Confirm new password</label>
          <input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required
            className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground outline-none focus:ring-2 focus:ring-ring/40 transition-shadow" />
        </div>
        <button type="submit" disabled={loading || !current || !newPw || !confirm}
          className="flex items-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm px-4 py-2 hover:opacity-90 disabled:opacity-50 transition-opacity">
          {loading ? <Loader2 size={14} className="animate-spin" /> : <Key size={14} />}
          Change password
        </button>
      </form>
    </div>
  )
}

function MFASection() {
  const [mfaEnabled, setMfaEnabled] = useState(false) // TODO: fetch from /auth/me
  const [showSetup, setShowSetup] = useState(false)
  const [disableCode, setDisableCode] = useState('')
  const [showDisable, setShowDisable] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const handleDisable = async () => {
    setLoading(true)
    setError('')
    try {
      const token = localStorage.getItem('flint_access_token')
      const res = await fetch('/auth/mfa', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
        body: JSON.stringify({ code: disableCode }),
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || 'Failed')

      setMfaEnabled(false)
      setShowDisable(false)
      setDisableCode('')
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="island-shell p-5">
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <Shield size={18} className="text-primary" />
          <h2 className="text-base font-semibold text-foreground">Two-Factor Authentication</h2>
        </div>
        {mfaEnabled && (
          <span className="flex items-center gap-1 text-xs font-medium text-[var(--success)]">
            <Check size={12} /> Enabled
          </span>
        )}
      </div>

      {error && (
        <div className="flex items-center gap-2 rounded-lg p-3 mb-4 text-sm"
          style={{ background: 'rgba(220, 38, 38, 0.08)', border: '1px solid rgba(220, 38, 38, 0.2)' }}>
          <AlertCircle size={14} className="text-destructive" />
          <span className="text-destructive">{error}</span>
        </div>
      )}

      {showSetup ? (
        <MFASetupWizard
          onComplete={() => { setShowSetup(false); setMfaEnabled(true) }}
          onCancel={() => setShowSetup(false)}
        />
      ) : showDisable ? (
        <div className="space-y-3">
          <p className="text-sm text-muted-foreground">
            Enter your current authenticator code to disable MFA.
          </p>
          <input type="text" inputMode="numeric" value={disableCode}
            onChange={(e) => setDisableCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
            placeholder="000000" maxLength={6} autoFocus
            className="w-48 text-center rounded-lg border border-border bg-transparent px-3 py-2 text-sm font-mono text-foreground tracking-[0.3em] outline-none focus:ring-2 focus:ring-ring/40" />
          <div className="flex gap-2">
            <button onClick={handleDisable} disabled={loading || disableCode.length !== 6}
              className="flex items-center gap-2 rounded-lg bg-destructive text-white font-medium text-sm px-4 py-2 hover:opacity-90 disabled:opacity-50">
              {loading ? <Loader2 size={14} className="animate-spin" /> : null}
              Disable MFA
            </button>
            <button onClick={() => { setShowDisable(false); setDisableCode('') }}
              className="rounded-lg border border-border px-4 py-2 text-sm font-medium text-foreground hover:bg-accent/50">
              Cancel
            </button>
          </div>
        </div>
      ) : (
        <div>
          <p className="text-sm text-muted-foreground mb-4">
            {mfaEnabled
              ? 'Your account is protected with TOTP two-factor authentication.'
              : 'Add an extra layer of security by enabling two-factor authentication with an authenticator app.'}
          </p>
          {mfaEnabled ? (
            <button onClick={() => setShowDisable(true)}
              className="flex items-center gap-2 rounded-lg border border-destructive/30 text-destructive font-medium text-sm px-4 py-2 hover:bg-destructive/5 transition-colors">
              Disable MFA
            </button>
          ) : (
            <button onClick={() => setShowSetup(true)}
              className="flex items-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm px-4 py-2 hover:opacity-90 transition-opacity">
              <Shield size={14} />
              Enable MFA
            </button>
          )}
        </div>
      )}
    </div>
  )
}
