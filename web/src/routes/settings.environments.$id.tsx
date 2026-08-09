import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { ArrowLeft, Variable, Lock, FolderGit2, KeyRound, Eye, EyeOff, AlertTriangle } from 'lucide-react'
import { useState } from 'react'
import { orpc } from '#/lib/orpc'
import { ScopeBadges } from '#/components/ScopeBadges'
import { formatTime } from '#/lib/format-time'

export const Route = createFileRoute('/settings/environments/$id')({
  component: EnvironmentDetailPage,
})

function EnvironmentDetailPage() {
  const { id } = Route.useParams()

  const { data: envData } = useSuspenseQuery(orpc.environments.list.queryOptions({ input: {} }))
  const environments = envData.items
  const { data: varsData } = useSuspenseQuery(orpc.envVariables.list.queryOptions({ input: {} }))
  const variables = varsData.items
  const { data: valuesData } = useSuspenseQuery(orpc.envVariables.values.queryOptions({ input: {} }))
  const values = valuesData.items
  const { data: projectsData } = useSuspenseQuery(orpc.projects.list.queryOptions({ input: {} }))
  const projects = projectsData.items
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items
  // Must be fetched with the other queries, ABOVE the not-found early return:
  // a hook called after a conditional return runs on some renders and not
  // others, and React fails the whole tree with "rendered fewer hooks than
  // expected" the moment someone opens an environment id that doesn't exist.
  const { data: allRuns } = useSuspenseQuery(
    orpc.runs.list.queryOptions({ input: { limit: 50 } }),
  )

  const env = environments.find((e) => e.id === id)
  if (!env) {
    return (
      <div className="space-y-3">
        <Link to="/settings/environments" className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors">
          <ArrowLeft size={13} /> All environments
        </Link>
        <p className="text-sm text-muted-foreground">Environment not found.</p>
      </div>
    )
  }

  // Variables for this environment
  const envVars = variables.filter((v) => v.scope === 'environment')
  const valueMap = new Map<string, string>()
  for (const v of values) {
    if (v.environmentId === id) {
      valueMap.set(v.variableId, v.value)
    }
  }

  // Projects that reference this environment (match by env name/slug in pipeline config)
  // In the mock, projects have a workspace field. We match on environment field from runs.
  const projectIdsInEnv = new Set(
    allRuns.items.filter((r) => r.environment === env.name).map((r) => r.projectId),
  )
  const envProjects = projects.filter((p) => projectIdsInEnv.has(p.id))

  // Roles scoped to this environment
  const scopedRoles = roles.filter((r) => r.environments.includes(env.slug) || r.environments.includes(env.name))

  return (
    <div className="space-y-6">
      <Link
        to="/settings/environments"
        className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
      >
        <ArrowLeft size={13} />
        All environments
      </Link>

      {/* Header */}
      <div className="island-shell p-4 sm:p-5">
        <h2 className="display-title text-lg font-bold text-foreground">{env.name}</h2>
        <p className="text-sm text-muted-foreground font-mono mt-0.5">{env.slug}</p>
        <p className="text-xs text-muted-foreground mt-1">Created {formatTime(env.createdAt)}</p>
      </div>

      {/* Variables */}
      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <Variable size={14} className="text-muted-foreground" />
          <h3 className="text-xs font-semibold text-foreground">Variables</h3>
          <span className="text-[11px] text-muted-foreground opacity-50">
            {envVars.filter((v) => valueMap.has(v.id)).length}/{envVars.length} set
          </span>
        </div>

        {envVars.length === 0 ? (
          <p className="text-xs text-muted-foreground opacity-50 pl-5">No environment variables defined.</p>
        ) : (
          <div className="island-shell !p-0 overflow-hidden">
            <div className="divide-y divide-border/50">
              {envVars.map((variable) => {
                const value = valueMap.get(variable.id)
                return (
                  <EnvVarRow
                    key={variable.id}
                    name={variable.name}
                    description={variable.description}
                    isSecret={variable.isSecret}
                    value={value}
                  />
                )
              })}
            </div>
          </div>
        )}
      </section>

      {/* Projects targeting this environment */}
      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <FolderGit2 size={14} className="text-muted-foreground" />
          <h3 className="text-xs font-semibold text-foreground">Projects</h3>
          <span className="text-[11px] text-muted-foreground opacity-50">{envProjects.length}</span>
        </div>

        {envProjects.length === 0 ? (
          <p className="text-xs text-muted-foreground opacity-50 pl-5">No projects have deployed to this environment yet.</p>
        ) : (
          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
            {envProjects.map((project) => (
              <Link
                key={project.id}
                to="/ci/projects/$id"
                params={{ id: project.id }}
                className="island-shell p-3 flex items-center gap-3 hover:bg-accent transition-colors group"
              >
                <div className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: project.colour }} />
                <div className="min-w-0">
                  <p className="text-sm font-medium text-foreground group-hover:text-primary transition-colors truncate">{project.name}</p>
                  <p className="text-xs text-muted-foreground font-mono truncate">{project.repo}</p>
                </div>
              </Link>
            ))}
          </div>
        )}
      </section>

      {/* Roles scoped to this environment */}
      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <KeyRound size={14} className="text-muted-foreground" />
          <h3 className="text-xs font-semibold text-foreground">Scoped Roles</h3>
          <span className="text-[11px] text-muted-foreground opacity-50">{scopedRoles.length}</span>
        </div>

        {scopedRoles.length === 0 ? (
          <p className="text-xs text-muted-foreground opacity-50 pl-5">No roles specifically scoped to this environment. Unscoped roles apply everywhere.</p>
        ) : (
          <div className="space-y-1.5">
            {scopedRoles.map((role) => (
              <div key={role.id} className="island-shell p-3 flex items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium text-foreground">{role.name}</span>
                  {role.isSystem && (
                    <span className="island-kicker !text-[11px] bg-accent text-primary border-primary">System</span>
                  )}
                </div>
                <ScopeBadges workspaces={role.workspaces} environments={role.environments} />
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Variable row (read-only with reveal for secrets)
// ---------------------------------------------------------------------------

function EnvVarRow({ name, description, isSecret, value }: {
  name: string; description?: string; isSecret: boolean; value?: string
}) {
  const [revealed, setRevealed] = useState(false)

  return (
    <div className="flex items-center gap-3 px-4 py-2.5">
      {isSecret
        ? <Lock size={12} className="text-warning shrink-0" />
        : <Variable size={12} className="text-muted-foreground shrink-0" />}

      <div className="w-[160px] shrink-0 min-w-0">
        <span className="text-xs font-mono font-medium text-foreground truncate block">{name}</span>
        {description && <p className="text-[11px] text-muted-foreground truncate">{description}</p>}
      </div>

      {value ? (
        <div className="flex items-center gap-1.5 flex-1 min-w-0">
          <span className="text-xs font-mono text-foreground truncate">
            {isSecret && !revealed ? '••••••••' : value}
          </span>
          {isSecret && (
            <button
              type="button"
              onClick={() => setRevealed(!revealed)}
              className="w-5 h-5 flex items-center justify-center rounded text-muted-foreground hover:text-foreground transition-colors shrink-0"
            >
              {revealed ? <EyeOff size={10} /> : <Eye size={10} />}
            </button>
          )}
        </div>
      ) : (
        <span className="flex items-center gap-1 text-xs text-destructive/60">
          <AlertTriangle size={9} />
          not set
        </span>
      )}
    </div>
  )
}
