import { createFileRoute } from '@tanstack/react-router'
import { useState, useEffect } from 'react'
import { Globe, Eye, EyeOff, Check, Loader2, AlertCircle, ExternalLink } from 'lucide-react'

export const Route = createFileRoute('/settings/sso')({
  component: SSOSettings,
})

function SSOSettings() {
  const [activeTab, setActiveTab] = useState<'oidc' | 'saml'>('oidc')
  const [providers, setProviders] = useState<any>(null)
  const [, setLoading] = useState(true)

  useEffect(() => {
    const fetchProviders = async () => {
      try {
        const token = localStorage.getItem('flint_access_token')
        const res = await fetch('/api/v1/auth/providers', {
          headers: { 'Authorization': `Bearer ${token}` },
        })
        const data = await res.json()
        setProviders(data)
      } catch {
        // ignore
      } finally {
        setLoading(false)
      }
    }
    fetchProviders()
  }, [])

  return (
    <div className="max-w-2xl space-y-6">
      <div>
        <h1 className="display-title text-xl font-bold text-foreground">Single Sign-On</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Configure SSO to let your team sign in with their corporate identity provider
        </p>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 rounded-lg bg-muted/30 p-1">
        {(['oidc', 'saml'] as const).map((tab) => (
          <button
            key={tab}
            onClick={() => setActiveTab(tab)}
            className={`flex-1 flex items-center justify-center gap-2 rounded-md py-2 text-sm font-medium transition-all ${
              activeTab === tab
                ? 'bg-card text-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            <Globe size={14} />
            {tab.toUpperCase()}
            {providers && ((tab === 'oidc' && providers.oidcConfigured) || (tab === 'saml' && providers.samlConfigured)) && (
              <span className="flex h-2 w-2 rounded-full bg-[var(--success)]" />
            )}
          </button>
        ))}
      </div>

      {activeTab === 'oidc' ? <OIDCForm /> : <SAMLForm />}
    </div>
  )
}

function OIDCForm() {
  const [issuerUrl, setIssuerUrl] = useState('')
  const [clientId, setClientId] = useState('')
  const [clientSecret, setClientSecret] = useState('')
  const [showSecret, setShowSecret] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState(false)

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setSuccess(false)
    setLoading(true)

    try {
      const token = localStorage.getItem('flint_access_token')
      const res = await fetch('/api/v1/auth/provider', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
        body: JSON.stringify({
          providerType: 'oidc',
          displayName: 'OIDC',
          config: { issuerUrl, clientId, clientSecret },
        }),
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || 'Save failed')

      setSuccess(true)
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="island-shell p-5">
      <div className="flex items-center gap-2 mb-1">
        <h2 className="text-base font-semibold text-foreground">OpenID Connect</h2>
      </div>
      <p className="text-sm text-muted-foreground mb-5">
        Works with Okta, Azure AD, Google Workspace, Auth0, Keycloak, and any OIDC-compliant provider.
      </p>

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
          <span className="text-[var(--success)]">OIDC provider configured and activated</span>
        </div>
      )}

      <form onSubmit={handleSave} className="space-y-4">
        <div>
          <label className="block text-sm font-medium text-foreground mb-1">Issuer URL</label>
          <input type="url" value={issuerUrl} onChange={(e) => setIssuerUrl(e.target.value)}
            placeholder="https://acme.okta.com/oauth2/default" required
            className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow" />
          <p className="text-xs text-muted-foreground mt-1">
            Must support OIDC Discovery (<code className="text-xs">.well-known/openid-configuration</code>)
          </p>
        </div>

        <div>
          <label className="block text-sm font-medium text-foreground mb-1">Client ID</label>
          <input type="text" value={clientId} onChange={(e) => setClientId(e.target.value)}
            placeholder="0oa1b2c3d4e5f6..." required
            className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow" />
        </div>

        <div>
          <label className="block text-sm font-medium text-foreground mb-1">Client Secret</label>
          <div className="relative">
            <input type={showSecret ? 'text' : 'password'} value={clientSecret}
              onChange={(e) => setClientSecret(e.target.value)} placeholder="Enter client secret" required
              className="w-full rounded-lg border border-border bg-transparent px-3 py-2 pr-10 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow" />
            <button type="button" onClick={() => setShowSecret(!showSecret)}
              className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
              {showSecret ? <EyeOff size={14} /> : <Eye size={14} />}
            </button>
          </div>
        </div>

        <div className="flex items-center gap-3 pt-2">
          <button type="submit" disabled={loading || !issuerUrl || !clientId || !clientSecret}
            className="flex items-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm px-4 py-2 hover:opacity-90 disabled:opacity-50 transition-opacity">
            {loading ? <Loader2 size={14} className="animate-spin" /> : <Check size={14} />}
            Save & activate
          </button>
        </div>
      </form>
    </div>
  )
}

function SAMLForm() {
  const [metadataUrl, setMetadataUrl] = useState('')
  const [entityId, setEntityId] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState(false)

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setSuccess(false)
    setLoading(true)

    try {
      const token = localStorage.getItem('flint_access_token')
      const res = await fetch('/api/v1/auth/provider', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
        body: JSON.stringify({
          providerType: 'saml',
          displayName: 'SAML',
          config: { metadataUrl, entityId },
        }),
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || 'Save failed')

      setSuccess(true)
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="island-shell p-5">
      <div className="flex items-center gap-2 mb-1">
        <h2 className="text-base font-semibold text-foreground">SAML 2.0</h2>
      </div>
      <p className="text-sm text-muted-foreground mb-5">
        For enterprise IdPs that support SAML 2.0 (ADFS, Shibboleth, etc.)
      </p>

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
          <span className="text-[var(--success)]">SAML provider configured</span>
        </div>
      )}

      <form onSubmit={handleSave} className="space-y-4">
        <div>
          <label className="block text-sm font-medium text-foreground mb-1">IdP Metadata URL</label>
          <input type="url" value={metadataUrl} onChange={(e) => setMetadataUrl(e.target.value)}
            placeholder="https://idp.company.com/metadata" required
            className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow" />
        </div>

        <div>
          <label className="block text-sm font-medium text-foreground mb-1">Entity ID</label>
          <input type="text" value={entityId} onChange={(e) => setEntityId(e.target.value)}
            placeholder="urn:flint:sp" required
            className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none focus:ring-2 focus:ring-ring/40 transition-shadow" />
        </div>

        <div className="flex items-center gap-3 pt-2">
          <button type="submit" disabled={loading || !metadataUrl || !entityId}
            className="flex items-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm px-4 py-2 hover:opacity-90 disabled:opacity-50 transition-opacity">
            {loading ? <Loader2 size={14} className="animate-spin" /> : <Check size={14} />}
            Save & activate
          </button>
        </div>
      </form>

      <div className="mt-5 pt-4 border-t border-border">
        <p className="text-xs text-muted-foreground">
          Flint SP metadata is available at{' '}
          <a href="/auth/saml/metadata" target="_blank" rel="noopener"
            className="text-primary hover:underline inline-flex items-center gap-1">
            /auth/saml/metadata <ExternalLink size={10} />
          </a>
        </p>
      </div>
    </div>
  )
}
