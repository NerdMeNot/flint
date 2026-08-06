import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Shield,
  ShieldCheck,
  GitBranch,
  GitCommit,
  User,
  X,
  ChevronLeft,
  ChevronRight,
  CheckCircle,
  XCircle,
  Loader2,
  Bell,
} from 'lucide-react'
import { client } from '#/lib/orpc'
import { PanelHeader } from './run-gantt'
import type { PipelineStep } from '#/lib/api/types'
import { BranchLabel, ShortSha } from '#/components/GitRef'

// Mocked approver pool until the real RBAC integration lands. The
// "you" entry is what makes the panel actionable; flip its `me` field
// to false to test the unauthorized state.
const APPROVERS = [
  { id: 'you', name: 'You', initials: 'YO', me: true, online: true },
  { id: 'maria', name: 'Maria Chen', initials: 'MC', me: false, online: true },
  { id: 'taro', name: 'Taro Sato', initials: 'TS', me: false, online: false },
  { id: 'alex', name: 'Alex Kim', initials: 'AK', me: false, online: false },
] as const

interface RunLike {
  id: string
  branch: string
  commitSha: string
  commitMessage: string
  triggeredBy: string
  environment?: string
}

interface GatePanelProps {
  step: PipelineStep
  steps: PipelineStep[]
  run: RunLike
  onSelectStep: (name: string) => void
  onBackToOverview: () => void
}

type DecisionState =
  | { kind: 'idle' }
  | { kind: 'approving' }
  | { kind: 'approved'; by: string; at: string; note?: string }
  | { kind: 'rejecting' }
  | { kind: 'rejected'; by: string; at: string; reason?: string }

export function GatePanel({
  step, steps, run, onSelectStep, onBackToOverview,
}: GatePanelProps) {
  const noteKey = `gate:${run.id}:${step.name}:note`
  const [note, setNote] = useState(() => {
    if (typeof window === 'undefined') return ''
    return localStorage.getItem(noteKey) ?? ''
  })
  useEffect(() => {
    if (typeof window === 'undefined') return
    if (note) localStorage.setItem(noteKey, note)
    else localStorage.removeItem(noteKey)
  }, [note, noteKey])

  const [decision, setDecision] = useState<DecisionState>({ kind: 'idle' })
  const me = APPROVERS.find((a) => a.me)
  const canApprove = !!me

  // Live "blocked Xm" ticker. 30s cadence is plenty — we're showing a
  // human-readable rounded value, not a stopwatch.
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 30_000)
    return () => clearInterval(id)
  }, [])

  const blockedMs = step.startedAt
    ? Math.max(0, now - new Date(step.startedAt).getTime())
    : 0
  const blockedLabel = formatBlockedFor(blockedMs)

  // Step navigation by keyboard, parallel to LogPanel.
  const idx = steps.findIndex((s) => s.name === step.name)
  const prev = idx > 0 ? steps[idx - 1] : null
  const next = idx < steps.length - 1 ? steps[idx + 1] : null

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const t = e.target as HTMLElement | null
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return
      if (e.key === 'ArrowLeft' && prev) { e.preventDefault(); onSelectStep(prev.name) }
      else if (e.key === 'ArrowRight' && next) { e.preventDefault(); onSelectStep(next.name) }
      else if (e.key === 'Escape') { e.preventDefault(); onBackToOverview() }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [prev, next, onSelectStep, onBackToOverview])

  // ─── decision actions ──────────────────────────────────────
  function approve() {
    setDecision({ kind: 'approving' })
    client.gates.approve({
      runId: run.id,
      stepName: step.name,
      comment: note || undefined,
    })
      .then(() => {
        setDecision({
          kind: 'approved',
          by: me?.name ?? 'You',
          at: new Date().toISOString(),
          note: note || undefined,
        })
        localStorage.removeItem(noteKey)
      })
      .catch(() => setDecision({ kind: 'idle' }))
  }
  function reject() {
    if (!note.trim()) return // a reason is required to reject
    setDecision({ kind: 'rejecting' })
    client.gates.reject({
      runId: run.id,
      stepName: step.name,
      reason: note,
    })
      .then(() => {
        setDecision({
          kind: 'rejected',
          by: me?.name ?? 'You',
          at: new Date().toISOString(),
          reason: note,
        })
        localStorage.removeItem(noteKey)
      })
      .catch(() => setDecision({ kind: 'idle' }))
  }
  function notifyApprovers() {
    // Stub — would post to a notification channel in a real system.
    console.info('[mock] notifying approvers for', run.id, step.name)
  }

  // ─── header subtitle reflects state ────────────────────────
  let headerSubtitle: React.ReactNode
  if (decision.kind === 'approved') {
    headerSubtitle = <span className="text-success font-medium">Approved · just now</span>
  } else if (decision.kind === 'rejected') {
    headerSubtitle = <span className="text-destructive font-medium">Rejected · just now</span>
  } else {
    headerSubtitle = (
      <>
        Awaiting approval{blockedLabel && <> · blocked {blockedLabel}</>}
      </>
    )
  }

  return (
    <div className="island-shell !p-0 overflow-hidden flex flex-col">
      <PanelHeader
        icon={<Shield size={14} className="text-warning" />}
        title="Approval gate"
        subtitle={headerSubtitle}
        toolbar={
          <NavToolbar
            prev={prev}
            next={next}
            onSelectStep={onSelectStep}
            onClose={onBackToOverview}
          />
        }
      />

      {decision.kind === 'approved' || decision.kind === 'rejected' ? (
        <DecidedBody
          decision={decision}
          environment={run.environment ?? 'production'}
        />
      ) : (
        <PendingBody
          step={step}
          run={run}
          note={note}
          onNoteChange={setNote}
          decision={decision}
          canApprove={canApprove}
          onApprove={approve}
          onReject={reject}
          onNotify={notifyApprovers}
        />
      )}
    </div>
  )
}

