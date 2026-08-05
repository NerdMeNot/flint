import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { ShieldCheck } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { formatTime } from '#/lib/format-time'

export const Route = createFileRoute('/settings/environments/')({
  component: EnvironmentsPage,
})

function EnvironmentsPage() {
  const { data: envData } = useSuspenseQuery(orpc.environments.list.queryOptions({ input: {} }))
  const environments = envData.items
  const { data: varsData } = useSuspenseQuery(orpc.envVariables.list.queryOptions({ input: {} }))
  const variables = varsData.items
  const { data: valuesData } = useSuspenseQuery(orpc.envVariables.values.queryOptions({ input: {} }))
  const values = valuesData.items

  const envVarCount = variables.filter((v) => v.scope === 'environment').length

  const valueCounts = new Map<string, number>()
  for (const v of values) {
    if (v.environmentId) {
      valueCounts.set(v.environmentId, (valueCounts.get(v.environmentId) ?? 0) + 1)
    }
  }

  return (
    <div className="space-y-5">
      <div>
        <h2 className="display-title text-lg text-foreground">Environments</h2>
        <p className="text-muted-foreground text-xs mt-0.5">
          {environments.length} {environments.length === 1 ? 'environment' : 'environments'}
        </p>
      </div>

      {environments.length === 0 ? (
        <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
          <ShieldCheck size={32} strokeWidth={1.2} />
          <span className="text-sm">No environments configured yet.</span>
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {environments.map((env, i) => {
            const varCount = valueCounts.get(env.id) ?? 0
            return (
              <Link
                key={env.id}
                to="/settings/environments/$id"
                params={{ id: env.id }}
                className="feature-card rise-in p-5 space-y-3 flex flex-col hover:bg-accent transition-colors group"
                style={{ animationDelay: `${i * 50 + 30}ms` }}
              >
                <div className="flex items-start justify-between">
                  <div>
                    <h3 className="font-semibold text-sm text-foreground group-hover:text-primary transition-colors truncate">{env.name}</h3>
                    <p className="text-xs text-muted-foreground font-mono truncate">{env.slug}</p>
                  </div>
                </div>

                <div className="flex items-center gap-3 text-xs text-muted-foreground mt-auto">
                  <span>{varCount}/{envVarCount} variables set</span>
                  <span className="opacity-50">Created {formatTime(env.createdAt)}</span>
                </div>
              </Link>
            )
          })}
        </div>
      )}
    </div>
  )
}
