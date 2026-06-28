import { Globe, ShieldCheck } from 'lucide-react'

// Monochrome provider marks. They use currentColor so they inherit the tile's
// text color and adapt to light/dark automatically. These are simplified,
// recognizable glyphs — not pixel-exact brand logos — kept visually consistent
// across the gallery.

interface LogoProps {
  size?: number
  className?: string
}

function Okta({ size = 24, className }: LogoProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" className={className} aria-hidden>
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="4" />
    </svg>
  )
}

function Entra({ size = 24, className }: LogoProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden>
      <rect x="3" y="3" width="8" height="8" rx="1" />
      <rect x="13" y="3" width="8" height="8" rx="1" opacity="0.7" />
      <rect x="3" y="13" width="8" height="8" rx="1" opacity="0.7" />
      <rect x="13" y="13" width="8" height="8" rx="1" />
    </svg>
  )
}

function Google({ size = 24, className }: LogoProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" className={className} aria-hidden>
      <path
        d="M21 12.2c0 5-3.4 8.3-8.4 8.3a8.5 8.5 0 1 1 5.7-14.8l-2.4 2.3A5.1 5.1 0 1 0 17.4 13H12.6v-3H21c.1.4.1.8.1 2.2Z"
        fill="currentColor"
      />
    </svg>
  )
}

function Auth0({ size = 24, className }: LogoProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden>
      <path d="M12 2 4 5v6.5c0 4.6 3.2 8 8 10.5 4.8-2.5 8-5.9 8-10.5V5l-8-3Zm0 2.3 6 2.2v5c0 3.4-2.2 6.1-6 8.2-3.8-2.1-6-4.8-6-8.2v-5l6-2.2Z" />
    </svg>
  )
}

function OneLogin({ size = 24, className }: LogoProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden>
      <rect x="4" y="4" width="16" height="16" rx="4" opacity="0.18" />
      <path d="M11 7h2.2v10H11v-7.4l-2 .6V8.4L11 7Z" />
    </svg>
  )
}

function Keycloak({ size = 24, className }: LogoProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" className={className} aria-hidden>
      <path d="M12 2.5 4 7v10l8 4.5L20 17V7l-8-4.5Z" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
      <circle cx="12" cy="10.5" r="2.2" fill="currentColor" />
      <path d="M12 12.5v4" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  )
}

function JumpCloud({ size = 24, className }: LogoProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden>
      <circle cx="12" cy="12" r="3.4" />
      <g stroke="currentColor" strokeWidth="2" strokeLinecap="round">
        <path d="M12 2.6v3M12 18.4v3M2.6 12h3M18.4 12h3M5.3 5.3l2.1 2.1M16.6 16.6l2.1 2.1M18.7 5.3l-2.1 2.1M7.4 16.6l-2.1 2.1" />
      </g>
    </svg>
  )
}

const MARKS: Record<string, (p: LogoProps) => React.ReactNode> = {
  okta: Okta,
  entra: Entra,
  google: Google,
  auth0: Auth0,
  onelogin: OneLogin,
  keycloak: Keycloak,
  jumpcloud: JumpCloud,
}

export function ProviderLogo({ id, size = 24, className }: { id: string; size?: number; className?: string }) {
  const Mark = MARKS[id]
  if (Mark) return <>{Mark({ size, className })}</>
  if (id === 'generic-saml') return <ShieldCheck size={size} className={className} aria-hidden />
  return <Globe size={size} className={className} aria-hidden />
}
