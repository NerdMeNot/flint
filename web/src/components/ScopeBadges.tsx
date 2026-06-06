import { Boxes, ShieldCheck } from 'lucide-react'

interface ScopeBadgesProps {
  workspaces: string[]
  environments: string[]
}

export function ScopeBadges({ workspaces, environments }: ScopeBadgesProps) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {workspaces.length === 0 ? (
        <span className="inline-flex items-center gap-1 rounded-md border border-border px-1.5 py-0.5 text-[11px] text-muted-foreground opacity-60">
          <Boxes size={9} />
          All workspaces
        </span>
      ) : (
        workspaces.map((ws) => (
          <span
            key={ws}
            className="inline-flex items-center gap-1 rounded-md border border-primary/20 bg-primary/5 px-1.5 py-0.5 text-[11px] font-medium text-primary"
          >
            <Boxes size={9} />
            {ws}
          </span>
        ))
      )}
      {environments.length === 0 ? (
        <span className="inline-flex items-center gap-1 rounded-md border border-border px-1.5 py-0.5 text-[11px] text-muted-foreground opacity-60">
          <ShieldCheck size={9} />
          All environments
        </span>
      ) : (
        environments.map((env) => (
          <span
            key={env}
            className="inline-flex items-center gap-1 rounded-md border border-emerald-500/20 bg-emerald-500/5 px-1.5 py-0.5 text-[11px] font-medium text-emerald-400"
          >
            <ShieldCheck size={9} />
            {env}
          </span>
        ))
      )}
    </div>
  )
}
