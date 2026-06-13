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
  Tag,
} from 'lucide-react'

// A single union of route paths keeps `to` typed for TanStack Link while
// letting the groups share one item shape (mirrors Sidebar's NavItem pattern).
type AdminTo =
  | '/settings/users' | '/settings/teams' | '/settings/roles'
  | '/settings/sso' | '/settings/security' | '/settings/sessions' | '/settings/api-keys'
  | '/settings/workspaces' | '/settings/tags' | '/settings/environments' | '/settings/variables'
  | '/settings/runners' | '/settings/connections' | '/settings/audit-log'

type AdminItem = { to: AdminTo; icon: typeof UserCircle; label: string }
type AdminGroup = { section: string; items: AdminItem[] }

// Grouped so 14 destinations stay scannable. Order: who can get in → how they
// authenticate → what they configure → the infra it runs on → the trail.
const adminNav: AdminGroup[] = [
  {
    section: 'Identity',
    items: [
      { to: '/settings/users', icon: UserCircle, label: 'Users' },
      { to: '/settings/teams', icon: Users, label: 'Teams' },
      { to: '/settings/roles', icon: KeyRound, label: 'Roles' },
    ],
  },
  {
    section: 'Authentication',
    items: [
      { to: '/settings/sso', icon: Globe, label: 'SSO' },
      { to: '/settings/security', icon: Shield, label: 'Security' },
      { to: '/settings/sessions', icon: Monitor, label: 'Sessions' },
      { to: '/settings/api-keys', icon: Key, label: 'API Keys' },
    ],
  },
  {
    section: 'Configuration',
    items: [
      { to: '/settings/workspaces', icon: Boxes, label: 'Workspaces' },
      { to: '/settings/tags', icon: Tag, label: 'Tags' },
      { to: '/settings/environments', icon: ShieldCheck, label: 'Environments' },
      { to: '/settings/variables', icon: Lock, label: 'Variables' },
    ],
  },
  {
    section: 'Infrastructure',
    items: [
      { to: '/settings/runners', icon: Server, label: 'Runners' },
      { to: '/settings/connections', icon: GitFork, label: 'Connections' },
    ],
  },
  {
    section: 'Monitoring',
    items: [
      { to: '/settings/audit-log', icon: ScrollText, label: 'Audit Log' },
    ],
  },
]

const flatNav = adminNav.flatMap((g) => g.items)

export const Route = createFileRoute('/settings')({
  component: SettingsLayout,
})

function SettingsLayout() {
  const path = useRouterState({ select: (s) => s.location.pathname })

  return (
    <div className="rise-in space-y-6">
      {/* Mobile nav — horizontal scroll (flattened), hidden on desktop */}
      <nav aria-label="Admin sections" className="md:hidden flex items-center gap-1 overflow-x-auto border-b border-border pb-px -mb-px">
        {flatNav.map((item) => {
          const isActive = path === item.to || path.startsWith(item.to + '/')
          return (
            <Link
              key={item.to}
              to={item.to}
              aria-current={isActive ? 'page' : undefined}
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
        <nav aria-label="Admin sections" className="hidden md:flex flex-col shrink-0 w-[200px] space-y-4">
          <Link
            to="/"
            className="flex items-center gap-2 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors -mt-1"
          >
            <ArrowLeft size={13} />
            Back to Flint
          </Link>

          {adminNav.map((group) => (
            <div key={group.section} className="space-y-1">
              <p className="island-kicker !text-[10px] px-3 pb-0.5 opacity-60">{group.section}</p>
              {group.items.map((item) => {
                const isActive = path === item.to || path.startsWith(item.to + '/')
                return (
                  <Link
                    key={item.to}
                    to={item.to}
                    aria-current={isActive ? 'page' : undefined}
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
            </div>
          ))}
        </nav>

        {/* Content */}
        <div className="flex-1 min-w-0">
          <Outlet />
        </div>
      </div>
    </div>
  )
}
