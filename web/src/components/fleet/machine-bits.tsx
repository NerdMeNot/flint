import { Badge } from '#/components/Badge'

// machineStatusVariant maps a machine lifecycle status to the admin Badge
// palette: green = doing work or ready, amber = in transition, red = trouble.
export function machineStatusVariant(status: string): 'neutral' | 'primary' | 'success' | 'warning' | 'danger' {
  switch (status) {
    case 'idle': return 'success'
    case 'busy': return 'primary'
    case 'requested':
    case 'provisioning':
    case 'draining':
    case 'terminating': return 'warning'
    case 'failed':
    case 'lost': return 'danger'
    default: return 'neutral'
  }
}

export function MachineStatusBadge({ status }: { status: string }) {
  return <Badge variant={machineStatusVariant(status)}>{status}</Badge>
}

export function fmtCpuMem(cpuMillis: number, memoryMb: number): string {
  const cores = cpuMillis > 0 ? `${cpuMillis / 1000} vCPU` : '? vCPU'
  const mem = memoryMb > 0 ? `${Math.round(memoryMb / 1024)} GB` : '? GB'
  return `${cores} · ${mem}`
}

export function fmtAgo(iso: string): string {
  const secs = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000)
  if (secs < 60) return `${Math.round(secs)}s ago`
  if (secs < 3600) return `${Math.round(secs / 60)}m ago`
  if (secs < 86400) return `${Math.round(secs / 3600)}h ago`
  return `${Math.round(secs / 86400)}d ago`
}

export function fmtDuration(fromIso: string, toIso?: string): string {
  const from = new Date(fromIso).getTime()
  const to = toIso ? new Date(toIso).getTime() : Date.now()
  const secs = Math.max(0, (to - from) / 1000)
  if (secs < 90) return `${Math.round(secs)}s`
  if (secs < 5400) return `${Math.round(secs / 60)}m`
  if (secs < 129600) return `${(secs / 3600).toFixed(1)}h`
  return `${(secs / 86400).toFixed(1)}d`
}
