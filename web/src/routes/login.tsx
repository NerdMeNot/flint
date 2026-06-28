import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { Shield, Eye, EyeOff, AlertCircle, Loader2, ArrowRight } from 'lucide-react'
import { storeSession } from '#/lib/auth-token'

// The Go API returns errors as { error: { code, message, requestId } }, but some
// paths return a plain string. Extract a renderable string either way — never the
// raw object (React can't render it, and it leaks requestId into the UI).
function errMessage(data: unknown, fallback: string): string {
  const d = data as { error?: unknown; message?: unknown } | null
  const e = d?.error
  if (typeof e === 'string') return e
  if (e && typeof (e as { message?: unknown }).message === 'string') {
    return (e as { message: string }).message
  }
  if (typeof d?.message === 'string') return d.message
  return fallback
}

export const Route = createFileRoute('/login')({
  component: LoginPage,
})

function LoginPage() {
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  // MFA challenge state
  const [mfaRequired, setMfaRequired] = useState(false)
  const [mfaToken, setMfaToken] = useState('')
  const [mfaCode, setMfaCode] = useState('')
  const [useRecovery, setUseRecovery] = useState(false)
  const [recoveryCode, setRecoveryCode] = useState('')

  // Force password change state
  const [forceChange, setForceChange] = useState(false)
  const [accessToken, setAccessToken] = useState('')
  const [refreshToken, setRefreshToken] = useState('')
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)

    try {
      const res = await fetch('/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      })
      const data = await res.json()

      if (!res.ok) {
        setError(errMessage(data, 'Login failed'))
        return
      }

      if (data.mfaRequired) {
        setMfaRequired(true)
        setMfaToken(data.mfaToken)
        return
      }

      if (data.forcePasswordChange) {
        setForceChange(true)
        setAccessToken(data.accessToken)
        setRefreshToken(data.refreshToken)
        setCurrentPassword(password)
        return
      }

      // Success — store tokens and redirect.
      storeSession(data.accessToken, data.refreshToken)
      navigate({ to: '/' })
    } catch {
      setError('Unable to connect to server')
    } finally {
      setLoading(false)
    }
  }

  const handleMFAVerify = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)

    try {
      const body: Record<string, string> = { mfaToken }
      if (useRecovery) {
        body.recoveryCode = recoveryCode
      } else {
        body.code = mfaCode
      }

      const res = await fetch('/auth/mfa/verify', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      const data = await res.json()

      if (!res.ok) {
        setError(errMessage(data, 'Verification failed'))
        return
      }

      storeSession(data.accessToken, data.refreshToken)
      navigate({ to: '/' })
    } catch {
      setError('Unable to connect to server')
    } finally {
      setLoading(false)
    }
  }

  const handlePasswordChange = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')

    if (newPassword.length < 8) {
      setError('Password must be at least 8 characters')
      return
    }
    if (newPassword !== confirmPassword) {
      setError('Passwords do not match')
      return
    }

    setLoading(true)
    try {
      const res = await fetch('/auth/change-password', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${accessToken}`,
        },
        body: JSON.stringify({ currentPassword, newPassword }),
      })
      const data = await res.json()

      if (!res.ok) {
        setError(errMessage(data, 'Password change failed'))
        return
      }

      // Password changed — store tokens and continue.
      storeSession(accessToken, refreshToken)
      navigate({ to: '/' })
    } catch {
      setError('Unable to connect to server')
    } finally {
      setLoading(false)
    }
  }

  const handleSSOLogin = () => {
    window.location.href = '/auth/login'
  }

  // Handle auto-submit for TOTP (6 digits)
  const handleMFACodeChange = (val: string) => {
    const clean = val.replace(/\D/g, '').slice(0, 6)
    setMfaCode(clean)
  }

  return (
    <div className="min-h-screen flex items-center justify-center px-4"
      style={{ background: 'var(--background)' }}>

      {/* Atmospheric background */}
      <div className="fixed inset-0 pointer-events-none" style={{
        background: `
          radial-gradient(ellipse 60% 50% at 50% 20%, var(--hero-a) 0%, transparent 70%),
          radial-gradient(ellipse 40% 40% at 80% 60%, var(--hero-b) 0%, transparent 60%)
        `,
      }} />

      <div className="relative w-full max-w-[400px]">
        {/* Brand */}
        <div className="flex flex-col items-center mb-8">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg font-bold text-lg mb-3"
            style={{
              background: 'linear-gradient(135deg, var(--ring), var(--success))',
              color: 'white',
              fontFamily: 'Fraunces, Georgia, serif',
              boxShadow: '0 4px 16px rgba(34, 211, 238, 0.3)',
            }}>
            F
          </div>
          <h1 className="display-title text-xl font-bold text-foreground tracking-tight">
            {forceChange ? 'Change Your Password' : mfaRequired ? 'Two-Factor Authentication' : 'Sign in to Flint'}
          </h1>
          <p className="text-sm text-muted-foreground mt-1">
            {forceChange
              ? 'You must set a new password before continuing'
              : mfaRequired
                ? 'Enter the code from your authenticator app'
                : 'Your CI platform, your way'}
          </p>
        </div>

        {/* Card */}
        <div className="island-shell p-6">
          {error && (
            <div className="flex items-start gap-2 rounded-lg p-3 mb-4 text-sm"
              style={{ background: 'rgba(220, 38, 38, 0.08)', border: '1px solid rgba(220, 38, 38, 0.2)' }}>
              <AlertCircle size={16} className="text-destructive shrink-0 mt-0.5" />
              <span className="text-destructive">{error}</span>
            </div>
          )}

          {/* Phase 1: Login form */}
          {!mfaRequired && !forceChange && (
            <form onSubmit={handleLogin} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-foreground mb-1.5">Email</label>
                <input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="you@company.dev"
                  required
                  autoFocus
                  className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-foreground mb-1.5">Password</label>
                <div className="relative">
                  <input
                    type={showPassword ? 'text' : 'password'}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder="Enter your password"
                    required
                    className="w-full rounded-lg border border-border bg-transparent px-3 py-2 pr-10 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow"
                  />
                  <button
                    type="button"
                    onClick={() => setShowPassword(!showPassword)}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
                  >
                    {showPassword ? <EyeOff size={16} /> : <Eye size={16} />}
                  </button>
                </div>
              </div>
              <button
                type="submit"
                disabled={loading || !email || !password}
                className="w-full flex items-center justify-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm py-2.5 hover:opacity-90 disabled:opacity-50 transition-opacity"
              >
                {loading ? <Loader2 size={16} className="animate-spin" /> : <ArrowRight size={16} />}
                Sign in
              </button>
            </form>
          )}

          {/* Phase 2: MFA verification */}
          {mfaRequired && !forceChange && (
            <form onSubmit={handleMFAVerify} className="space-y-4">
              {!useRecovery ? (
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1.5">
                    Authentication code
                  </label>
                  <div className="flex justify-center">
                    <input
                      type="text"
                      inputMode="numeric"
                      autoComplete="one-time-code"
                      value={mfaCode}
                      onChange={(e) => handleMFACodeChange(e.target.value)}
                      placeholder="000000"
                      maxLength={6}
                      autoFocus
                      className="w-48 text-center rounded-lg border border-border bg-transparent px-3 py-3 text-lg font-mono text-foreground tracking-[0.4em] placeholder:text-muted-foreground/30 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow"
                    />
                  </div>
                </div>
              ) : (
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1.5">
                    Recovery code
                  </label>
                  <input
                    type="text"
                    value={recoveryCode}
                    onChange={(e) => setRecoveryCode(e.target.value.toUpperCase())}
                    placeholder="XXXX-XXXX"
                    autoFocus
                    className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm font-mono text-foreground tracking-wider placeholder:text-muted-foreground/50 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow"
                  />
                </div>
              )}
              <button
                type="submit"
                disabled={loading || (!useRecovery && mfaCode.length !== 6) || (useRecovery && !recoveryCode)}
                className="w-full flex items-center justify-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm py-2.5 hover:opacity-90 disabled:opacity-50 transition-opacity"
              >
                {loading ? <Loader2 size={16} className="animate-spin" /> : <Shield size={16} />}
                Verify
              </button>
              <button
                type="button"
                onClick={() => { setUseRecovery(!useRecovery); setError('') }}
                className="w-full text-sm text-muted-foreground hover:text-foreground transition-colors"
              >
                {useRecovery ? 'Use authenticator app instead' : 'Use a recovery code'}
              </button>
            </form>
          )}

          {/* Phase 3: Forced password change */}
          {forceChange && (
            <form onSubmit={handlePasswordChange} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-foreground mb-1.5">New password</label>
                <input
                  type="password"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  placeholder="At least 8 characters"
                  required
                  autoFocus
                  className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-foreground mb-1.5">Confirm password</label>
                <input
                  type="password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  placeholder="Re-enter your new password"
                  required
                  className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow"
                />
              </div>
              <button
                type="submit"
                disabled={loading || !newPassword || !confirmPassword}
                className="w-full flex items-center justify-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm py-2.5 hover:opacity-90 disabled:opacity-50 transition-opacity"
              >
                {loading ? <Loader2 size={16} className="animate-spin" /> : <ArrowRight size={16} />}
                Set password & continue
              </button>
            </form>
          )}

          {/* SSO separator */}
          {!mfaRequired && !forceChange && (
            <>
              <div className="flex items-center gap-3 my-5">
                <div className="flex-1 h-px bg-border" />
                <span className="text-xs text-muted-foreground font-medium">or</span>
                <div className="flex-1 h-px bg-border" />
              </div>
              <button
                onClick={handleSSOLogin}
                className="w-full flex items-center justify-center gap-2 rounded-lg border border-border py-2.5 text-sm font-medium text-foreground hover:bg-accent/50 transition-colors"
              >
                <Shield size={16} />
                Sign in with SSO
              </button>
              <p className="text-center text-xs text-muted-foreground mt-4">
                Forgot your password?{' '}
                <span className="text-primary">Contact your Flint admin to reset it.</span>
              </p>
            </>
          )}
        </div>

        {/* Footer */}
        <p className="text-center text-xs text-muted-foreground mt-6 opacity-60">
          Flint CI — the platform your Kubernetes cluster deserves
        </p>
      </div>
    </div>
  )
}
