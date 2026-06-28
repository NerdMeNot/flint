import { Check, Copy } from 'lucide-react'
import { useCopyToClipboard } from '#/hooks/use-copy-to-clipboard'

/**
 * A read-only labelled value with a copy button — used to surface the Service
 * Provider values (Redirect URI, ACS URL, SP Entity ID, metadata link) that the
 * admin must paste into their IdP. Surfacing these inline removes the single
 * most common SSO setup footgun.
 */
export function CopyField({ label, value, hint }: { label: string; value: string; hint?: string }) {
  const { copied, copy } = useCopyToClipboard()
  return (
    <div>
      <label className="block text-xs font-medium text-foreground mb-1">{label}</label>
      <div className="flex items-stretch gap-2">
        <code className="flex-1 min-w-0 truncate rounded-lg border border-border bg-muted/30 px-3 py-2 text-xs font-mono text-foreground select-all">
          {value}
        </code>
        <button
          type="button"
          onClick={() => copy(value)}
          aria-label={`Copy ${label}`}
          className="flex shrink-0 items-center gap-1.5 rounded-lg border border-border px-2.5 text-xs font-medium text-foreground hover:bg-accent/50 transition-colors"
        >
          {copied ? <Check size={13} className="text-[var(--success)]" /> : <Copy size={13} />}
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      {hint && <p className="text-xs text-muted-foreground mt-1">{hint}</p>}
    </div>
  )
}
