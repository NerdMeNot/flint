import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import {
  Search,
  FolderGit2,
  ScrollText,
  LayoutDashboard,
  Workflow,
  Shield,
  Settings,
  ArrowRight,
  CornerDownLeft,
  Users,
  KeyRound,
  Globe,
  Variable,
  Server,
  GitFork,
  FileText,
  Cpu,
} from 'lucide-react'
import { orpc } from '#/lib/orpc'
import type { Project, PipelineRun } from '#/lib/api/types'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface PaletteItem {
  id: string
  label: string
  sublabel?: string
  icon: React.ReactNode
  to: string
  category: 'page' | 'project' | 'run'
  colour?: string
}

// ---------------------------------------------------------------------------
// Static pages for navigation
// ---------------------------------------------------------------------------

const pages: PaletteItem[] = [
  { id: 'nav-dashboard', label: 'Dashboard', icon: <LayoutDashboard size={15} />, to: '/ci', category: 'page' },
  { id: 'nav-projects', label: 'Projects', icon: <FolderGit2 size={15} />, to: '/ci/projects', category: 'page' },
  { id: 'nav-runs', label: 'Runs', icon: <ScrollText size={15} />, to: '/ci/runs', category: 'page' },
  { id: 'nav-gates', label: 'Gates', icon: <Shield size={15} />, to: '/ci/gates', category: 'page' },
  { id: 'nav-workflows', label: 'Workflows', icon: <Workflow size={15} />, to: '/workflows', category: 'page' },
  { id: 'nav-settings', label: 'Admin Settings', icon: <Settings size={15} />, to: '/settings', category: 'page' },
  { id: 'nav-workspaces', label: 'Workspaces', sublabel: 'Admin', icon: <Globe size={15} />, to: '/settings/workspaces', category: 'page' },
  { id: 'nav-users', label: 'Users', sublabel: 'Admin', icon: <Users size={15} />, to: '/settings/users', category: 'page' },
  { id: 'nav-teams', label: 'Teams', sublabel: 'Admin', icon: <Users size={15} />, to: '/settings/teams', category: 'page' },
  { id: 'nav-roles', label: 'Roles', sublabel: 'Admin', icon: <KeyRound size={15} />, to: '/settings/roles', category: 'page' },
  { id: 'nav-environments', label: 'Environments', sublabel: 'Admin', icon: <Server size={15} />, to: '/settings/environments', category: 'page' },
  { id: 'nav-variables', label: 'Variables', sublabel: 'Admin', icon: <Variable size={15} />, to: '/settings/variables', category: 'page' },
  { id: 'nav-apikeys', label: 'API Keys', sublabel: 'Admin', icon: <KeyRound size={15} />, to: '/settings/api-keys', category: 'page' },
  { id: 'nav-audit', label: 'Audit Log', sublabel: 'Admin', icon: <FileText size={15} />, to: '/settings/audit-log', category: 'page' },
  { id: 'nav-runners', label: 'Runners', sublabel: 'Admin', icon: <Cpu size={15} />, to: '/settings/runners', category: 'page' },
  { id: 'nav-connections', label: 'Connections', sublabel: 'Admin', icon: <GitFork size={15} />, to: '/settings/connections', category: 'page' },
]

// ---------------------------------------------------------------------------
// Hook: global Cmd+K listener
// ---------------------------------------------------------------------------

export function useCommandPalette() {
  const [open, setOpen] = useState(false)

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        setOpen((prev) => !prev)
      }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [])

  return { open, setOpen }
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface CommandPaletteProps {
  open: boolean
  onClose: () => void
}