// ────────────────────────────────────────────────────────────
// Pending body — the "moment of decision" surface
// ────────────────────────────────────────────────────────────

function PendingBody({
  step, run, note, onNoteChange, decision, canApprove, onApprove, onReject, onNotify,
}: {
  step: PipelineStep
  run: RunLike
  note: string
  onNoteChange: (v: string) => void
  decision: DecisionState
  canApprove: boolean
  onApprove: () => void
  onReject: () => void
  onNotify: () => void
}) {
  const isBusy = decision.kind === 'approving' || decision.kind === 'rejecting'
  const env = run.environment ?? 'production'

  return (
    <div
      className="px-5 py-7 sm:px-7 sm:py-8 lg:px-10 lg:py-10"
      style={{
        // Warm "manuscript" sub-surface that breaks the cool ocean
        // theme and signals "this is the human's domain".
        background:
          'linear-gradient(165deg, color-mix(in oklab, var(--card), oklch(95% 0.03 80) 22%), color-mix(in oklab, var(--card), oklch(95% 0.03 80) 8%))',
        boxShadow: 'inset 3px 0 0 -1px color-mix(in oklab, var(--warning), transparent 70%)',
      }}
    >
      <p className="island-kicker !text-warning mb-3">Manual approval</p>
      <h2 className="display-title text-2xl sm:text-3xl lg:text-4xl text-foreground leading-[1.15]">
        Approve deployment to{' '}
        <span className="text-primary">{env}</span>
        ?
      </h2>
      <p className="text-sm text-muted-foreground mt-1.5 font-mono">{step.name}</p>

      <div className="mt-7 space-y-6">
        <Section label="Change summary">
          <p className="text-base text-foreground mb-2 leading-snug line-clamp-2" title={run.commitMessage}>{run.commitMessage}</p>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span className="flex items-center gap-1.5">
              <GitBranch size={12} />
              <BranchLabel branch={run.branch} icon={false} max="16rem" />
            </span>
            <span className="flex items-center gap-1.5">
              <GitCommit size={12} />
              <ShortSha sha={run.commitSha} />
            </span>
            <span className="flex items-center gap-1.5">
              <User size={12} />
              {run.triggeredBy}
            </span>
          </div>
        </Section>

        <Section label="Required by">
          <p className="text-sm text-foreground mb-3 leading-snug">
            Manual approval required for <span className="font-medium">{env}</span> deploys.
          </p>
          <ApproverChips />
        </Section>

        <Section label={canApprove ? 'Note (optional)' : 'Add context (optional)'}>
          <textarea
            value={note}
            onChange={(e) => onNoteChange(e.target.value)}
            placeholder={
              canApprove
                ? 'Comment on this decision — required if rejecting…'
                : 'Add a note for the eligible approvers…'
            }
            rows={3}
            disabled={isBusy}
            className="w-full rounded-lg border border-border bg-card px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40 transition-colors resize-y disabled:opacity-50"
          />
        </Section>
      </div>

      {/* Action surface — different shape based on whether viewer can act */}
      <div className="mt-8 pt-6 border-t border-border flex flex-col sm:flex-row sm:items-center gap-3">
        {canApprove ? (
          <>
            <button
              type="button"
              onClick={onReject}
              disabled={!note.trim() || isBusy}
              title={!note.trim() ? 'A reason is required to reject' : 'Reject this deployment'}
              className="inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-card px-4 py-2.5 text-sm font-medium text-muted-foreground hover:text-destructive hover:border-destructive disabled:opacity-40 disabled:pointer-events-none transition-colors"
            >
              {decision.kind === 'rejecting' ? (
                <Loader2 size={14} className="animate-spin" />
              ) : (
                <X size={14} />
              )}
              Reject
            </button>

            <div className="flex-1 sm:max-w-[420px]">
              <HoldToApprove onComplete={onApprove} disabled={isBusy} busy={decision.kind === 'approving'} />
            </div>

            <span className="text-[11px] text-muted-foreground/60 sm:ml-auto sm:text-right">
              Hold the approve button for a moment to confirm.
            </span>
          </>
        ) : (
          <>
            <p className="flex-1 text-sm text-muted-foreground">
              You don't have approval rights for {env} deploys.
            </p>
            <button
              type="button"
              onClick={onNotify}
              className="inline-flex items-center justify-center gap-1.5 rounded-lg px-4 py-2.5 text-sm font-medium text-white transition-colors"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
            >
              <Bell size={14} />
              Notify approvers
            </button>
          </>
        )}
      </div>
    </div>
  )
}

