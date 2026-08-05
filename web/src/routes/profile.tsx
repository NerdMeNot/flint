import { createFileRoute, Link, Outlet, useRouterState } from '@tanstack/react-router'
import { ArrowLeft, UserCircle, Shield, Monitor, Key } from 'lucide-react'

// The personal account area — its own left sub-nav, fully independent of the
// org Admin section. Self-service surfaces (security, sessions, tokens) live
// here so they never bounce the user into admin.
const accountNav = [
  { to: '/profile', icon: UserCircle, label: 'General', exact: true },
  { to: '/profile/security', icon: Shield, label: 'Security', exact: false },
  { to: '/profile/sessions', icon: Monitor, label: 'Sessions', exact: false },
  { to: '/profile/tokens', icon: Key, label: 'Access tokens', exact: false },
] as const

export const Route = createFileRoute('/profile')({
  component: AccountLayout,
})

function AccountLayout() {
  const path = useRouterState({ select: (s) => s.location.pathname })
  const isActive = (item: (typeof accountNav)[number]) =>
    item.exact ? path === item.to : path === item.to || path.startsWith(item.to + '/')

  return (
    <div className="rise-in space-y-6">
      {/* Mobile sub-nav — horizontal scroll */}
      <nav aria-label="Account sections" className="md:hidden flex items-center gap-1 overflow-x-auto border-b border-border pb-px -mb-px">
        {accountNav.map((item) => {
          const active = isActive(item)
          return (
            <Link
              key={item.to}
              to={item.to}
              aria-current={active ? 'page' : undefined}
              className={`flex items-center gap-1.5 shrink-0 px-3 py-2 text-xs font-medium border-b-2 transition-colors ${
                active ? 'border-primary text-primary' : 'border-transparent text-muted-foreground hover:text-foreground hover:border-border'
              }`}
            >
              <item.icon size={13} />
              {item.label}
            </Link>
          )
        })}
      </nav>

      <div className="flex gap-6">
        {/* Account sub-nav — desktop */}
        <nav aria-label="Account sections" className="hidden md:flex flex-col shrink-0 w-[200px] space-y-1">
          <Link
            to="/"
            className="flex items-center gap-2 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors mb-3 -mt-1"
          >
            <ArrowLeft size={13} />
            Back to Flint
          </Link>
          <p className="island-kicker !text-[10px] px-3 pb-0.5 opacity-60">Account</p>
          {accountNav.map((item) => {
            const active = isActive(item)
            return (
              <Link
                key={item.to}
                to={item.to}
                aria-current={active ? 'page' : undefined}
                className={`flex items-center gap-2.5 rounded-lg px-3 py-2 text-xs font-medium transition-colors ${
                  active ? 'text-white' : 'text-muted-foreground hover:text-foreground hover:bg-accent'
                }`}
                style={active ? { background: 'color-mix(in oklab, var(--ring), black 35%)' } : undefined}
              >
                <item.icon size={15} strokeWidth={active ? 2.2 : 1.8} className="shrink-0" />
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
