import { Link, useRouterState } from '@tanstack/react-router'
import { useState, useEffect, createContext, useContext } from 'react'
import {
  LayoutDashboard,
  FolderGit2,
  Shield,
  Settings,
  ScrollText,
  Server,
  Workflow,
  ChevronsLeft,
  Menu,
  X,
  LogOut,
} from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { orpc } from '#/lib/orpc'
import { logout } from '#/lib/auth-token'
import { ScopeSelector } from './ScopeSelector'
import { CapabilitySwitcher, activeCapabilityID } from './CapabilitySwitcher'
import { ViewsNav } from './ViewsNav'

// A single NavItem type keeps both section arrays unionable so `section.items`
// is one array type (not a union of arrays, which breaks .map typing).
type NavItem = {
  to: '/ci' | '/ci/projects' | '/ci/runs' | '/ci/gates' | '/workflows' | '/fleet'
  icon: typeof LayoutDashboard
  label: string
  match: string
}

const navItems: NavItem[] = [
  { to: '/ci', icon: LayoutDashboard, label: 'Dashboard', match: '' },
  { to: '/ci/projects', icon: FolderGit2, label: 'Projects', match: '/ci/projects' },
  { to: '/ci/runs', icon: ScrollText, label: 'Runs', match: '/ci/runs' },
  { to: '/ci/gates', icon: Shield, label: 'Gates', match: '' },
  { to: '/fleet', icon: Server, label: 'Fleet', match: '/fleet' },
]

const workflowNavItems: NavItem[] = [
  { to: '/workflows', icon: Workflow, label: 'Runs', match: '/workflows' },
]

const bottomItems = [
  { to: '/settings' as const, icon: Settings, label: 'Admin', match: '/settings' },
]

const activeStyle = { background: 'color-mix(in oklab, var(--ring), black 35%)' }

// ── Sidebar context ──────────────────────────────────────
interface SidebarContextValue {
  collapsed: boolean
  setCollapsed: (v: boolean) => void
  mobileOpen: boolean
  setMobileOpen: (v: boolean) => void
}

const SidebarContext = createContext<SidebarContextValue>({
  collapsed: false,
  setCollapsed: () => {},
  mobileOpen: false,
  setMobileOpen: () => {},
})

export function useSidebar() {
  return useContext(SidebarContext)
}

export function SidebarProvider({ children }: { children: React.ReactNode }) {
  const [collapsed, setCollapsed] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)

  // Load saved preference
  useEffect(() => {
    if (typeof window === 'undefined') return
    const saved = localStorage.getItem('flint-sidebar-collapsed')
    if (saved === 'true') setCollapsed(true)
  }, [])

  // Persist preference + set CSS variable for modal positioning
  useEffect(() => {
    if (typeof window === 'undefined') return
    localStorage.setItem('flint-sidebar-collapsed', String(collapsed))
    document.documentElement.style.setProperty('--sidebar-width', collapsed ? '64px' : '240px')
  }, [collapsed])

  // Close mobile drawer on route change
  const routerState = useRouterState()
  useEffect(() => {
    setMobileOpen(false)
  }, [routerState.location.pathname])

  return (
    <SidebarContext.Provider value={{ collapsed, setCollapsed, mobileOpen, setMobileOpen }}>
      {children}
    </SidebarContext.Provider>
  )
}

// ── Mobile hamburger button ──────────────────────────────
export function MobileMenuButton() {
  const { setMobileOpen } = useSidebar()
  return (
    <button
      type="button"
      onClick={() => setMobileOpen(true)}
      className="lg:hidden flex items-center justify-center w-9 h-9 rounded-lg text-muted-foreground hover:text-foreground hover:bg-[var(--link-bg-hover)] transition-colors"
      aria-label="Open menu"
    >
      <Menu size={20} />
    </button>
  )
}