// ────────────────────────────────────────────────────────────
// Decided body — calm reflective state
// ────────────────────────────────────────────────────────────

function DecidedBody({
  decision, environment,
}: {
  decision: Extract<DecisionState, { kind: 'approved' | 'rejected' }>
  environment: string
}) {
  const isApproved = decision.kind === 'approved'
  const text = isApproved ? decision.note : decision.reason
  const accent = isApproved ? 'var(--success)' : 'var(--destructive)'

  return (
    <div
      className="px-5 py-7 sm:px-7 sm:py-8 lg:px-10 lg:py-10"
      style={{
        background:
          'linear-gradient(165deg, color-mix(in oklab, var(--card), oklch(95% 0.03 80) 18%), color-mix(in oklab, var(--card), oklch(95% 0.03 80) 6%))',
        boxShadow: `inset 3px 0 0 -1px color-mix(in oklab, ${accent}, transparent 65%)`,
      }}
    >
      <p
        className="island-kicker mb-3"
        style={{ color: accent }}
      >
        {isApproved ? 'Approved' : 'Rejected'}
      </p>
      <h2 className="display-title text-2xl sm:text-3xl lg:text-4xl text-foreground leading-[1.15]">
        {decision.by}{' '}
        <span style={{ color: accent }}>
          {isApproved ? 'approved' : 'rejected'}
        </span>{' '}
        deployment to{' '}
        <span className="text-primary">{environment}</span>.
      </h2>

      <div className="mt-7 space-y-6">
        {text && (
          <Section label={isApproved ? 'Note' : 'Reason'}>
            <blockquote
              className="text-base text-foreground leading-snug pl-4 border-l-2"
              style={{ borderColor: `color-mix(in oklab, ${accent}, transparent 60%)` }}
            >
              "{text}"
            </blockquote>
          </Section>
        )}

        <Section label="Decided">
          <p className="text-sm text-foreground">
            <span className="font-medium">{decision.by}</span>
            <span className="text-muted-foreground"> · just now</span>
          </p>
        </Section>
      </div>

      <div className="mt-8 pt-6 border-t border-border flex items-center gap-3">
        <span className="text-xs text-muted-foreground">
          The pipeline will {isApproved ? 'continue past this gate' : 'not proceed past this gate'}.
        </span>
      </div>
    </div>
  )
}

