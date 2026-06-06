import { createFileRoute, Link, Outlet, useRouterState } from '@tanstack/react-router'
import {
  ArrowLeft,
  Boxes,
  Users,
  UserCircle,
  KeyRound,
  ShieldCheck,
  Lock,
  Key,
  Server,
  GitFork,
  ScrollText,
  Globe,
  Shield,
  Monitor,
} from 'lucide-react'

const adminNav = [
  { to: '/settings/workspaces', icon: Boxes, label: 'Workspaces' },
  { to: '/settings/users', icon: UserCircle, label: 'Users' },
  { to: '/settings/teams', icon: Users, label: 'Teams' },
  { to: '/settings/roles', icon: KeyRound, label: 'Roles' },
  { to: '/settings/environments', icon: ShieldCheck, label: 'Environments' },
  { to: '/settings/variables', icon: Lock, label: 'Variables' },
  { to: '/settings/api-keys', icon: Key, label: 'API Keys' },
  { to: '/settings/sso', icon: Globe, label: 'SSO' },
  { to: '/settings/security', icon: Shield, label: 'Security' },
  { to: '/settings/sessions', icon: Monitor, label: 'Sessions' },
  { to: '/settings/runners', icon: Server, label: 'Runners' },
  { to: '/settings/connections', icon: GitFork, label: 'Connections' },
  { to: '/settings/audit-log', icon: ScrollText, label: 'Audit Log' },
] as const

export const Route = createFileRoute('/settings')({
  component: SettingsLayout,
})

function SettingsLayout() {
  const path = useRouterState({ select: (s) => s.location.pathname })

  return (
    <div className="rise-in space-y-6">
      {/* Mobile nav — horizontal scroll, hidden on desktop */}
      <nav className="md:hidden flex items-center gap-1 overflow-x-auto border-b border-border pb-px -mb-px">
        {adminNav.map((item) => {
          const isActive = path === item.to || path.startsWith(item.to + '/')
          return (
            <Link
              key={item.to}
              to={item.to}
              className={`flex items-center gap-1.5 shrink-0 px-3 py-2 text-xs font-medium border-b-2 transition-colors ${
                isActive
                  ? 'border-primary text-primary'
                  : 'border-transparent text-muted-foreground hover:text-foreground hover:border-border'
              }`}
            >
              <item.icon size={13} />
              {item.label}
            </Link>
          )
        })}
      </nav>

      {/* Desktop: sidebar + content side by side */}
      <div className="flex gap-6">
        {/* Admin sidebar — hidden on mobile */}
        <nav className="hidden md:flex flex-col shrink-0 w-[200px] space-y-1">
          <Link
            to="/"
            className="flex items-center gap-2 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors mb-4 -mt-1"
          >
            <ArrowLeft size={13} />
            Back to Flint
          </Link>

          <h2 className="text-xs font-semibold text-foreground tracking-wide uppercase opacity-50 px-3 pb-1">
            Admin
          </h2>

          {adminNav.map((item) => {
            const isActive = path === item.to || path.startsWith(item.to + '/')
            return (
              <Link
                key={item.to}
                to={item.to}
                className={`flex items-center gap-2.5 rounded-lg px-3 py-2 text-xs font-medium transition-colors ${
                  isActive
                    ? 'text-white shadow-sm'
                    : 'text-muted-foreground hover:text-foreground hover:bg-accent'
                }`}
                style={isActive ? { background: 'color-mix(in oklab, var(--ring), black 35%)' } : undefined}
              >
                <item.icon size={15} strokeWidth={isActive ? 2.2 : 1.8} className="shrink-0" />
                {item.label}
              </Link>
            )
          })}
        </nav>

        {/* Content */}
        <div className="flex-1 min-w-0">
          <Outlet />
        </div>
      </div>
    </div>
  )
}