// ── Sidebar ──────────────────────────────────────────────
export function Sidebar() {
  const { collapsed, setCollapsed, mobileOpen, setMobileOpen } = useSidebar()
  const routerState = useRouterState()
  const currentPath = routerState.location.pathname
  // The active product determines which section nav (if any) renders below the
  // capability switcher. Admin (/settings) has its own shell, so no section here.
  const activeCap = activeCapabilityID(currentPath)
  const section =
    activeCap === 'ci'
      ? { label: 'CI', items: navItems }
      : activeCap === 'workflows'
        ? { label: 'Workflows', items: workflowNavItems }
        : null

  const sidebarWidth = collapsed ? 'w-[64px]' : 'w-[240px]'

  const navContent = (
    <>
      {/* Brand + collapse toggle */}
      <div className={`flex h-14 lg:h-16 items-center border-b border-border ${collapsed ? 'flex-col justify-center gap-1 px-2' : 'gap-2.5 px-5'}`}>
        <Link to="/" className="flex items-center gap-2.5 min-w-0" title="Home">
          <div className="flex h-7 w-7 lg:h-8 lg:w-8 items-center justify-center rounded-md font-bold text-sm lg:text-base shrink-0"
            style={{ background: 'var(--primary)', color: 'var(--primary-foreground)' }}>
            F
          </div>
          {!collapsed && (
            <span className="display-title font-bold text-foreground text-lg tracking-tight">
              Flint
            </span>
          )}
        </Link>
        {!collapsed && (
          <button
            type="button"
            onClick={() => setCollapsed(true)}
            className="hidden lg:flex ml-auto items-center justify-center w-6 h-6 rounded text-muted-foreground/40 hover:text-foreground hover:bg-[var(--link-bg-hover)] transition-colors"
            title="Collapse sidebar"
          >
            <ChevronsLeft size={14} />
          </button>
        )}
      </div>
      {/* Expand button — collapsed desktop only */}
      {collapsed && (
        <button
          type="button"
          onClick={() => setCollapsed(false)}
          className="hidden lg:flex items-center justify-center mx-auto my-1.5 w-8 h-5 rounded text-muted-foreground/30 hover:text-foreground hover:bg-[var(--link-bg-hover)] transition-colors"
          title="Expand sidebar"
        >
          <ChevronsLeft size={13} className="rotate-180" />
        </button>
      )}

      {/* Product switcher */}
      <CapabilitySwitcher collapsed={collapsed} />

      {/* Section nav — shown for the active product. */}
      {section ? (
        <nav className={`flex-1 overflow-y-auto pb-3 space-y-0.5 ${collapsed ? 'px-1.5' : 'px-3'}`}>
          {!collapsed && (
            <p className="px-1 pb-1 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground/45">{section.label}</p>
          )}
          {section.items.map((item) => {
            const isActive = item.match
              ? currentPath.startsWith(item.match)
              : currentPath === item.to
            return (
              <Link
                key={item.label}
                to={item.to}
                title={collapsed ? item.label : undefined}
                className={`group flex items-center whitespace-nowrap rounded-lg py-2 text-sm font-medium transition-all duration-150 ${
                  collapsed ? 'justify-center px-2' : 'gap-2.5 px-3'
                } ${
                  isActive
                    ? 'text-white'
                    : 'text-muted-foreground hover:text-foreground hover:bg-[var(--link-bg-hover)]'
                }`}
                style={isActive ? activeStyle : undefined}
              >
                <item.icon size={16} strokeWidth={isActive ? 2.2 : 1.8} className="shrink-0" />
                {!collapsed && item.label}
              </Link>
            )
          })}
          {activeCap === 'ci' && <ViewsNav collapsed={collapsed} />}
        </nav>
      ) : (
        <div className="flex-1" />
      )}

      {/* Bottom — Admin nav */}
      <div className={`border-t border-border pt-3 pb-1 space-y-0.5 ${collapsed ? 'px-1.5' : 'px-3'}`}>
        {bottomItems.map((item) => {
          const isActive = item.match
            ? currentPath.startsWith(item.match)
            : currentPath === item.to
          return (
            <Link
              key={item.label}
              to={item.to}
              title={collapsed ? item.label : undefined}
              className={`group flex items-center whitespace-nowrap rounded-lg py-2 text-sm font-medium transition-all duration-150 ${
                collapsed ? 'justify-center px-2' : 'gap-2.5 px-3'
              } ${
                isActive
                  ? 'text-white'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--link-bg-hover)]'
              }`}
              style={isActive ? activeStyle : undefined}
            >
              <item.icon size={16} strokeWidth={1.8} className="shrink-0" />
              {!collapsed && item.label}
            </Link>
          )
        })}
      </div>

      {/* User profile — anchored to bottom */}
      <UserProfile collapsed={collapsed} />
    </>
  )

  return (
    <>
      {/* Desktop sidebar */}
      <aside
        className={`hidden lg:flex fixed inset-y-0 left-0 z-30 flex-col border-r border-border transition-all duration-200 ease-out ${sidebarWidth}`}
        style={{ background: 'linear-gradient(180deg, var(--surface-strong), var(--surface))', backdropFilter: 'blur(12px)' }}
      >
        {navContent}
      </aside>

      {/* Mobile overlay */}
      {mobileOpen && (
        <div
          className="lg:hidden fixed inset-0 z-40 bg-black/40 backdrop-blur-sm"
          onClick={() => setMobileOpen(false)}
        />
      )}

      {/* Mobile drawer */}
      <aside
        className={`lg:hidden fixed inset-y-0 left-0 z-50 flex w-[82vw] max-w-[300px] flex-col border-r border-border transition-transform duration-250 ease-out ${
          mobileOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
        style={{ background: 'linear-gradient(180deg, var(--surface-strong), var(--surface))', backdropFilter: 'blur(16px)' }}
      >
        {/* Close button */}
        <button
          type="button"
          onClick={() => setMobileOpen(false)}
          className="absolute top-3 right-3 flex items-center justify-center w-8 h-8 rounded-lg text-muted-foreground hover:text-foreground hover:bg-[var(--link-bg-hover)] transition-colors"
          aria-label="Close menu"
        >
          <X size={18} />
        </button>
        {/* Brand (same as navContent) */}
        <div className="flex h-14 items-center border-b border-border gap-2.5 px-5">
          <div className="flex h-7 w-7 items-center justify-center rounded-md font-bold text-sm shrink-0"
            style={{ background: 'var(--primary)', color: 'var(--primary-foreground)' }}>
            F
          </div>
          <span className="display-title font-bold text-foreground text-lg tracking-tight">Flint</span>
        </div>
        {/* Scope filters — mobile only */}
        <div className="px-3 py-2.5 border-b border-border">
          <p className="text-[11px] font-semibold uppercase tracking-widest text-muted-foreground/40 mb-2 px-1">Scope</p>
          <ScopeSelector />
        </div>
        {/* Product switcher */}
        <CapabilitySwitcher />
        {/* Section nav */}
        {section ? (
          <nav className="flex-1 overflow-y-auto pb-3 space-y-0.5 px-3">
            <p className="px-1 pb-1 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground/45">{section.label}</p>
            {section.items.map((item) => {
              const isActive = item.match
                ? currentPath.startsWith(item.match)
                : currentPath === item.to
              return (
                <Link
                  key={item.label}
                  to={item.to}
                  className={`group flex items-center whitespace-nowrap rounded-lg py-2 text-sm font-medium transition-all duration-150 gap-2.5 px-3 ${
                    isActive
                      ? 'text-white'
                      : 'text-muted-foreground hover:text-foreground hover:bg-[var(--link-bg-hover)]'
                  }`}
                  style={isActive ? activeStyle : undefined}
                >
                  <item.icon size={16} strokeWidth={isActive ? 2.2 : 1.8} className="shrink-0" />
                  {item.label}
                </Link>
              )
            })}
            {activeCap === 'ci' && <ViewsNav collapsed={false} />}
          </nav>
        ) : (
          <div className="flex-1" />
        )}
        {/* Bottom — Admin nav */}
        <div className="border-t border-border pt-3 pb-1 space-y-0.5 px-3">
          {bottomItems.map((item) => {
            const isActive = item.match
              ? currentPath.startsWith(item.match)
              : currentPath === item.to
            return (
              <Link
                key={item.label}
                to={item.to}
                className={`group flex items-center whitespace-nowrap rounded-lg py-2 text-sm font-medium transition-all duration-150 gap-2.5 px-3 ${
                  isActive
                    ? 'text-white'
                    : 'text-muted-foreground hover:text-foreground hover:bg-[var(--link-bg-hover)]'
                }`}
                style={isActive ? activeStyle : undefined}
              >
                <item.icon size={16} strokeWidth={1.8} className="shrink-0" />
                {item.label}
              </Link>
            )
          })}
        </div>
        {/* User profile */}
        <UserProfile collapsed={false} />
      </aside>
    </>
  )
}

// ── User profile ────────────────────────────────────────
function UserProfile({ collapsed }: { collapsed: boolean }) {
  const { data } = useQuery(orpc.auth.me.queryOptions({}))
  const user = data as { userId?: string; name?: string; email?: string; role?: string } | undefined
  if (!user) return null

  const initials = (user.name ?? user.email ?? '?')
    .split(/[\s@]+/)
    .slice(0, 2)
    .map((s) => s[0]?.toUpperCase() ?? '')
    .join('')

  if (collapsed) {
    return (
      <div className="border-t border-border px-1.5 py-2.5">
        <Link
          to="/profile"
          title={user.name ?? user.email}
          className="flex items-center justify-center w-full rounded-lg py-1.5 hover:bg-[var(--link-bg-hover)] transition-colors"
        >
          <div
            className="flex h-7 w-7 items-center justify-center rounded-full text-[11px] font-bold shrink-0"
            style={{ background: 'color-mix(in oklab, var(--primary) 25%, var(--muted))', color: 'var(--foreground)' }}
          >
            {initials}
          </div>
        </Link>
      </div>
    )
  }

  return (
    <div className="border-t border-border px-3 py-2.5">
      <div className="flex items-center gap-2.5">
        <Link
          to="/profile"
          className="flex items-center gap-2.5 flex-1 min-w-0 rounded-lg px-1 py-1.5 -ml-1 hover:bg-[var(--link-bg-hover)] transition-colors"
        >
          <div
            className="flex h-7 w-7 items-center justify-center rounded-full text-[11px] font-bold shrink-0"
            style={{ background: 'color-mix(in oklab, var(--primary) 25%, var(--muted))', color: 'var(--foreground)' }}
          >
            {initials}
          </div>
          <div className="min-w-0">
            <p className="text-xs font-medium text-foreground truncate">{user.name}</p>
            <p className="text-[12px] text-muted-foreground truncate">{user.email}</p>
          </div>
        </Link>
        <button
          type="button"
          onClick={() => void logout()}
          title="Sign out"
          className="shrink-0 flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground/40 hover:text-destructive hover:bg-destructive-subtle transition-colors"
        >
          <LogOut size={14} />
        </button>
      </div>
    </div>
  )
}
