import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { ScrollText, Search, Copy, Download, Check, ChevronDown } from 'lucide-react'
import { useCopyToClipboard } from '#/hooks/use-copy-to-clipboard'
import { orpc } from '#/lib/orpc'
import { PanelHeader } from '#/components/pipeline/run-gantt'
import { LogView } from '#/components/pipeline/log-view'
import { stripAnsi } from '#/lib/ansi'
import { StatusIcon, formatDuration } from '#/components/run-detail/run-status'

export function AllLogsPanel({ runId, steps, toolbar }: { runId: string; steps: any[]; toolbar?: React.ReactNode }) {
  const { data } = useQuery(orpc.runs.logs.queryOptions({ input: { runId } }))
  const logsMap = data?.logs ?? {}
  const { copied, copy } = useCopyToClipboard()
  const [search, setSearch] = useState('')
  const [overrides, setOverrides] = useState<Record<string, boolean>>({})
  const q = search.toLowerCase().trim()

  // Started steps, in execution order. Collapse succeeded by default; expand
  // failed/running so the interesting output is visible without a click. A
  // search expands everything (and hides sections with no match).
  const started = steps
    .filter((s) => s.startedAt)
    .sort((a, b) => Date.parse(a.startedAt) - Date.parse(b.startedAt))

  const defaultOpen = (s: any) => s.status === 'failed' || s.status === 'running'
  const isOpen = (s: any) => (q ? true : s.name in overrides ? overrides[s.name] : defaultOpen(s))
  const toggle = (s: any) => setOverrides((o) => ({ ...o, [s.name]: !(s.name in o ? o[s.name] : defaultOpen(s)) }))

  const allOpen = started.every(isOpen)
  const setAll = (open: boolean) => setOverrides(Object.fromEntries(started.map((s) => [s.name, open])))

  const fullText = started.map((s) => `===== ${s.name} (${s.status}) =====\n${logsMap[s.name] ?? ''}`).join('\n\n')
  function download() {
    const blob = new Blob([fullText], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${runId}.log`
    a.click()
    URL.revokeObjectURL(url)
  }

  const visible = started.filter((s) => !q || stripAnsi(logsMap[s.name] ?? '').toLowerCase().includes(q))

  return (
    <div className="island-shell !p-0 overflow-hidden flex flex-col">
      <PanelHeader
        icon={<ScrollText size={14} className="text-primary" />}
        title="Output"
        subtitle={`${started.length} step${started.length === 1 ? '' : 's'}`}
        toolbar={toolbar}
      />

      <div className="flex items-center gap-2 px-3 sm:px-4 py-2 border-b border-border bg-muted">
        <div className="relative flex-1 max-w-xs">
          <Search size={12} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search all output…"
            className="w-full pl-7 pr-2 py-1 text-xs rounded-md border border-border bg-transparent text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-1 focus:ring-ring/40"
          />
        </div>
        <button
          type="button"
          onClick={() => setAll(!allOpen)}
          className="text-[11px] font-medium text-muted-foreground hover:text-foreground transition-colors px-1.5"
        >
          {allOpen ? 'Collapse all' : 'Expand all'}
        </button>
        <span className="block w-px h-4 bg-border" />
        <button
          type="button"
          onClick={() => copy(fullText)}
          title="Copy all output"
          className="flex items-center gap-1 text-[11px] font-medium text-muted-foreground hover:text-foreground transition-colors px-1.5"
        >
          {copied ? <Check size={12} className="text-success" /> : <Copy size={12} />}
          <span className="hidden sm:inline">{copied ? 'Copied' : 'Copy'}</span>
        </button>
        <button
          type="button"
          onClick={download}
          title="Download .log"
          className="flex items-center gap-1 text-[11px] font-medium text-muted-foreground hover:text-foreground transition-colors px-1.5"
        >
          <Download size={12} />
          <span className="hidden sm:inline">Download</span>
        </button>
      </div>

      <div className="bg-[#0d1117] overflow-auto max-h-[75vh] min-h-[300px]">
        {visible.length === 0 ? (
          <p className="p-6 text-center text-sm text-[#484f58]">
            {q ? 'No matching output.' : 'No output yet.'}
          </p>
        ) : (
          visible.map((s) => {
            const text = logsMap[s.name] ?? ''
            const lines = text ? text.split('\n') : []
            const matches = q ? lines.filter((l) => stripAnsi(l).toLowerCase().includes(q)).length : 0
            const open = isOpen(s)
            return (
              <div key={s.name} className="border-b border-[#21262d] last:border-0">
                <button
                  type="button"
                  onClick={() => toggle(s)}
                  className="w-full flex items-center gap-2 px-3 sm:px-4 py-2 hover:bg-[#161b22] text-left sticky top-0 bg-[#0d1117] z-10 border-b border-[#21262d]"
                >
                  <ChevronDown size={13} className={`text-[#8b949e] shrink-0 transition-transform ${open ? '' : '-rotate-90'}`} />
                  <StatusIcon status={s.status} size={13} />
                  <span className="font-mono text-sm text-[#c9d1d9]">{s.name}</span>
                  {s.startedAt && s.finishedAt && (
                    <span className="text-[11px] text-[#484f58]">{formatDuration(s.startedAt, s.finishedAt)}</span>
                  )}
                  <span className="ml-auto text-[11px] text-[#484f58]">
                    {q ? `${matches} match${matches === 1 ? '' : 'es'}` : `${lines.length} line${lines.length === 1 ? '' : 's'}`}
                  </span>
                </button>
                {open && (
                  <div className="px-3 sm:px-4 py-2 font-mono text-[13px] leading-[1.65]">
                    <LogView entries={lines.map((line) => ({ content: line }))} search={q} />
                  </div>
                )}
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Log panel — replaces overview when a step is selected
// ---------------------------------------------------------------------------
