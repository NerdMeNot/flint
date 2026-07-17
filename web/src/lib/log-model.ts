// Log group model — turns a step's raw log lines into collapsible sections.
//
// Flint's convention (borrowed from Buildkite, which most CI users know):
//   `--- Title`   opens a section, collapsed by default
//   `+++ Title`   opens a section, expanded by default
//   `~~~ Title`   opens a de-emphasized section (setup/teardown noise), collapsed
// GitHub Actions' `::group::Title` / `::endgroup::` are recognized too so
// ported scripts group correctly.
//
// Groups carry a duration when timestamps are available: the span from their
// marker line to the start of the next group (or the last line of the log).

import { stripAnsi } from './ansi'

export interface LogEntry {
  content: string
  timestamp?: string
  stream?: string
}

export type GroupMarker = 'collapsed' | 'expanded' | 'muted'

export interface LogGroupModel {
  /** Section title (marker stripped). null = preamble lines before any marker. */
  title: string | null
  marker: GroupMarker | null
  /** 1-based global line number of the marker line (or first entry for preamble). */
  markerLine: number
  entries: Array<LogEntry & { line: number }>
  durationMs: number | null
}

function markerFor(plain: string): { marker: GroupMarker; title: string } | null {
  if (plain.startsWith('--- ')) return { marker: 'collapsed', title: plain.slice(4) }
  if (plain.startsWith('+++ ')) return { marker: 'expanded', title: plain.slice(4) }
  if (plain.startsWith('~~~ ')) return { marker: 'muted', title: plain.slice(4) }
  if (plain.startsWith('::group::')) return { marker: 'collapsed', title: plain.slice(9).trim() }
  return null
}

function ts(e: LogEntry | undefined): number | null {
  if (!e?.timestamp) return null
  const ms = Date.parse(e.timestamp)
  return Number.isNaN(ms) ? null : ms
}

export function buildLogModel(entries: LogEntry[]): LogGroupModel[] {
  const groups: LogGroupModel[] = []
  let current: LogGroupModel | null = null
  let markerEntry: LogEntry | undefined

  const flushDuration = (group: LogGroupModel, anchor: LogEntry | undefined, next: LogEntry | undefined) => {
    const start = ts(anchor) ?? ts(group.entries[0])
    const end = ts(next) ?? ts(group.entries[group.entries.length - 1])
    group.durationMs = start != null && end != null && end >= start ? end - start : null
  }

  entries.forEach((entry, i) => {
    const line = i + 1
    const plain = stripAnsi(entry.content)

    if (plain.startsWith('::endgroup::')) {
      // Close the current section; subsequent lines continue header-less.
      if (current) flushDuration(current, markerEntry, entry)
      current = null
      markerEntry = undefined
      return
    }

    const m = markerFor(plain)
    if (m) {
      if (current) flushDuration(current, markerEntry, entry)
      current = { title: m.title, marker: m.marker, markerLine: line, entries: [], durationMs: null }
      markerEntry = entry
      groups.push(current)
      return
    }

    if (!current) {
      current = { title: null, marker: null, markerLine: line, entries: [], durationMs: null }
      markerEntry = undefined
      groups.push(current)
    }
    current.entries.push({ ...entry, line })
  })

  if (current) flushDuration(current, markerEntry, undefined)
  return groups
}

/** Default open state for a group: expanded and preamble sections start open. */
export function defaultOpen(group: LogGroupModel): boolean {
  return group.marker === null || group.marker === 'expanded'
}
