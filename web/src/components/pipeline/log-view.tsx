import { useMemo, useState } from 'react'
import { ChevronDown, Clock } from 'lucide-react'
import { parseAnsiLine, stripAnsi, type AnsiSegment, type AnsiStyle } from '#/lib/ansi'
import { buildLogModel, defaultOpen, type LogEntry, type LogGroupModel } from '#/lib/log-model'

// LogView — the shared log surface: ANSI-rendered lines, collapsible
// `---`/`+++`/`~~~` sections with per-section durations (when the sink
// recorded timestamps), a timestamp gutter, and search highlighting.
// It renders inside the dark #0d1117 terminal panel its parents provide.

interface LogViewProps {
  entries: LogEntry[]
  /** Lower-cased search query; matching lines are highlighted, groups auto-open. */
  search?: string
  /** Offsets line numbers when rendering a slice of a larger stream. */
  lineOffset?: number
  emptyText?: string
}

interface RenderedLine {
  line: number
  timestamp?: string
  stream?: string
  segments: AnsiSegment[]
  plain: string
}

function fmtGroupDur(ms: number): string {
  const secs = Math.round(ms / 1000)
  if (secs < 1) return '<1s'
  if (secs < 60) return `${secs}s`
  return `${Math.floor(secs / 60)}m ${secs % 60}s`
}

function fmtClock(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toTimeString().slice(0, 8)
}

function segmentStyle(style: AnsiStyle): React.CSSProperties | undefined {
  if (!style.fg && !style.bg && !style.bold && !style.dim && !style.italic && !style.underline && !style.strike) {
    return undefined
  }
  return {
    color: style.fg,
    backgroundColor: style.bg,
    fontWeight: style.bold ? 600 : undefined,
    opacity: style.dim ? 0.6 : undefined,
    fontStyle: style.italic ? 'italic' : undefined,
    textDecoration: [style.underline && 'underline', style.strike && 'line-through'].filter(Boolean).join(' ') || undefined,
  }
}

function highlightPlain(text: string, q: string): React.ReactNode {
  if (!q) return text
  const low = text.toLowerCase()
  const out: React.ReactNode[] = []
  let i = 0
  while (i < text.length) {
    const j = low.indexOf(q, i)
    if (j < 0) {
      out.push(text.slice(i))
      break
    }
    if (j > i) out.push(text.slice(i, j))
    out.push(
      <mark key={j} className="bg-yellow-500/40 text-inherit rounded-[2px]">
        {text.slice(j, j + q.length)}
      </mark>,
    )
    i = j + q.length
  }
  return out
}

function LineRow({ line, showTimestamps, search }: { line: RenderedLine; showTimestamps: boolean; search: string }) {
  const matched = search !== '' && line.plain.toLowerCase().includes(search)
  return (
    <div className={`flex gap-3 hover:bg-[#161b22] -mx-2 px-2 py-px rounded ${matched ? 'bg-yellow-500/5' : ''}`}>
      <span className={`select-none shrink-0 w-9 text-right ${line.stream === 'stderr' ? 'text-[#f85149]/60' : 'text-[#484f58]'}`}>
        {line.line}
      </span>
      {showTimestamps && (
        <span className="select-none shrink-0 text-[#484f58] tabular-nums" title={line.timestamp}>
          {line.timestamp ? fmtClock(line.timestamp) : '        '}
        </span>
      )}
      <span className="whitespace-pre-wrap break-all text-[#8b949e]">
        {line.segments.length === 0
          ? ' '
          : line.segments.map((seg, i) => (
              <span key={i} style={segmentStyle(seg.style)}>
                {search ? highlightPlain(seg.text, search) : seg.text}
              </span>
            ))}
      </span>
    </div>
  )
}

