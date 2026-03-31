import { Link, useRouterState } from '@tanstack/react-router'
import { useState, useEffect, createContext, useContext } from 'react'
import {
  LayoutDashboard,
  FolderGit2,
  Shield,
  Boxes,
  Settings,
  Users,
  KeyRound,
  ScrollText,
  PanelLeftClose,
  PanelLeftOpen,
  Menu,
  X,
} from 'lucide-react'

const navItems = [
  { to: '/', icon: LayoutDashboard, label: 'Dashboard' },
  { to: '/projects', icon: FolderGit2, label: 'Projects' },
  { to: '/runs/$id', icon: ScrollText, label: 'Runs', match: '/runs' },
  { to: '/gates', icon: Shield, label: 'Gates' },
  { to: '/workspaces', icon: Boxes, label: 'Workspaces' },
] as const

const bottomItems = [
  { to: '/teams', icon: Users, label: 'Teams' },
  { to: '/settings/roles', icon: KeyRound, label: 'Roles' },
  { to: '/settings', icon: Settings, label: 'Settings' },
] as const

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

  // Persist preference
  useEffect(() => {
    if (typeof window === 'undefined') return
    localStorage.setItem('flint-sidebar-collapsed', String(collapsed))
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

  const sidebarWidth = collapsed ? 'w-[60px]' : 'w-[220px]'

  const navContent = (
    <>
      {/* Brand */}
      <div className={`flex h-14 items-center border-b border-border ${collapsed ? 'justify-center px-2' : 'gap-2.5 px-5'}`}>
        <div className="flex h-7 w-7 items-center justify-center rounded-md font-bold text-sm shrink-0"
          style={{ background: 'linear-gradient(135deg, var(--ring), var(--success))', color: 'white', fontFamily: 'Fraunces, Georgia, serif' }}>
          F
        </div>
        {!collapsed && (
          <>
            <span className="display-title font-bold text-foreground text-base tracking-tight">
              Flint
            </span>
            <span className="ml-auto island-kicker !text-[0.6rem] !tracking-[0.12em] opacity-60">
              CI
            </span>
          </>
        )}
      </div>

      {/* Main Nav */}
      <nav className={`flex-1 overflow-y-auto py-3 space-y-0.5 ${collapsed ? 'px-1.5' : 'px-3'}`}>
        {navItems.map((item) => {
          const isActive = item.match
            ? currentPath.startsWith(item.match)
            : currentPath === item.to
          return (
            <Link
              key={item.label}
              to={item.to}
              params={item.to.includes('$id') ? { id: 'r-103' } : undefined}
              title={collapsed ? item.label : undefined}
              className={`group flex items-center whitespace-nowrap rounded-lg py-2 text-[0.82rem] font-medium transition-all duration-150 ${
                collapsed ? 'justify-center px-2' : 'gap-2.5 px-3'
              } ${
                isActive
                  ? 'text-white shadow-sm'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--link-bg-hover)]'
              }`}
              style={isActive ? activeStyle : undefined}
            >
              <item.icon size={16} strokeWidth={isActive ? 2.2 : 1.8} className="shrink-0" />
              {!collapsed && item.label}
            </Link>
          )
        })}
      </nav>

      {/* Bottom Nav */}
      <div className={`border-t border-border py-3 space-y-0.5 ${collapsed ? 'px-1.5' : 'px-3'}`}>
        {bottomItems.map((item) => {
          const isActive = currentPath === item.to || currentPath.startsWith(item.to)
          return (
            <Link
              key={item.label}
              to={item.to}
              title={collapsed ? item.label : undefined}
              className={`group flex items-center whitespace-nowrap rounded-lg py-2 text-[0.82rem] font-medium transition-all duration-150 ${
                collapsed ? 'justify-center px-2' : 'gap-2.5 px-3'
              } ${
                isActive
                  ? 'text-white shadow-sm'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--link-bg-hover)]'
              }`}
              style={isActive ? activeStyle : undefined}
            >
              <item.icon size={16} strokeWidth={1.8} className="shrink-0" />
              {!collapsed && item.label}
            </Link>
          )
        })}

        {/* Collapse toggle — desktop only */}
        <button
          type="button"
          onClick={() => setCollapsed(!collapsed)}
          className="hidden lg:flex w-full items-center whitespace-nowrap rounded-lg py-2 text-[0.82rem] font-medium text-muted-foreground hover:text-foreground hover:bg-[var(--link-bg-hover)] transition-all duration-150"
          style={collapsed ? { justifyContent: 'center', padding: '0.5rem' } : { gap: '0.625rem', padding: '0.5rem 0.75rem' }}
          title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          {collapsed
            ? <PanelLeftOpen size={16} strokeWidth={1.8} className="shrink-0" />
            : <><PanelLeftClose size={16} strokeWidth={1.8} className="shrink-0" />Collapse</>
          }
        </button>
      </div>
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
        className={`lg:hidden fixed inset-y-0 left-0 z-50 flex w-[260px] flex-col border-r border-border transition-transform duration-250 ease-out ${
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
        {navContent}
      </aside>
    </>
  )
}
