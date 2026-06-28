import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  Check, ChevronLeft, Loader2, Eye, EyeOff, ExternalLink, AlertCircle,
  CheckCircle2, Upload, ArrowRight, LogIn,
} from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import type { ProviderConfig, ProviderTestResult, TestLoginResult } from '#/lib/api/types'
import {
  SSO_PRESETS, presetById, presetProtocols, type SSOProtocol, type SSOPreset,
} from '#/lib/sso/presets'
import { ProviderLogo } from './provider-logos'
import { CopyField } from './CopyField'

type Step = 'choose' | 'configure' | 'test' | 'map' | 'activate'
const STEPS: Step[] = ['choose', 'configure', 'test', 'map', 'activate']
const STEP_LABELS: Record<Step, string> = {
  choose: 'Choose', configure: 'Configure', test: 'Test', map: 'Map', activate: 'Activate',
}

const inputClass =
  'w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow'

const toScopes = (a?: string[]) => (a ?? []).join(' ')
const fromScopes = (s: string) => s.split(/\s+/).map((x) => x.trim()).filter(Boolean)
const toLines = (a?: string[]) => (a ?? []).join('\n')
const fromLines = (s: string) => s.split(/[\n,]/).map((x) => x.trim()).filter(Boolean)

interface Props {
  onComplete: () => void
  onCancel: () => void
}

