import { createFileRoute } from '@tanstack/react-router'
import { Gauge, Network, Activity, Target } from 'lucide-react'

export const Route = createFileRoute('/loadtest')({
  component: LoadTestPage,
})

const planned = [
  { icon: Network, title: 'Distributed workers', body: 'Coordinated load generators on your runner pools, scaled to the target RPS.' },
  { icon: Activity, title: 'Live percentiles', body: 'Streaming p50 / p95 / p99 latency and throughput as the test runs.' },
  { icon: Target, title: 'Thresholds & SLOs', body: 'Fail a run when latency or error-rate budgets are breached — gate releases on it.' },
]

function LoadTestPage() {
  return (
    <div className="rise-in mx-auto max-w-3xl py-10 lg:py-16">
      <div className="text-center">
        <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-2xl"
          style={{ background: 'color-mix(in oklab, var(--primary) 14%, transparent)', color: 'var(--primary)' }}>
          <Gauge size={30} strokeWidth={1.6} />
        </div>
        <p className="island-kicker mt-5 !tracking-[0.18em]">Coming soon</p>
        <h1 className="display-title mt-2 text-3xl lg:text-4xl font-bold tracking-tight text-foreground">Load Testing</h1>
        <p className="mx-auto mt-3 max-w-md text-sm leading-relaxed text-muted-foreground">
          Distributed HTTP &amp; gRPC load testing on the Flint platform — running on the same engine,
          identity, RBAC, and runner pools as your pipelines and workflows.
        </p>
      </div>

      <div className="mt-10 grid gap-3 sm:grid-cols-3">
        {planned.map((f) => (
          <div key={f.title} className="feature-card p-4 text-left">
            <div className="flex h-9 w-9 items-center justify-center rounded-lg"
              style={{ background: 'color-mix(in oklab, var(--primary) 12%, transparent)', color: 'var(--primary)' }}>
              <f.icon size={17} strokeWidth={1.9} />
            </div>
            <p className="mt-3 text-sm font-semibold text-foreground">{f.title}</p>
            <p className="mt-1 text-[13px] leading-relaxed text-muted-foreground">{f.body}</p>
          </div>
        ))}
      </div>

      <p className="mt-8 text-center text-xs text-muted-foreground/60">
        Enable it per-deployment once it ships — <code className="rounded bg-muted px-1.5 py-0.5">products.loadtest.enabled</code>
      </p>
    </div>
  )
}