export function CommandPalette({ open, onClose }: CommandPaletteProps) {
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const navigate = useNavigate()
  const [container, setContainer] = useState<HTMLDivElement | null>(null)

  // Portal container
  useEffect(() => {
    if (typeof document === 'undefined') return
    const el = document.createElement('div')
    document.body.appendChild(el)
    setContainer(el)
    return () => { document.body.removeChild(el) }
  }, [])

  // Lock body scroll when open
  useEffect(() => {
    if (!open) return
    document.body.style.overflow = 'hidden'
    return () => { document.body.style.overflow = '' }
  }, [open])

  // Reset state on open
  useEffect(() => {
    if (open) {
      setQuery('')
      setActiveIndex(0)
      setTimeout(() => inputRef.current?.focus(), 10)
    }
  }, [open])

  // Search API query
  const trimmed = query.trim()
  const { data: searchData } = useQuery({
    ...orpc.search.query.queryOptions({ input: { q: trimmed, limit: 6 } }),
    enabled: open && trimmed.length > 0,
  })

  // Build results list
  const items = useMemo<PaletteItem[]>(() => {
    const q = trimmed.toLowerCase()

    // Filter pages
    const matchedPages = q
      ? pages.filter(
          (p) =>
            p.label.toLowerCase().includes(q) ||
            (p.sublabel && p.sublabel.toLowerCase().includes(q)),
        )
      : pages.slice(0, 5) // show top pages when empty

    // Map search results
    const projectItems: PaletteItem[] =
      searchData?.projects?.map((p: Project) => ({
        id: `proj-${p.id}`,
        label: p.name,
        sublabel: p.repo,
        icon: <div className="w-2.5 h-2.5 rounded-full shrink-0" style={{ backgroundColor: p.colour }} />,
        to: `/ci/projects/${p.id}`,
        category: 'project' as const,
        colour: p.colour,
      })) ?? []

    const runItems: PaletteItem[] =
      searchData?.runs?.map((r: PipelineRun) => ({
        id: `run-${r.id}`,
        label: r.commitMessage.length > 50 ? r.commitMessage.slice(0, 50) + '...' : r.commitMessage,
        sublabel: `${r.projectName} / ${r.branch}`,
        icon: <RunStatusDot status={r.status} />,
        to: `/ci/runs/${r.id}`,
        category: 'run' as const,
      })) ?? []

    if (!q) return matchedPages
    return [...projectItems, ...runItems, ...matchedPages]
  }, [trimmed, searchData])

  // Reset active index when items change
  useEffect(() => {
    setActiveIndex(0)
  }, [items.length, trimmed])

  // Navigate to selected item
  const go = useCallback(
    (item: PaletteItem) => {
      onClose()
      navigate({ to: item.to })
    },
    [navigate, onClose],
  )

  // Keyboard navigation
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
      } else if (e.key === 'ArrowDown') {
        e.preventDefault()
        setActiveIndex((i) => (i + 1) % Math.max(items.length, 1))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setActiveIndex((i) => (i - 1 + items.length) % Math.max(items.length, 1))
      } else if (e.key === 'Enter') {
        e.preventDefault()
        if (items[activeIndex]) go(items[activeIndex])
      }
    },
    [items, activeIndex, go, onClose],
  )

  // Scroll active item into view
  useEffect(() => {
    if (!listRef.current) return
    const active = listRef.current.querySelector('[data-active="true"]')
    if (active) active.scrollIntoView({ block: 'nearest' })
  }, [activeIndex])

  if (!open || !container) return null

  // Group items by category
  const groups: { label: string; items: PaletteItem[] }[] = []
  const projectGroup = items.filter((i) => i.category === 'project')
  const runGroup = items.filter((i) => i.category === 'run')
  const pageGroup = items.filter((i) => i.category === 'page')
  if (projectGroup.length) groups.push({ label: 'Projects', items: projectGroup })
  if (runGroup.length) groups.push({ label: 'Runs', items: runGroup })
  if (pageGroup.length) groups.push({ label: trimmed ? 'Pages' : 'Quick Navigation', items: pageGroup })

  // Flat index helper
  let flatIdx = 0

  return createPortal(
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 z-[110] bg-black/50 backdrop-blur-[2px]"
        onClick={onClose}
      />
      {/* Palette */}
      <div className="fixed inset-0 z-[110] flex items-start justify-center pt-[15vh] px-4 pointer-events-none">
        <div
          className="w-full max-w-xl rounded-xl border border-border overlay-edge overflow-hidden pointer-events-auto rise-in"
          style={{ background: 'var(--surface-strong)', animationDuration: '250ms' }}
          onKeyDown={handleKeyDown}
        >
          {/* Search input */}
          <div className="flex items-center gap-3 px-4 border-b border-border">
            <Search size={16} className="text-muted-foreground shrink-0" />
            <input
              ref={inputRef}
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search projects, runs, pages..."
              className="flex-1 py-3.5 text-sm bg-transparent text-foreground placeholder:text-muted-foreground/50 focus:outline-none"
            />
            <kbd className="hidden sm:inline-flex items-center gap-0.5 px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground/60 border border-border rounded">
              ESC
            </kbd>
          </div>

          {/* Results */}
          <div ref={listRef} className="max-h-[50vh] overflow-y-auto py-2">
            {items.length === 0 ? (
              <div className="px-4 py-8 text-center text-sm text-muted-foreground">
                No results for "{trimmed}"
              </div>
            ) : (
              groups.map((group) => (
                <div key={group.label}>
                  <div className="px-4 pt-2 pb-1">
                    <span className="text-[11px] font-semibold uppercase tracking-widest text-muted-foreground/50">
                      {group.label}
                    </span>
                  </div>
                  {group.items.map((item) => {
                    const idx = flatIdx++
                    const isActive = idx === activeIndex
                    return (
                      <button
                        key={item.id}
                        type="button"
                        data-active={isActive}
                        onClick={() => go(item)}
                        onMouseEnter={() => setActiveIndex(idx)}
                        className={`w-full flex items-center gap-3 px-4 py-2.5 text-left transition-colors ${
                          isActive
                            ? 'bg-accent text-foreground'
                            : 'text-muted-foreground hover:text-foreground'
                        }`}
                      >
                        <span className={`shrink-0 ${isActive ? 'text-primary' : ''}`}>
                          {item.icon}
                        </span>
                        <div className="flex-1 min-w-0">
                          <span className="text-sm font-medium truncate block">{item.label}</span>
                          {item.sublabel && (
                            <span className="text-xs text-muted-foreground/60 font-mono truncate block">
                              {item.sublabel}
                            </span>
                          )}
                        </div>
                        {isActive && (
                          <ArrowRight size={13} className="text-primary shrink-0" />
                        )}
                      </button>
                    )
                  })}
                </div>
              ))
            )}
          </div>

          {/* Footer hints */}
          <div className="flex items-center gap-4 px-4 py-2.5 border-t border-border text-[11px] text-muted-foreground/40">
            <span className="flex items-center gap-1">
              <kbd className="px-1 py-0.5 border border-border rounded text-[11px]">&uarr;&darr;</kbd>
              navigate
            </span>
            <span className="flex items-center gap-1">
              <CornerDownLeft size={10} />
              open
            </span>
            <span className="flex items-center gap-1">
              <kbd className="px-1 py-0.5 border border-border rounded text-[11px]">esc</kbd>
              close
            </span>
          </div>
        </div>
      </div>
    </>,
    container,
  )
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function RunStatusDot({ status }: { status: PipelineRun['status'] }) {
  const colors: Record<PipelineRun['status'], string> = {
    succeeded: 'var(--success)',
    failed: 'var(--destructive)',
    running: 'var(--primary)',
    pending: 'var(--muted-foreground)',
    paused: 'var(--warning)',
    cancelled: 'var(--muted-foreground)',
  }
  return (
    <div
      className="w-2.5 h-2.5 rounded-full shrink-0"
      style={{ backgroundColor: colors[status] ?? 'var(--muted-foreground)' }}
    />
  )
}