// ────────────────────────────────────────────────────────────
// Hold-to-approve — the differentiated micro-interaction
// ────────────────────────────────────────────────────────────

const HOLD_MS = 700

function HoldToApprove({
  onComplete, disabled, busy,
}: {
  onComplete: () => void
  disabled: boolean
  busy: boolean
}) {
  const [progress, setProgress] = useState(0)
  const [held, setHeld] = useState(false)
  const rafRef = useRef<number | null>(null)
  const startRef = useRef(0)
  const completedRef = useRef(false)

  const start = useCallback(() => {
    if (disabled || busy) return
    completedRef.current = false
    setHeld(true)
    startRef.current = performance.now()
    function tick(t: number) {
      const p = Math.min((t - startRef.current) / HOLD_MS, 1)
      setProgress(p)
      if (p >= 1) {
        cancelAnimationFrame(rafRef.current!)
        rafRef.current = null
        completedRef.current = true
        setHeld(false)
        // Leave the bar full for a beat, then fire.
        setTimeout(() => {
          setProgress(0)
          onComplete()
        }, 120)
      } else {
        rafRef.current = requestAnimationFrame(tick)
      }
    }
    rafRef.current = requestAnimationFrame(tick)
  }, [disabled, busy, onComplete])

  const cancel = useCallback(() => {
    if (completedRef.current) return
    setHeld(false)
    if (rafRef.current) cancelAnimationFrame(rafRef.current)
    rafRef.current = null
    setProgress(0)
  }, [])

  useEffect(() => () => {
    if (rafRef.current) cancelAnimationFrame(rafRef.current)
  }, [])

  const label = busy
    ? 'Approving…'
    : held
      ? progress >= 1 ? 'Approved' : 'Hold to approve…'
      : 'Hold to approve'

  return (
    <button
      type="button"
      onPointerDown={start}
      onPointerUp={cancel}
      onPointerLeave={cancel}
      onPointerCancel={cancel}
      onKeyDown={(e) => {
        if ((e.key === 'Enter' || e.key === ' ') && !held) {
          e.preventDefault()
          start()
        }
      }}
      onKeyUp={(e) => {
        if (e.key === 'Enter' || e.key === ' ') cancel()
      }}
      onBlur={cancel}
      disabled={disabled || busy}
      aria-label="Hold to approve deployment"
      className="relative w-full select-none flex items-center justify-center gap-2 rounded-lg px-5 py-2.5 text-sm font-semibold whitespace-nowrap overflow-hidden text-white disabled:opacity-50 disabled:pointer-events-none transition-shadow focus:outline-none focus-visible:ring-2 focus-visible:ring-ring/60"
      style={{
        background: 'color-mix(in oklab, var(--success), black 18%)',
      }}
    >
      <span
        className="absolute inset-y-0 left-0 origin-left"
        style={{
          width: '100%',
          background: 'color-mix(in oklab, var(--success), white 8%)',
          transform: `scaleX(${progress})`,
          transition: held ? 'none' : 'transform 240ms ease-out',
        }}
        aria-hidden
      />
      <span className="relative flex items-center gap-2">
        {busy ? <Loader2 size={15} className="animate-spin" /> : <ShieldCheck size={15} />}
        <span>{label}</span>
      </span>
    </button>
  )
}

