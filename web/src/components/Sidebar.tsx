import { Link, useRouterState } from '@tanstack/react-router'
import {
  LayoutDashboard,
  FolderGit2,
  Shield,
  Boxes,
  Settings,
  Users,
  KeyRound,
  ScrollText,
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

export function Sidebar() {
  const routerState = useRouterState()
  const currentPath = routerState.location.pathname

  return (
    <aside className="fixed inset-y-0 left-0 z-30 flex w-[220px] flex-col border-r border-border"
      style={{ background: 'linear-gradient(180deg, var(--surface-strong), var(--surface))', backdropFilter: 'blur(12px)' }}>
      {/* Brand */}
      <div className="flex h-14 items-center gap-2.5 px-5 border-b border-border">
        <div className="flex h-7 w-7 items-center justify-center rounded-md font-bold text-sm"
          style={{ background: 'linear-gradient(135deg, var(--ring), var(--success))', color: 'white', fontFamily: 'Fraunces, Georgia, serif' }}>
          F
        </div>
        <span className="display-title font-bold text-foreground text-base tracking-tight">
          Flint
        </span>
        <span className="ml-auto island-kicker !text-[0.6rem] !tracking-[0.12em] opacity-60">
          CI
        </span>
      </div>

      {/* Main Nav */}
      <nav className="flex-1 overflow-y-auto px-3 py-3 space-y-0.5">
        {navItems.map((item) => {
          const isActive = item.match
            ? currentPath.startsWith(item.match)
            : currentPath === item.to
          return (
            <Link
              key={item.label}
              to={item.to}
              params={item.to.includes('$id') ? { id: 'r-103' } : undefined}
              className={`group flex items-center gap-2.5 rounded-lg px-3 py-2 text-[0.82rem] font-medium whitespace-nowrap transition-all duration-150 ${
                isActive
                  ? 'text-white shadow-sm'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--link-bg-hover)]'
              }`}
              style={isActive ? activeStyle : undefined}
            >
              <item.icon size={16} strokeWidth={isActive ? 2.2 : 1.8} />
              {item.label}
            </Link>
          )
        })}
      </nav>

      {/* Bottom Nav */}
      <div className="border-t border-border px-3 py-3 space-y-0.5">
        {bottomItems.map((item) => {
          const isActive = currentPath === item.to || currentPath.startsWith(item.to)
          return (
            <Link
              key={item.label}
              to={item.to}
              className={`group flex items-center gap-2.5 rounded-lg px-3 py-2 text-[0.82rem] font-medium whitespace-nowrap transition-all duration-150 ${
                isActive
                  ? 'text-white shadow-sm'
                  : 'text-muted-foreground hover:text-foreground hover:bg-[var(--link-bg-hover)]'
              }`}
              style={isActive ? activeStyle : undefined}
            >
              <item.icon size={16} strokeWidth={1.8} />
              {item.label}
            </Link>
          )
        })}
      </div>
    </aside>
  )
}
