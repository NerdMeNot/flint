import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Shield, Copy, Download, Check, ArrowRight, Loader2 } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'

type MFASetupStep = 'qr' | 'verify' | 'recovery'

interface MFASetupWizardProps {
  onComplete: () => void
  onCancel?: () => void
}

export function MFASetupWizard({ onComplete, onCancel }: MFASetupWizardProps) {
  const [step, setStep] = useState<MFASetupStep>('qr')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // Setup data
  const [secret, setSecret] = useState('')
  const [qrCodeURL, setQrCodeURL] = useState('')
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([])
  const [showSecret, setShowSecret] = useState(false)

  // Verify
  const [code, setCode] = useState('')
  const [copied, setCopied] = useState(false)
  const queryClient = useQueryClient()

  const startSetup = async () => {
    setLoading(true)
    setError('')
    try {
      const data = await client.auth.mfa.setup()
      setSecret(data.secret)
      setQrCodeURL(data.qrCodeURL)
      setRecoveryCodes(data.recoveryCodes || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Setup failed')
    } finally {
      setLoading(false)
    }
  }

  const verifyCode = async () => {
    setLoading(true)
    setError('')
    try {
      await client.auth.mfa.verifySetup({ code })
      // MFA is now enabled — refresh /auth/me so the parent reflects it.
      await queryClient.invalidateQueries({ queryKey: orpc.auth.me.key() })
      setStep('recovery')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Verification failed')
    } finally {
      setLoading(false)
    }
  }

  const copyRecoveryCodes = () => {
    navigator.clipboard.writeText(recoveryCodes.join('\n'))
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const downloadRecoveryCodes = () => {
    const text = `Flint CI - Recovery Codes\n${'='.repeat(30)}\n\nStore these codes safely. Each code can only be used once.\n\n${recoveryCodes.join('\n')}\n`
    const blob = new Blob([text], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'flint-recovery-codes.txt'
    a.click()
    URL.revokeObjectURL(url)
  }

  // Auto-start setup
  if (!secret && !loading && !error) {
    startSetup()
  }

  const handleCodeChange = (val: string) => {
    setCode(val.replace(/\D/g, '').slice(0, 6))
  }

  return (
    <div className="space-y-5">
      {/* Progress indicator */}
      <div className="flex items-center gap-2 mb-6">
        {(['qr', 'verify', 'recovery'] as const).map((s, i) => (
          <div key={s} className="flex items-center gap-2">
            <div className={`flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold transition-colors ${
              step === s
                ? 'bg-primary text-primary-foreground'
                : i < ['qr', 'verify', 'recovery'].indexOf(step)
                  ? 'bg-[var(--success)] text-white'
                  : 'bg-muted text-muted-foreground'
            }`}>
              {i < ['qr', 'verify', 'recovery'].indexOf(step) ? <Check size={14} /> : i + 1}
            </div>
            {i < 2 && <div className="w-8 h-px bg-border" />}
          </div>
        ))}
      </div>

      {error && (
        <div className="rounded-lg p-3 text-sm"
          style={{ background: 'rgba(220, 38, 38, 0.08)', border: '1px solid rgba(220, 38, 38, 0.2)' }}>
          <span className="text-destructive">{error}</span>
        </div>
      )}

      {/* Step 1: QR Code */}
      {step === 'qr' && (
        <div className="space-y-4">
          <p className="text-sm text-muted-foreground">
            Scan this QR code with your authenticator app (Google Authenticator, Authy, 1Password, etc.)
          </p>

          {qrCodeURL ? (
            <div className="flex justify-center py-2">
              <div className="rounded-xl p-4 bg-white">
                <img
                  src={`https://api.qrserver.com/v1/create-qr-code/?size=200x200&data=${encodeURIComponent(qrCodeURL)}`}
                  alt="TOTP QR Code"
                  className="w-48 h-48"
                />
              </div>
            </div>
          ) : (
            <div className="flex justify-center py-8">
              <Loader2 size={24} className="animate-spin text-muted-foreground" />
            </div>
          )}

          <div>
            <button
              type="button"
              onClick={() => setShowSecret(!showSecret)}
              className="text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
              {showSecret ? 'Hide' : 'Show'} manual entry key
            </button>
            {showSecret && (
              <div className="mt-2 rounded-lg border border-border p-3 bg-muted/30">
                <code className="text-xs font-mono text-foreground break-all select-all">{secret}</code>
              </div>
            )}
          </div>

          <button
            onClick={() => setStep('verify')}
            disabled={!secret}
            className="w-full flex items-center justify-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm py-2.5 hover:opacity-90 disabled:opacity-50 transition-opacity"
          >
            <ArrowRight size={16} />
            Continue
          </button>
        </div>
      )}

      {/* Step 2: Verify */}
      {step === 'verify' && (
        <div className="space-y-4">
          <p className="text-sm text-muted-foreground">
            Enter the 6-digit code from your authenticator app to confirm setup.
          </p>
          <div className="flex justify-center">
            <input
              type="text"
              inputMode="numeric"
              autoComplete="one-time-code"
              value={code}
              onChange={(e) => handleCodeChange(e.target.value)}
              placeholder="000000"
              maxLength={6}
              autoFocus
              className="w-48 text-center rounded-lg border border-border bg-transparent px-3 py-3 text-lg font-mono text-foreground tracking-[0.4em] placeholder:text-muted-foreground/30 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow"
            />
          </div>
          <button
            onClick={verifyCode}
            disabled={loading || code.length !== 6}
            className="w-full flex items-center justify-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm py-2.5 hover:opacity-90 disabled:opacity-50 transition-opacity"
          >
            {loading ? <Loader2 size={16} className="animate-spin" /> : <Shield size={16} />}
            Verify & enable MFA
          </button>
        </div>
      )}

      {/* Step 3: Recovery codes */}
      {step === 'recovery' && (
        <div className="space-y-4">
          <div className="flex items-start gap-2 rounded-lg p-3"
            style={{ background: 'rgba(217, 119, 6, 0.08)', border: '1px solid rgba(217, 119, 6, 0.2)' }}>
            <Shield size={16} className="text-[var(--warning)] shrink-0 mt-0.5" />
            <span className="text-sm text-foreground">
              Save these recovery codes in a safe place. Each code can only be used once if you lose access to your authenticator.
            </span>
          </div>

          <div className="grid grid-cols-2 gap-2 rounded-lg border border-border p-4 bg-muted/20">
            {recoveryCodes.map((code, i) => (
              <code key={i} className="text-sm font-mono text-foreground py-1">{code}</code>
            ))}
          </div>

          <div className="flex gap-2">
            <button
              onClick={copyRecoveryCodes}
              className="flex-1 flex items-center justify-center gap-2 rounded-lg border border-border py-2 text-sm font-medium text-foreground hover:bg-accent/50 transition-colors"
            >
              {copied ? <Check size={14} className="text-[var(--success)]" /> : <Copy size={14} />}
              {copied ? 'Copied' : 'Copy'}
            </button>
            <button
              onClick={downloadRecoveryCodes}
              className="flex-1 flex items-center justify-center gap-2 rounded-lg border border-border py-2 text-sm font-medium text-foreground hover:bg-accent/50 transition-colors"
            >
              <Download size={14} />
              Download
            </button>
          </div>

          <button
            onClick={onComplete}
            className="w-full flex items-center justify-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm py-2.5 hover:opacity-90 transition-opacity"
          >
            <Check size={16} />
            I've saved my recovery codes
          </button>
        </div>
      )}

      {onCancel && step === 'qr' && (
        <button
          onClick={onCancel}
          className="w-full text-sm text-muted-foreground hover:text-foreground transition-colors"
        >
          Cancel
        </button>
      )}
    </div>
  )
}
