import { createFileRoute } from '@tanstack/react-router'
import { Gauge } from 'lucide-react'

export const Route = createFileRoute('/loadtest')({
  component: LoadTestPage,
})

function LoadTestPage() {
  return (
    <div className="flex min-h-[60vh] items-center justify-center">
      <div className="max-w-md text-center">
        <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-2xl"
          style={{ background: 'color-mix(in oklab, var(--primary) 14%, transparent)', color: 'var(--primary)' }}>
          <Gauge size={30} strokeWidth={1.6} />
        </div>
        <p className="island-kicker mt-5 !tracking-[0.18em]">Coming soon</p>
        <h1 className="display-title mt-2 text-3xl font-bold tracking-tight text-foreground">Load Testing</h1>
        <p className="mx-auto mt-3 text-sm leading-relaxed text-muted-foreground">
          Distributed HTTP &amp; gRPC load testing on the Flint platform — coordinated workers,
          live percentiles, and thresholds. It shares your identity, RBAC, and runner pools.
        </p>
      </div>
    </div>
  )
}