export function SSOWizard({ onComplete, onCancel }: Props) {
  const queryClient = useQueryClient()
  const [step, setStep] = useState<Step>('choose')
  const [presetId, setPresetId] = useState<string>('')
  const [protocol, setProtocol] = useState<SSOProtocol>('oidc')
  const [config, setConfig] = useState<ProviderConfig>({})
  const [showSecret, setShowSecret] = useState(false)
  const [metaMode, setMetaMode] = useState<'url' | 'xml'>('url')

  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<ProviderTestResult | null>(null)
  const [signingIn, setSigningIn] = useState(false)
  const [capturedClaims, setCapturedClaims] = useState<NonNullable<TestLoginResult['result']> | null>(null)
  const [signInError, setSignInError] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const preset = presetById(presetId)
  const origin = typeof window !== 'undefined' ? window.location.origin : ''
  const redirectUri = `${origin}/auth/oidc/callback`
  const acsUrl = `${origin}/auth/saml/acs`
  const spEntityId = `${origin}/auth/saml/metadata`

  const set = (patch: Partial<ProviderConfig>) => setConfig((c) => ({ ...c, ...patch }))

  const choose = (p: SSOPreset) => {
    const protos = presetProtocols(p)
    const proto = protos[0]
    setPresetId(p.id)
    setProtocol(proto)
    setConfig({ ...(proto === 'oidc' ? p.oidc?.defaults : p.saml?.defaults) })
    setTestResult(null)
    setError('')
    setStep('configure')
  }

  const switchProtocol = (proto: SSOProtocol) => {
    if (!preset) return
    setProtocol(proto)
    setConfig({ ...(proto === 'oidc' ? preset.oidc?.defaults : preset.saml?.defaults) })
    setTestResult(null)
  }

  const protoPreset = preset && (protocol === 'oidc' ? preset.oidc : preset.saml)

  const canConfigure =
    protocol === 'oidc'
      ? Boolean(config.issuerUrl && config.clientId)
      : Boolean(config.metadataUrl || config.metadataXml)

  const runTest = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      const r = await client.auth.providers.test({ providerType: protocol, config })
      setTestResult(r)
    } catch (err) {
      setTestResult({ ok: false, error: err instanceof Error ? err.message : 'Test failed' })
    } finally {
      setTesting(false)
    }
  }

  // Decoded test sign-in: open the IdP login in a popup, then poll for the exact
  // claims/assertion it returns — never creating a session.
  const runTestSignIn = async () => {
    setSigningIn(true)
    setSignInError('')
    setCapturedClaims(null)
    try {
      const { testId, authUrl } = await client.auth.providers.testLoginStart({ providerType: protocol, config })
      const popup = window.open(authUrl, 'flint-sso-test', 'width=480,height=680')
      const deadline = Date.now() + 3 * 60 * 1000
      while (Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 1500))
        const r = await client.auth.providers.testLoginResult({ id: testId })
        if (r.status === 'complete') {
          setCapturedClaims(r.result ?? null)
          popup?.close()
          return
        }
        if (r.status === 'error') {
          setSignInError(r.error || 'Sign-in failed')
          popup?.close()
          return
        }
        if (popup && popup.closed) {
          setSignInError('The sign-in window was closed before completing.')
          return
        }
      }
      setSignInError('Timed out waiting for sign-in.')
    } catch (err) {
      setSignInError(err instanceof Error ? err.message : 'Test sign-in failed')
    } finally {
      setSigningIn(false)
    }
  }

  const activate = async () => {
    setSaving(true)
    setError('')
    try {
      await client.auth.providers.save({
        providerType: protocol,
        displayName: preset?.name ?? protocol.toUpperCase(),
        config,
      })
      await queryClient.invalidateQueries({ queryKey: orpc.auth.providers.list.key() })
      onComplete()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Activation failed')
    } finally {
      setSaving(false)
    }
  }

  const goBack = () => {
    const i = STEPS.indexOf(step)
    if (i <= 0) { onCancel(); return }
    setStep(STEPS[i - 1])
  }

  const onFile = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = () => set({ metadataXml: String(reader.result || ''), metadataUrl: '' })
    reader.readAsText(file)
  }

  return (
    <div className="space-y-6">
      {/* Progress */}
      <div className="flex items-center gap-2">
        {STEPS.map((s, i) => {
          const active = step === s
          const done = i < STEPS.indexOf(step)
          return (
            <div key={s} className="flex items-center gap-2">
              <div className="flex items-center gap-2">
                <div className={`flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold transition-colors ${
                  active ? 'bg-primary text-primary-foreground'
                    : done ? 'bg-[var(--success)] text-white'
                      : 'bg-muted text-muted-foreground'
                }`}>
                  {done ? <Check size={14} /> : i + 1}
                </div>
                <span className={`text-xs font-medium ${active ? 'text-foreground' : 'text-muted-foreground'}`}>
                  {STEP_LABELS[s]}
                </span>
              </div>
              {i < STEPS.length - 1 && <div className="w-6 h-px bg-border" />}
            </div>
          )
        })}
      </div>

      {/* ── Step: Choose ───────────────────────────────────────── */}
      {step === 'choose' && (
        <div className="space-y-4">
          <p className="text-sm text-muted-foreground">
            Choose your identity provider. Don't see yours? Use a generic option — it works with any
            OIDC or SAML 2.0 IdP.
          </p>
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
            {SSO_PRESETS.map((p) => (
              <button
                key={p.id}
                onClick={() => choose(p)}
                className="feature-card flex flex-col items-center justify-center gap-2 rounded-xl border border-border p-4 text-center hover:border-ring/40 transition-colors"
              >
                <ProviderLogo id={p.id} size={28} className="text-foreground" />
                <span className="text-sm font-medium text-foreground">{p.name}</span>
                <span className="text-[11px] text-muted-foreground">{p.blurb}</span>
                <div className="flex gap-1 mt-0.5">
                  {presetProtocols(p).map((proto) => (
                    <span
                      key={proto}
                      className="rounded bg-muted px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground"
                    >
                      {proto}
                    </span>
                  ))}
                </div>
              </button>
            ))}
          </div>
        </div>
      )}

      {/* ── Step: Configure ────────────────────────────────────── */}
      {step === 'configure' && preset && (
        <div className="space-y-5">
          <ProviderHeader preset={preset} />

          {presetProtocols(preset).length > 1 && (
            <div className="flex gap-1 rounded-lg bg-muted/30 p-1 w-fit">
              {presetProtocols(preset).map((proto) => (
                <button key={proto} onClick={() => switchProtocol(proto)}
                  className={`rounded-md px-3 py-1 text-xs font-medium transition-all ${
                    protocol === proto ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'
                  }`}>
                  {proto.toUpperCase()}
                </button>
              ))}
            </div>
          )}

          {protoPreset?.note && (
            <div className="flex items-start gap-2 rounded-lg border border-[var(--warning)]/30 bg-[var(--warning)]/10 p-3 text-sm">
              <AlertCircle size={15} className="text-[var(--warning)] shrink-0 mt-0.5" />
              <span className="text-foreground">{protoPreset.note}</span>
            </div>
          )}

          {/* SP values to paste into the IdP */}
          <div className="rounded-xl border border-border bg-muted/20 p-4 space-y-3">
            <p className="text-xs font-semibold text-foreground">Paste these into your IdP</p>
            {protocol === 'oidc' ? (
              <CopyField label="Redirect URI" value={redirectUri} />
            ) : (
              <>
                <CopyField label="ACS URL (Single sign-on URL)" value={acsUrl} />
                <CopyField label="SP Entity ID (Audience)" value={spEntityId} />
                <CopyField label="SP metadata" value={spEntityId} hint="Some IdPs can import this directly." />
              </>
            )}
          </div>

          {/* Connection fields */}
          {protocol === 'oidc' ? (
            <div className="space-y-4">
              <Field label="Issuer URL" hint="Must support OIDC discovery (.well-known/openid-configuration).">
                <input className={inputClass} type="url" value={config.issuerUrl ?? ''}
                  placeholder={protoPreset?.hint} onChange={(e) => set({ issuerUrl: e.target.value })} />
              </Field>
              <Field label="Client ID">
                <input className={inputClass} value={config.clientId ?? ''}
                  onChange={(e) => set({ clientId: e.target.value })} />
              </Field>
              <Field label="Client Secret">
                <div className="relative">
                  <input className={inputClass + ' pr-10'} type={showSecret ? 'text' : 'password'}
                    value={config.clientSecret ?? ''} onChange={(e) => set({ clientSecret: e.target.value })} />
                  <button type="button" onClick={() => setShowSecret(!showSecret)}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
                    {showSecret ? <EyeOff size={14} /> : <Eye size={14} />}
                  </button>
                </div>
              </Field>
            </div>
          ) : (
            <div className="space-y-4">
              <div className="flex gap-1 rounded-lg bg-muted/30 p-1 w-fit">
                {(['url', 'xml'] as const).map((m) => (
                  <button key={m} onClick={() => setMetaMode(m)}
                    className={`rounded-md px-3 py-1 text-xs font-medium transition-all ${
                      metaMode === m ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'
                    }`}>
                    {m === 'url' ? 'Metadata URL' : 'Upload XML'}
                  </button>
                ))}
              </div>
              {metaMode === 'url' ? (
                <Field label="IdP Metadata URL">
                  <input className={inputClass} type="url" value={config.metadataUrl ?? ''}
                    placeholder={protoPreset?.hint} onChange={(e) => set({ metadataUrl: e.target.value, metadataXml: '' })} />
                </Field>
              ) : (
                <Field label="IdP Metadata XML">
                  <label className="flex items-center gap-2 rounded-lg border border-dashed border-border px-3 py-3 text-sm text-muted-foreground cursor-pointer hover:border-ring/40 transition-colors">
                    <Upload size={14} />
                    {config.metadataXml ? 'XML loaded — choose a different file' : 'Choose a metadata.xml file'}
                    <input type="file" accept=".xml,application/xml,text/xml" className="hidden" onChange={onFile} />
                  </label>
                </Field>
              )}
              <Field label="SP Entity ID" hint="Optional — defaults to the SP metadata URL above.">
                <input className={inputClass} value={config.entityId ?? ''}
                  placeholder={spEntityId} onChange={(e) => set({ entityId: e.target.value })} />
              </Field>
            </div>
          )}

          {/* Setup steps */}
          {protoPreset && (
            <details className="rounded-lg border border-border bg-muted/10 p-3">
              <summary className="text-xs font-medium text-foreground cursor-pointer">Setup steps for {preset.name}</summary>
              <ol className="mt-2 space-y-1.5 text-xs text-muted-foreground list-decimal pl-4">
                {protoPreset.setupSteps.map((s, i) => <li key={i}>{s}</li>)}
              </ol>
              {preset.docUrl && (
                <a href={preset.docUrl} target="_blank" rel="noopener"
                  className="mt-2 inline-flex items-center gap-1 text-xs text-primary hover:underline">
                  Provider documentation <ExternalLink size={10} />
                </a>
              )}
            </details>
          )}

          <StepButtons onBack={goBack} onNext={() => setStep('test')} nextDisabled={!canConfigure} nextLabel="Continue to test" />
        </div>
      )}

      {/* ── Step: Test ─────────────────────────────────────────── */}
      {step === 'test' && (
        <div className="space-y-5">
          <p className="text-sm text-muted-foreground">
            We'll validate the connection without saving — running OIDC discovery or fetching SAML
            metadata — so you can fix any issues before activating.
          </p>

          <button onClick={runTest} disabled={testing}
            className="flex items-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm px-4 py-2 hover:opacity-90 disabled:opacity-50 transition-opacity">
            {testing ? <Loader2 size={14} className="animate-spin" /> : <CheckCircle2 size={14} />}
            {testResult ? 'Test again' : 'Test connection'}
          </button>

          {testResult && !testResult.ok && (
            <div className="flex items-start gap-2 rounded-lg border border-destructive/25 bg-destructive/10 p-3 text-sm">
              <AlertCircle size={15} className="text-destructive shrink-0 mt-0.5" />
              <span className="text-destructive">{testResult.error || 'Connection failed'}</span>
            </div>
          )}

          {testResult?.ok && (
            <div className="space-y-3 rounded-xl border border-[var(--success)]/30 bg-[var(--success)]/10 p-4">
              <div className="flex items-center gap-2 text-sm font-medium text-foreground">
                <CheckCircle2 size={15} className="text-[var(--success)]" /> Connection verified
              </div>
              {testResult.oidc && (
                <dl className="grid grid-cols-1 gap-1 text-xs text-muted-foreground">
                  <DetailRow label="Issuer" value={testResult.oidc.issuer} />
                  <DetailRow label="Authorization" value={testResult.oidc.authorizationEndpoint} />
                  <DetailRow label="Token" value={testResult.oidc.tokenEndpoint} />
                  {testResult.oidc.userinfoEndpoint && <DetailRow label="UserInfo" value={testResult.oidc.userinfoEndpoint} />}
                  {testResult.oidc.scopesSupported?.length ? (
                    <DetailRow label="Scopes" value={testResult.oidc.scopesSupported.join(', ')} />
                  ) : null}
                </dl>
              )}
              {testResult.saml && (
                <dl className="grid grid-cols-1 gap-1 text-xs text-muted-foreground">
                  <DetailRow label="IdP Entity ID" value={testResult.saml.idpEntityId} />
                  {testResult.saml.ssoUrl && <DetailRow label="SSO URL" value={testResult.saml.ssoUrl} />}
                </dl>
              )}
            </div>
          )}

          <div className="border-t border-border pt-4 space-y-3">
            <div>
              <p className="text-sm font-medium text-foreground">See exactly what your IdP sends</p>
              <p className="text-xs text-muted-foreground">
                Do a real sign-in in a popup — Flint captures the claims it receives, without creating
                a session. The surest way to verify your mapping before going live.
              </p>
            </div>
            <button onClick={runTestSignIn} disabled={signingIn}
              className="flex items-center gap-2 rounded-lg border border-border px-4 py-2 text-sm font-medium text-foreground hover:bg-accent/50 disabled:opacity-50 transition-colors">
              {signingIn ? <Loader2 size={14} className="animate-spin" /> : <LogIn size={14} />}
              {signingIn ? 'Waiting for sign-in…' : 'Test sign-in'}
            </button>
            {signInError && (
              <div className="flex items-start gap-2 rounded-lg border border-destructive/25 bg-destructive/10 p-3 text-sm">
                <AlertCircle size={15} className="text-destructive shrink-0 mt-0.5" />
                <span className="text-destructive">{signInError}</span>
              </div>
            )}
            {capturedClaims && (
              <div className="space-y-3 rounded-xl border border-[var(--success)]/30 bg-[var(--success)]/10 p-4">
                <div className="flex items-center gap-2 text-sm font-medium text-foreground">
                  <CheckCircle2 size={15} className="text-[var(--success)]" /> Captured from a real sign-in
                </div>
                <dl className="grid grid-cols-1 gap-1 text-xs text-muted-foreground">
                  <DetailRow label="Email" value={capturedClaims.email || '—'} />
                  <DetailRow label="Name" value={capturedClaims.name || '—'} />
                  <DetailRow label="Groups" value={capturedClaims.groups?.join(', ') || '—'} />
                </dl>
                <div>
                  <p className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground mb-1">
                    Raw claims your IdP sent
                  </p>
                  <div className="rounded-lg border border-border bg-muted/30 p-2 font-mono text-[11px] text-foreground max-h-48 overflow-auto space-y-0.5">
                    {Object.entries(capturedClaims.raw).map(([k, v]) => (
                      <div key={k} className="flex gap-2">
                        <span className="text-primary shrink-0">{k}</span>
                        <span className="text-muted-foreground break-all">{JSON.stringify(v)}</span>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            )}
          </div>

          <StepButtons onBack={goBack} onNext={() => setStep('map')} nextDisabled={!testResult?.ok}
            nextLabel="Continue to mapping" />
        </div>
      )}

      {/* ── Step: Map ──────────────────────────────────────────── */}
      {step === 'map' && (
        <div className="space-y-5">
          <p className="text-sm text-muted-foreground">
            Confirm how Flint reads identity from your IdP. These are pre-filled with the provider's
            conventions — override them only if your IdP differs.
          </p>

          {protocol === 'oidc' ? (
            <div className="space-y-4">
              <Field label="Scopes" hint="Space-separated. Requesting unsupported scopes can be rejected by some IdPs.">
                <input className={inputClass} value={toScopes(config.scopes)}
                  onChange={(e) => set({ scopes: fromScopes(e.target.value) })} />
              </Field>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <Field label="Email claim">
                  <input className={inputClass} value={config.emailClaim ?? ''} placeholder="email"
                    onChange={(e) => set({ emailClaim: e.target.value })} />
                </Field>
                <Field label="Name claim">
                  <input className={inputClass} value={config.nameClaim ?? ''} placeholder="name"
                    onChange={(e) => set({ nameClaim: e.target.value })} />
                </Field>
                <Field label="Groups claim">
                  <input className={inputClass} value={config.groupsClaim ?? ''} placeholder="groups"
                    onChange={(e) => set({ groupsClaim: e.target.value })} />
                </Field>
              </div>
              {testResult?.oidc?.claimsSupported?.length ? (
                <p className="text-xs text-muted-foreground">
                  IdP advertises claims: <span className="font-mono">{testResult.oidc.claimsSupported.join(', ')}</span>
                </p>
              ) : null}
            </div>
          ) : (
            <div className="space-y-4">
              <Field label="Email attribute(s)" hint="One per line. First match wins.">
                <textarea className={inputClass + ' min-h-[64px] font-mono text-xs'} value={toLines(config.emailAttributes)}
                  placeholder="email" onChange={(e) => set({ emailAttributes: fromLines(e.target.value) })} />
              </Field>
              <Field label="Name attribute(s)">
                <textarea className={inputClass + ' min-h-[64px] font-mono text-xs'} value={toLines(config.nameAttributes)}
                  placeholder="displayName" onChange={(e) => set({ nameAttributes: fromLines(e.target.value) })} />
              </Field>
              <Field label="Groups attribute(s)">
                <textarea className={inputClass + ' min-h-[64px] font-mono text-xs'} value={toLines(config.groupsAttributes)}
                  placeholder="groups" onChange={(e) => set({ groupsAttributes: fromLines(e.target.value) })} />
              </Field>
            </div>
          )}

          <StepButtons onBack={goBack} onNext={() => setStep('activate')} nextLabel="Review & activate" />
        </div>
      )}

      {/* ── Step: Activate ─────────────────────────────────────── */}
      {step === 'activate' && preset && (
        <div className="space-y-5">
          <div className="rounded-xl border border-border bg-muted/20 p-4 space-y-2">
            <div className="flex items-center gap-2">
              <ProviderLogo id={preset.id} size={20} className="text-foreground" />
              <span className="text-sm font-semibold text-foreground">{preset.name}</span>
              <span className="rounded-md bg-muted px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground">{protocol.toUpperCase()}</span>
            </div>
            <dl className="grid grid-cols-1 gap-1 text-xs text-muted-foreground pt-1">
              {protocol === 'oidc' ? (
                <>
                  <DetailRow label="Issuer" value={config.issuerUrl ?? ''} />
                  <DetailRow label="Scopes" value={toScopes(config.scopes)} />
                  <DetailRow label="Groups claim" value={config.groupsClaim || 'groups'} />
                </>
              ) : (
                <>
                  <DetailRow label="Metadata" value={config.metadataUrl || 'XML uploaded'} />
                  <DetailRow label="Groups attr" value={(config.groupsAttributes ?? []).join(', ') || 'defaults'} />
                </>
              )}
            </dl>
          </div>

          <p className="text-sm text-muted-foreground">
            Activating makes this provider live immediately — your team can sign in with it on the
            next visit to the login page.
          </p>

          {error && (
            <div className="flex items-start gap-2 rounded-lg border border-destructive/25 bg-destructive/10 p-3 text-sm">
              <AlertCircle size={15} className="text-destructive shrink-0 mt-0.5" />
              <span className="text-destructive">{error}</span>
            </div>
          )}

          <div className="flex items-center gap-3">
            <button onClick={goBack} className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-2 text-sm font-medium text-foreground hover:bg-accent/50 transition-colors">
              <ChevronLeft size={14} /> Back
            </button>
            <button onClick={activate} disabled={saving}
              className="flex items-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm px-4 py-2 hover:opacity-90 disabled:opacity-50 transition-opacity">
              {saving ? <Loader2 size={14} className="animate-spin" /> : <Check size={14} />}
              Save &amp; activate
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function ProviderHeader({ preset }: { preset: SSOPreset }) {
  return (
    <div className="flex items-center gap-3">
      <div className="flex h-10 w-10 items-center justify-center rounded-lg border border-border bg-muted/30">
        <ProviderLogo id={preset.id} size={22} className="text-foreground" />
      </div>
      <div>
        <h3 className="text-sm font-semibold text-foreground">{preset.name}</h3>
        <p className="text-xs text-muted-foreground">{preset.blurb}</p>
      </div>
    </div>
  )
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="block text-sm font-medium text-foreground mb-1">{label}</label>
      {children}
      {hint && <p className="text-xs text-muted-foreground mt-1">{hint}</p>}
    </div>
  )
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline gap-2">
      <dt className="shrink-0 w-28 text-muted-foreground/70">{label}</dt>
      <dd className="min-w-0 truncate font-mono text-foreground/90">{value}</dd>
    </div>
  )
}

function StepButtons({ onBack, onNext, nextDisabled, nextLabel }: {
  onBack: () => void; onNext: () => void; nextDisabled?: boolean; nextLabel: string
}) {
  return (
    <div className="flex items-center gap-3 pt-1">
      <button onClick={onBack} className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-2 text-sm font-medium text-foreground hover:bg-accent/50 transition-colors">
        <ChevronLeft size={14} /> Back
      </button>
      <button onClick={onNext} disabled={nextDisabled}
        className="flex items-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm px-4 py-2 hover:opacity-90 disabled:opacity-50 transition-opacity">
        {nextLabel} <ArrowRight size={14} />
      </button>
    </div>
  )
}
