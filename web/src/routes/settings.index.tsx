import { createFileRoute, Link } from '@tanstack/react-router'
import {
  UserCircle, Users, KeyRound, Globe, Key,
  Boxes, Tag, ShieldCheck, Lock, Server, GitFork, ScrollText, ChevronRight,
} from 'lucide-react'
import { PageHeader } from '#/components/PageHeader'

export const Route = createFileRoute('/settings/')({
  component: SettingsOverview,
})

const groups = [
  {
    section: 'Identity',
    tiles: [
      { to: '/settings/users', icon: UserCircle, label: 'Users', desc: 'People with access to this organization' },
      { to: '/settings/teams', icon: Users, label: 'Teams', desc: 'Group users for access and ownership' },
      { to: '/settings/roles', icon: KeyRound, label: 'Roles', desc: 'Permission sets and their assignments' },
    ],
  },
  {
    section: 'Authentication',
    tiles: [
      { to: '/settings/sso', icon: Globe, label: 'SSO', desc: 'OIDC and SAML single sign-on' },
      { to: '/settings/api-keys', icon: Key, label: 'API Keys', desc: 'Org-level keys for the API and CLI' },
    ],
  },
  {
    section: 'Configuration',
    tiles: [
      { to: '/settings/workspaces', icon: Boxes, label: 'Workspaces', desc: 'Ownership partitions for projects' },
      { to: '/settings/tags', icon: Tag, label: 'Tags', desc: 'Curated key:value classification' },
      { to: '/settings/environments', icon: ShieldCheck, label: 'Environments', desc: 'Deploy targets and gates' },
      { to: '/settings/variables', icon: Lock, label: 'Variables', desc: 'Config and secrets, scoped by environment' },
    ],
  },
  {
    section: 'Infrastructure',
    tiles: [
      { to: '/settings/runners', icon: Server, label: 'Runners', desc: 'Runner pools that execute jobs' },
      { to: '/settings/connections', icon: GitFork, label: 'Connections', desc: 'Linked forges (GitHub, GitLab)' },
    ],
  },
  {
    section: 'Monitoring',
    tiles: [
      { to: '/settings/audit-log', icon: ScrollText, label: 'Audit Log', desc: 'A trail of who changed what' },
    ],
  },
] as const

function SettingsOverview() {
  return (
    <div className="space-y-6 rise-in">
      <PageHeader title="Settings" subtitle="Administer access, configuration, and infrastructure" />

      {groups.map((group) => (
        <section key={group.section} className="space-y-2">
          <p className="island-kicker">{group.section}</p>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {group.tiles.map((tile) => (
              <Link
                key={tile.to}
                to={tile.to}
                className="feature-card p-4 flex items-start gap-3 group"
              >
                <div
                  className="flex h-9 w-9 items-center justify-center rounded-lg shrink-0"
                  style={{ background: 'color-mix(in oklab, var(--primary) 12%, transparent)', color: 'var(--primary)' }}
                >
                  <tile.icon size={17} strokeWidth={1.9} />
                </div>
                <div className="min-w-0 flex-1">
                  <h3 className="text-sm font-semibold text-foreground group-hover:text-primary transition-colors">{tile.label}</h3>
                  <p className="text-xs text-muted-foreground mt-0.5 line-clamp-2">{tile.desc}</p>
                </div>
                <ChevronRight size={15} className="text-muted-foreground/40 group-hover:text-foreground transition-colors shrink-0 mt-0.5" />
              </Link>
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}
