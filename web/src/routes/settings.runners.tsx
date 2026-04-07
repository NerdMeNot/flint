import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Server, Cpu, HardDrive, Microchip } from 'lucide-react'
import { orpc } from '#/lib/orpc'

export const Route = createFileRoute('/settings/runners')({
  component: RunnersPage,
})

function RunnersPage() {
  const { data: runnersData } = useSuspenseQuery(orpc.runners.list.queryOptions({ input: {} }))
  const runners = runnersData.items

  return (
    <div className="space-y-5">
      <p className="text-muted-foreground text-sm">
        {runners.length} runner {runners.length === 1 ? 'pool' : 'pools'} configured
      </p>

      {runners.length === 0 ? (
        <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
          <Server size={32} strokeWidth={1.2} />
          <span className="text-sm">No runner pools configured yet.</span>
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {runners.map((runner, i) => (
            <div
              key={runner.id}
              className="feature-card rise-in p-5 space-y-3"
              style={{ animationDelay: `${i * 50 + 30}ms` }}
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <Server size={14} className="text-muted-foreground shrink-0" />
                    <h3 className="font-semibold text-sm text-foreground truncate">{runner.name}</h3>
                  </div>
                  {runner.description && (
                    <p className="text-xs text-muted-foreground mt-1 line-clamp-2">{runner.description}</p>
                  )}
                </div>
                <span
                  className={`island-kicker !text-[0.55rem] shrink-0 flex items-center gap-1 ${
                    runner.ready
                      ? 'bg-success/10 text-success border-success/20'
                      : 'bg-destructive/10 text-destructive border-destructive/20'
                  }`}
                >
                  <span
                    className={`w-1.5 h-1.5 rounded-full ${
                      runner.ready ? 'bg-success' : 'bg-destructive'
                    }`}
                  />
                  {runner.ready ? 'Ready' : 'Not Ready'}
                </span>
              </div>

              <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                <span className="flex items-center gap-1">
                  <Cpu size={12} />
                  {runner.cpu} CPU
                </span>
                <span className="flex items-center gap-1">
                  <HardDrive size={12} />
                  {runner.memory}
                </span>
                <span className="font-mono text-[0.65rem] bg-muted px-1.5 py-0.5 rounded">
                  {runner.arch}
                </span>
              </div>

              {runner.gpuVendor && (
                <div className="flex items-center gap-1.5 text-xs text-muted-foreground border-t border-border pt-3">
                  <Microchip size={12} />
                  <span>
                    {runner.gpuCount && runner.gpuCount > 1 ? `${runner.gpuCount}x ` : ''}
                    {runner.gpuVendor}
                    {runner.gpuModel ? ` ${runner.gpuModel}` : ''}
                  </span>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