// ────────────────────────────────────────────────────────────
// Building blocks
// ────────────────────────────────────────────────────────────

function Section({
  label, sublabel, children,
}: {
  label: string
  sublabel?: string
  children: React.ReactNode
}) {
  return (
    <section>
      <div className="flex items-baseline gap-2 mb-2">
        <h3 className="island-kicker">{label}</h3>
        {sublabel && (
          <span className="text-[11px] text-muted-foreground/60 normal-case tracking-normal font-normal">
            {sublabel}
          </span>
        )}
      </div>
      {children}
    </section>
  )
}

const CHIP_PALETTE = [
  'bg-blue-500/15 text-blue-500',
  'bg-emerald-500/15 text-emerald-500',
  'bg-violet-500/15 text-violet-500',
  'bg-amber-500/15 text-amber-500',
] as const

function ApproverChips() {
  return (
    <div className="flex items-center gap-2 flex-wrap">
      {APPROVERS.map((a, i) => {
        const colorClass = CHIP_PALETTE[i % CHIP_PALETTE.length]!
        return (
          <span
            key={a.id}
            title={`${a.name}${a.online ? ' · online' : ''}${a.me ? ' (you)' : ''}`}
            className={`inline-flex items-center gap-1.5 rounded-full pl-1 pr-2.5 py-0.5 text-xs font-medium border ${
              a.me
                ? 'border-primary bg-accent text-primary'
                : 'border-border bg-card text-foreground'
            }`}
          >
            <span
              className={`relative flex items-center justify-center w-5 h-5 rounded-full text-[9px] font-bold ${colorClass}`}
            >
              {a.initials}
              {a.online && (
                <span
                  className="absolute -bottom-px -right-px w-1.5 h-1.5 rounded-full ring-1"
                  style={{
                    background: 'var(--success)',
                    boxShadow: '0 0 0 1.5px var(--card)',
                  }}
                  aria-hidden
                />
              )}
            </span>
            {a.me ? 'You' : a.name}
          </span>
        )
      })}
    </div>
  )
}

function NavToolbar({
  prev, next, onSelectStep, onClose,
}: {
  prev: PipelineStep | null
  next: PipelineStep | null
  onSelectStep: (name: string) => void
  onClose: () => void
}) {
  return (
    <div className="flex items-center gap-1">
      <button
        type="button"
        onClick={() => prev && onSelectStep(prev.name)}
        disabled={!prev}
        title={prev ? `Previous: ${prev.name} (←)` : 'No previous step'}
        className="flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent disabled:opacity-25 disabled:pointer-events-none transition-colors"
      >
        <ChevronLeft size={14} />
      </button>
      <button
        type="button"
        onClick={() => next && onSelectStep(next.name)}
        disabled={!next}
        title={next ? `Next: ${next.name} (→)` : 'No next step'}
        className="flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent disabled:opacity-25 disabled:pointer-events-none transition-colors"
      >
        <ChevronRight size={14} />
      </button>
      <span className="block w-px h-5 bg-border mx-1.5" />
      <button
        type="button"
        onClick={onClose}
        title="Close (Esc)"
        className="flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
      >
        <X size={14} />
      </button>
    </div>
  )
}

// ────────────────────────────────────────────────────────────
// helpers
// ────────────────────────────────────────────────────────────

function formatBlockedFor(ms: number): string {
  if (ms < 60_000) return 'just now'
  const m = Math.floor(ms / 60_000)
  if (m < 60) return `${m}m`
  const h = Math.floor(m / 60)
  return h === 1 ? '1h' : `${h}h ${m % 60}m`
}

// Re-export icons that the route doesn't need so future callers have them.
export { CheckCircle, XCircle }