export function LogView({ entries, search = '', lineOffset = 0, emptyText = 'No output.' }: LogViewProps) {
  const q = search.toLowerCase().trim()
  const [overrides, setOverrides] = useState<Record<number, boolean>>({})
  const [showTimestamps, setShowTimestamps] = useState(false)

  // Parse once per content change: group structure + ANSI segments with style
  // state carried across every raw line (including collapsed ones).
  const { groups, rendered, hasTimestamps } = useMemo(() => {
    const groups = buildLogModel(entries)
    const rendered = new Map<number, RenderedLine>()
    let state: AnsiStyle = {}
    let hasTimestamps = false
    entries.forEach((entry, i) => {
      const { segments, state: next } = parseAnsiLine(entry.content, state)
      state = next
      if (entry.timestamp) hasTimestamps = true
      rendered.set(i + 1, {
        line: i + 1 + lineOffset,
        timestamp: entry.timestamp,
        stream: entry.stream,
        segments,
        plain: segments.map((s) => s.text).join(''),
      })
    })
    return { groups, rendered, hasTimestamps }
  }, [entries, lineOffset])

  const isOpen = (g: LogGroupModel, idx: number) =>
    q !== '' ? true : idx in overrides ? overrides[idx]! : defaultOpen(g)
  const toggle = (g: LogGroupModel, idx: number) =>
    setOverrides((o) => ({ ...o, [idx]: !(idx in o ? o[idx] : defaultOpen(g)) }))

  const hasGroups = groups.some((g) => g.title !== null)
  const allOpen = groups.every(isOpen)

  if (entries.length === 0) {
    return <span className="text-[#484f58] italic">{emptyText}</span>
  }

  const groupMatches = (g: LogGroupModel) =>
    g.entries.filter((e) => rendered.get(e.line)?.plain.toLowerCase().includes(q)).length

  return (
    <div>
      {(hasGroups || hasTimestamps) && (
        <div className="flex items-center justify-end gap-2 mb-1.5 -mt-1">
          {hasTimestamps && (
            <button
              type="button"
              onClick={() => setShowTimestamps((v) => !v)}
              className={`flex items-center gap-1 text-[11px] font-medium transition-colors px-1.5 ${
                showTimestamps ? 'text-[#c9d1d9]' : 'text-[#484f58] hover:text-[#8b949e]'
              }`}
              title="Toggle timestamps"
            >
              <Clock size={11} />
              Timestamps
            </button>
          )}
          {hasGroups && (
            <button
              type="button"
              onClick={() => setOverrides(Object.fromEntries(groups.map((_, i) => [i, !allOpen])))}
              className="text-[11px] font-medium text-[#484f58] hover:text-[#8b949e] transition-colors px-1.5"
            >
              {allOpen ? 'Collapse all' : 'Expand all'}
            </button>
          )}
        </div>
      )}

      {groups.map((group, idx) => {
        const open = isOpen(group, idx)
        const matches = q ? groupMatches(group) : 0
        if (q && group.title !== null && matches === 0) return null
        const muted = group.marker === 'muted'
        return (
          <div key={idx} className={muted ? 'opacity-70' : undefined}>
            {group.title !== null && (
              <button
                type="button"
                onClick={() => toggle(group, idx)}
                className="w-full flex items-center gap-2 -mx-2 px-2 py-1 rounded text-left sticky top-0 bg-[#0d1117] hover:bg-[#161b22] z-10"
              >
                <ChevronDown
                  size={13}
                  className={`text-[#8b949e] shrink-0 transition-transform ${open ? '' : '-rotate-90'}`}
                />
                <span className={`font-semibold ${muted ? 'text-[#8b949e]' : 'text-[#c9d1d9]'}`}>
                  {q ? highlightPlain(stripAnsi(group.title), q) : stripAnsi(group.title)}
                </span>
                <span className="ml-auto flex items-center gap-3 text-[11px] text-[#484f58] tabular-nums shrink-0">
                  {q !== '' && <span>{matches} match{matches === 1 ? '' : 'es'}</span>}
                  {!open && q === '' && <span>{group.entries.length} line{group.entries.length === 1 ? '' : 's'}</span>}
                  {group.durationMs != null && <span>{fmtGroupDur(group.durationMs)}</span>}
                </span>
              </button>
            )}
            {open &&
              group.entries.map((e) => {
                const r = rendered.get(e.line)
                if (!r) return null
                if (q && group.title === null && !r.plain.toLowerCase().includes(q)) return null
                return <LineRow key={e.line} line={r} showTimestamps={showTimestamps} search={q} />
              })}
          </div>
        )
      })}
    </div>
  )
}
