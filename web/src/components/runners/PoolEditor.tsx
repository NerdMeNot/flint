import { Link, useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import {
  ArrowLeft, Check, Zap, Copy, Plus, X, Trash2, Star,
  KeyRound, Scale, Timer, DollarSign,
} from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { Modal } from '#/components/Modal'
import { FormSelect } from '#/components/FormSelect'
import { ComboBox, type ComboOption } from '#/components/ComboBox'
import { Badge } from '#/components/Badge'
import type { RunnerPool, PolicyOverride } from '#/lib/api/types'

const inputClass =
  'w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40 disabled:opacity-60'

type CapacityType = 'spot' | 'on_demand' | 'any'
type Objective = 'cost' | 'latency' | 'balanced'

const CAPACITY_OPTIONS: { key: CapacityType; label: string; hint: string }[] = [
  { key: 'on_demand', label: 'On-demand', hint: 'never interrupted' },
  { key: 'spot', label: 'Spot', hint: 'cheapest, may be reclaimed' },
  { key: 'any', label: 'Any', hint: 'fleet picks per offer' },
]

const OBJECTIVE_OPTIONS: { key: Objective; label: string; hint: string }[] = [
  { key: 'balanced', label: 'Balanced', hint: 'price × boot time' },
  { key: 'cost', label: 'Cost', hint: 'cheapest $/hr wins' },
  { key: 'latency', label: 'Latency', hint: 'fastest boot wins' },
]

const GPU_VENDOR_PRESETS: ComboOption[] = [
  { value: 'nvidia' }, { value: 'amd' }, { value: 'intel' },
]
const GPU_MODEL_PRESETS: ComboOption[] = [
  { value: 'a100' }, { value: 'h100' }, { value: 'l4' }, { value: 'l40s' },
  { value: 't4' }, { value: 'a10g' },
]

// PoolEditor is the full-page create/edit surface for a machine pool: the
// machine shape it provides, the compute provider it draws from, and the
// economics policy the fleet optimizes within. Edit mode adds the join-token
// mint (how static machines enroll) and delete.
export function PoolEditor({ pool }: { pool?: RunnerPool }) {
  const navigate = useNavigate()
  const editing = !!pool

  const [name, setName] = useState(pool?.name ?? '')
  const [description, setDescription] = useState(pool?.description ?? '')
  const [provider, setProvider] = useState(pool?.provider ?? 'static')
  const [cpu, setCpu] = useState(pool?.cpu ?? '')
  // Memory is edited in GB (a plain number) and stored as Gi, so authors never
  // touch Mi/Gi suffixes.
  const [memory, setMemory] = useState(memToGB(pool?.memory ?? ''))
  const [disk, setDisk] = useState(diskToGB(pool?.disk ?? ''))
  const [arch, setArch] = useState(pool?.arch ?? 'amd64')
  const [hasGpu, setHasGpu] = useState(!!pool?.gpuVendor)
  const [gpuVendor, setGpuVendor] = useState(pool?.gpuVendor ?? 'nvidia')
  const [gpuModel, setGpuModel] = useState(pool?.gpuModel ?? '')
  const [gpuCount, setGpuCount] = useState(String(pool?.gpuCount ?? 1))

  // Economics policy.
  const [capacityType, setCapacityType] = useState<CapacityType>((pool?.capacityType as CapacityType) ?? 'on_demand')
  const [objective, setObjective] = useState<Objective>((pool?.objective as Objective) ?? 'balanced')
  const [minWarm, setMinWarm] = useState(String(pool?.minWarm ?? 0))
  const [maxMachines, setMaxMachines] = useState(String(pool?.maxMachines ?? 10))
  const [idleTtlMin, setIdleTtlMin] = useState(String(Math.round((pool?.idleTtlSeconds ?? 900) / 60)))
  const [hourlyCost, setHourlyCost] = useState(pool?.hourlyCost != null ? String(pool.hourlyCost) : '')
  const [overrides, setOverrides] = useState<PolicyOverride[]>(pool?.overrides ?? [])

  // Elastic allow-lists.
  const [instanceTypes, setInstanceTypes] = useState((pool?.instanceTypes ?? []).join(', '))
  const [regions, setRegions] = useState((pool?.regions ?? []).join(', '))

  const [showDelete, setShowDelete] = useState(false)
  const [mintedToken, setMintedToken] = useState<string | null>(null)

  const { data: providersData } = useQuery(orpc.computeProviders.list.queryOptions({}))
  const providerOptions = providersData?.providers ?? []
  const selectedProvider = providerOptions.find((p) => p.name === provider)
  const isStatic = (selectedProvider?.type ?? (provider === 'static' ? 'static' : '')) === 'static'

  const save = useAction(
    (data: Parameters<typeof client.runners.create>[0]) =>
      editing ? client.runners.update(data) : client.runners.create(data),
    { invalidate: [orpc.runners.list.key()], onSuccess: () => navigate({ to: '/settings/runners' }) },
  )
  const remove = useAction(() => client.runners.delete({ name }), {
    invalidate: [orpc.runners.list.key()],
    onSuccess: () => navigate({ to: '/settings/runners' }),
  })
  const setDefault = useAction(() => client.runners.setDefault({ name }), {
    invalidate: [orpc.runners.list.key()],
  })
  const mint = useAction(() => client.runners.mintToken({ name }), {
    onSuccess: (r) => setMintedToken(r.token),
  })

  const submit = () => {
    save.mutate({
      name: name.trim(),
      description: description.trim() || undefined,
      provider,
      cpu: cpu.trim() || undefined,
      memory: memory.trim() ? `${memory.trim()}Gi` : undefined,
      disk: disk.trim() ? `${disk.trim()}Gi` : undefined,
      arch,
      gpu: hasGpu ? { vendor: gpuVendor, model: gpuModel || undefined, count: Number(gpuCount) || 1 } : undefined,
      instanceTypes: splitList(instanceTypes),
      regions: splitList(regions),
      capacityType,
      objective,
      minWarm: Number(minWarm) || 0,
      maxMachines: Number(maxMachines) || 0,
      idleTtlSeconds: (Number(idleTtlMin) || 0) * 60,
      overrides: overrides.length ? overrides : undefined,
      hourlyCost: hourlyCost.trim() ? Number(hourlyCost) : undefined,
    })
  }

  const warmMonthly = warmCostPerMonth(Number(minWarm) || 0, hourlyCost, pool)

  return (
    <div className="space-y-6 max-w-3xl">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="flex items-center gap-3">
          <Link to="/settings/runners" className="text-muted-foreground hover:text-foreground transition-colors">
            <ArrowLeft size={18} />
          </Link>
          <div>
            <h2 className="text-lg font-semibold text-foreground flex items-center gap-2">
              {editing ? <span className="font-mono">{pool.name}</span> : 'New pool'}
              {pool?.isDefault && <Badge variant="primary">Default</Badge>}
            </h2>
            <p className="text-xs text-muted-foreground">
              {editing ? 'Edit machine pool' : 'A pool of machines your pipelines target with runner:'}
            </p>
          </div>
        </div>
        {editing && !pool.isDefault && (
          <button
            onClick={() => setDefault.mutate(undefined)}
            className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-accent transition-colors"
          >
            <Star size={13} /> Make default
          </button>
        )}
      </div>

      {/* ── Identity ── */}
      <Section title="Identity">
        <Field label="Name" hint="Pipelines reference this with runner: <name>">
          <input
            className={`${inputClass} font-mono`}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="standard"
            disabled={editing}
          />
        </Field>
        <Field label="Description" hint="Optional">
          <input className={inputClass} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="General-purpose CI machines" />
        </Field>
        <Field label="Provider" hint="Where machines come from — static is bring-your-own">
          <FormSelect
            value={provider}
            onChange={setProvider}
            options={
              providerOptions.length
                ? providerOptions.map((p) => ({ key: p.name, label: `${p.name} (${p.type})` }))
                : [{ key: 'static', label: 'static (bring your own machines)' }]
            }
          />
        </Field>
      </Section>

      {/* ── Machine shape ── */}
      <Section title="Machine shape" caption="What one machine looks like — sizes provider offers and static capacity defaults">
        <div className="grid grid-cols-2 gap-3">
          <Field label="vCPU">
            <input className={inputClass} value={cpu} onChange={(e) => setCpu(e.target.value)} placeholder="4" inputMode="numeric" />
          </Field>
          <Field label="Memory (GB)">
            <input className={inputClass} value={memory} onChange={(e) => setMemory(e.target.value)} placeholder="8" inputMode="numeric" />
          </Field>
          <Field label="Disk (GB)">
            <input className={inputClass} value={disk} onChange={(e) => setDisk(e.target.value)} placeholder="50" inputMode="numeric" />
          </Field>
          <Field label="Architecture">
            <FormSelect value={arch} onChange={setArch} options={[{ key: 'amd64', label: 'amd64 (x86_64)' }, { key: 'arm64', label: 'arm64' }]} />
          </Field>
        </div>

        <label className="flex items-center gap-2 text-sm text-foreground cursor-pointer select-none">
          <input type="checkbox" checked={hasGpu} onChange={(e) => setHasGpu(e.target.checked)} className="accent-[var(--ring)]" />
          GPU machines
        </label>
        {hasGpu && (
          <div className="grid grid-cols-3 gap-3">
            <Field label="Vendor"><ComboBox value={gpuVendor} onChange={setGpuVendor} options={GPU_VENDOR_PRESETS} /></Field>
            <Field label="Model"><ComboBox value={gpuModel} onChange={setGpuModel} options={GPU_MODEL_PRESETS} placeholder="any" /></Field>
            <Field label="Count"><input className={inputClass} value={gpuCount} onChange={(e) => setGpuCount(e.target.value)} inputMode="numeric" /></Field>
          </div>
        )}

        {!isStatic && (
          <div className="grid grid-cols-2 gap-3">
            <Field label="Instance types" hint="Comma-separated allow-list; empty = provider picks">
              <input className={`${inputClass} font-mono`} value={instanceTypes} onChange={(e) => setInstanceTypes(e.target.value)} placeholder="c7g.xlarge, m7g.xlarge" />
            </Field>
            <Field label="Regions" hint="Comma-separated; empty = provider default">
              <input className={`${inputClass} font-mono`} value={regions} onChange={(e) => setRegions(e.target.value)} placeholder="us-east-1" />
            </Field>
          </div>
        )}
      </Section>

      {/* ── Economics policy ── */}
      <Section
        title="Economics"
        caption="You state the tradeoff; the fleet optimizes within it and records every decision"
      >
        <div className="grid grid-cols-2 gap-3">
          <Field label="Capacity" icon={<Zap size={12} />}>
            <FormSelect
              value={capacityType}
              onChange={(v) => setCapacityType(v as CapacityType)}
              options={CAPACITY_OPTIONS.map((o) => ({ key: o.key, label: `${o.label} — ${o.hint}` }))}
            />
          </Field>
          <Field label="Objective" icon={<Scale size={12} />}>
            <FormSelect
              value={objective}
              onChange={(v) => setObjective(v as Objective)}
              options={OBJECTIVE_OPTIONS.map((o) => ({ key: o.key, label: `${o.label} — ${o.hint}` }))}
            />
          </Field>
          <Field label="Warm minimum" hint="Machines kept running while idle. 0 = zero standing infra.">
            <input className={inputClass} value={minWarm} onChange={(e) => setMinWarm(e.target.value)} inputMode="numeric" />
          </Field>
          <Field label="Max machines" hint="Hard ceiling on the pool">
            <input className={inputClass} value={maxMachines} onChange={(e) => setMaxMachines(e.target.value)} inputMode="numeric" />
          </Field>
          <Field label="Idle timeout (minutes)" icon={<Timer size={12} />} hint="Idle machines above the warm minimum are destroyed after this">
            <input className={inputClass} value={idleTtlMin} onChange={(e) => setIdleTtlMin(e.target.value)} inputMode="numeric" />
          </Field>
          {isStatic && (
            <Field label="Hourly cost (USD)" icon={<DollarSign size={12} />} hint="Optional amortized cost so static machines feed the same economics">
              <input className={inputClass} value={hourlyCost} onChange={(e) => setHourlyCost(e.target.value)} placeholder="0.20" inputMode="decimal" />
            </Field>
          )}
        </div>

        {/* The tradeoff, in dollars: warm machines cost money; zero warm means
            the first run after idle waits for a boot. */}
        <div className="rounded-lg border border-border bg-muted/30 px-3 py-2.5 text-xs text-muted-foreground leading-relaxed">
          {Number(minWarm) > 0 ? (
            <>
              Keeping <span className="text-foreground font-medium">{minWarm}</span> machine{Number(minWarm) === 1 ? '' : 's'} warm
              {warmMonthly != null && <> costs about <span className="text-foreground font-medium">${warmMonthly.toFixed(0)}/month</span></>}
              {' '}— runs skip the boot wait while one is free.
            </>
          ) : (
            <>Zero standing infrastructure: nothing runs (or costs) while idle; the first run after a quiet period waits for a machine to boot.</>
          )}
        </div>

        <OverridesEditor overrides={overrides} onChange={setOverrides} />
      </Section>

      {/* ── Observed economics (edit mode) ── */}
      {editing && <InsightsSection poolName={pool.name} minWarm={Number(minWarm) || 0} />}

      {/* ── Join token (static pools, edit mode) ── */}
      {editing && isStatic && (
        <Section title="Agent enrollment" caption="Machines join this pool with a join token — mint one, then run flint-agent with it">
          {mintedToken ? (
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <code className="flex-1 rounded-lg border border-border bg-muted/40 px-3 py-2 text-xs font-mono break-all select-all">{mintedToken}</code>
                <button
                  onClick={() => navigator.clipboard.writeText(mintedToken)}
                  className="rounded-lg border border-border p-2 text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
                  title="Copy token"
                >
                  <Copy size={14} />
                </button>
              </div>
              <p className="text-[11px] text-muted-foreground">
                Shown once. Join a machine with:{' '}
                <code className="font-mono text-foreground">flint-agent daemon --server &lt;host:9443&gt; --token &lt;token&gt;</code>
              </p>
            </div>
          ) : (
            <button
              onClick={() => mint.mutate(undefined)}
              disabled={mint.isPending}
              className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-accent transition-colors"
            >
              <KeyRound size={13} /> {mint.isPending ? 'Minting…' : 'Mint join token'}
            </button>
          )}
          <p className="text-[11px] text-muted-foreground">
            Rotating invalidates the previous token for new joins; already-registered machines keep their machine tokens.
          </p>
        </Section>
      )}

      {/* ── Actions ── */}
      <div className="flex items-center justify-between gap-3">
        <div>
          {editing && (
            <button
              onClick={() => setShowDelete(true)}
              className="flex items-center gap-1.5 rounded-lg border border-danger/40 px-3 py-1.5 text-xs font-medium text-danger hover:bg-danger/10 transition-colors"
            >
              <Trash2 size={13} /> Delete pool
            </button>
          )}
        </div>
        <div className="flex items-center gap-2">
          {save.isError && <span className="text-xs text-danger">{(save.error as Error)?.message ?? 'Save failed'}</span>}
          <button
            onClick={submit}
            disabled={!name.trim() || save.isPending}
            className="flex items-center gap-1.5 rounded-lg px-4 py-2 text-sm font-medium text-white transition-colors disabled:opacity-50"
            style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
          >
            <Check size={14} /> {save.isPending ? 'Saving…' : editing ? 'Save changes' : 'Create pool'}
          </button>
        </div>
      </div>

      <Modal open={showDelete} onClose={() => setShowDelete(false)} title="Delete pool?">
        <p className="text-sm text-muted-foreground">
          Deletes <span className="font-mono text-foreground">{name}</span>. Machines already in the pool keep running
          until drained; pipelines targeting it will fail to resolve.
        </p>
        <div className="mt-4 flex justify-end gap-2">
          <button onClick={() => setShowDelete(false)} className="rounded-lg border border-border px-3 py-1.5 text-xs font-medium hover:bg-accent transition-colors">
            Cancel
          </button>
          <button
            onClick={() => remove.mutate(undefined)}
            className="rounded-lg bg-danger px-3 py-1.5 text-xs font-medium text-white hover:opacity-90 transition-colors"
          >
            Delete
          </button>
        </div>
      </Modal>
    </div>
  )
}

// OverridesEditor edits per-branch/event policy overrides: "main keeps 1 warm",
// "PRs run on spot". Platform owners own economics; repo authors just pick a pool.
function OverridesEditor({ overrides, onChange }: {
  overrides: PolicyOverride[]
  onChange: (o: PolicyOverride[]) => void
}) {
  const update = (i: number, patch: Partial<PolicyOverride>) => {
    onChange(overrides.map((o, j) => (j === i ? { ...o, ...patch } : o)))
  }
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-muted-foreground">Per-branch / event overrides</span>
        <button
          onClick={() => onChange([...overrides, { match: {}, set: {} }])}
          className="flex items-center gap-1 rounded-md border border-border px-2 py-1 text-[11px] font-medium text-foreground hover:bg-accent transition-colors"
        >
          <Plus size={11} /> Add override
        </button>
      </div>
      {overrides.map((o, i) => (
        <div key={i} className="rounded-lg border border-border p-3 space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-[11px] text-muted-foreground uppercase tracking-wider">When</span>
            <button onClick={() => onChange(overrides.filter((_, j) => j !== i))} className="text-muted-foreground hover:text-danger transition-colors">
              <X size={13} />
            </button>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <input
              className={`${inputClass} font-mono !py-1.5 !text-xs`}
              value={o.match.branch ?? ''}
              onChange={(e) => update(i, { match: { ...o.match, branch: e.target.value || undefined } })}
              placeholder="branch (e.g. main)"
            />
            <input
              className={`${inputClass} font-mono !py-1.5 !text-xs`}
              value={o.match.event ?? ''}
              onChange={(e) => update(i, { match: { ...o.match, event: e.target.value || undefined } })}
              placeholder="event (e.g. pull_request)"
            />
          </div>
          <span className="text-[11px] text-muted-foreground uppercase tracking-wider">Set</span>
          <div className="grid grid-cols-2 gap-2">
            <input
              className={`${inputClass} !py-1.5 !text-xs`}
              value={o.set.minWarm != null ? String(o.set.minWarm) : ''}
              onChange={(e) => update(i, { set: { ...o.set, minWarm: e.target.value === '' ? undefined : Number(e.target.value) } })}
              placeholder="minWarm"
              inputMode="numeric"
            />
            <select
              className={`${inputClass} !py-1.5 !text-xs`}
              value={o.set.capacityType ?? ''}
              onChange={(e) => update(i, { set: { ...o.set, capacityType: e.target.value || undefined } })}
            >
              <option value="">capacity (unchanged)</option>
              <option value="spot">spot</option>
              <option value="on_demand">on_demand</option>
              <option value="any">any</option>
            </select>
          </div>
        </div>
      ))}
    </div>
  )
}

// InsightsSection shows the pool's last-7-days observed economics and the
// minWarm=1 what-if — real history, not simulation, so the operator can see
// exactly what their policy is buying (or costing) before changing it.
function InsightsSection({ poolName, minWarm }: { poolName: string; minWarm: number }) {
  const { data } = useQuery(orpc.runners.insights.queryOptions({ input: { name: poolName } }))
  if (!data || (data.assignments.total === 0 && data.machineHours === 0)) return null

  const a = data.assignments
  return (
    <Section title="Last 7 days" caption="Observed from this pool's machines and assignments">
      <div className="grid grid-cols-2 sm:grid-cols-3 gap-4">
        <Insight label="Spend" value={`$${data.spendUsd.toFixed(2)}`} />
        <Insight label="Machine hours" value={data.machineHours.toFixed(1)} />
        <Insight label="Warm-hit rate" value={a.total > 0 ? `${Math.round(a.warmHitRate * 100)}%` : '—'} />
        <Insight label="Queue p50 / p95" value={`${fmtSecs(a.queueP50Secs)} / ${fmtSecs(a.queueP95Secs)}`} />
        <Insight label="Boot p50" value={data.boots > 0 ? fmtSecs(data.bootP50Secs) : '—'} />
        <Insight label="Spot interruptions" value={String(data.interruptions)} />
      </div>
      {data.whatIf && minWarm === 0 && (
        <div className="rounded-lg border border-border bg-muted/30 px-3 py-2.5 text-xs text-muted-foreground leading-relaxed">
          What if <span className="text-foreground font-medium">minWarm: 1</span>? A standing machine would cost about{' '}
          <span className="text-foreground font-medium">${data.whatIf.minWarmOne.costPerMonthUsd.toFixed(0)}/month</span>; observed
          cold runs waited <span className="text-foreground font-medium">{fmtSecs(data.whatIf.minWarmOne.coldWaitP50Secs)}</span> vs{' '}
          <span className="text-foreground font-medium">{fmtSecs(data.whatIf.minWarmOne.warmWaitP50Secs)}</span> warm.
        </div>
      )}
    </Section>
  )
}

function Insight({ label, value }: { label: string; value: string }) {
  return (
    <div className="space-y-0.5">
      <p className="text-[11px] text-muted-foreground uppercase tracking-wider">{label}</p>
      <p className="text-sm font-semibold text-foreground tabular-nums">{value}</p>
    </div>
  )
}

function fmtSecs(s: number): string {
  if (s < 90) return `${Math.round(s)}s`
  return `${(s / 60).toFixed(1)}m`
}

function Section({ title, caption, children }: { title: string; caption?: string; children: React.ReactNode }) {
  return (
    <section className="island-shell p-4 sm:p-5 space-y-4">
      <div>
        <h3 className="text-sm font-semibold text-foreground">{title}</h3>
        {caption && <p className="text-xs text-muted-foreground mt-0.5">{caption}</p>}
      </div>
      {children}
    </section>
  )
}

function Field({ label, hint, icon, children }: {
  label: string
  hint?: string
  icon?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <label className="block space-y-1.5">
      <span className="flex items-center gap-1 text-xs font-medium text-muted-foreground">{icon}{label}</span>
      {children}
      {hint && <span className="block text-[11px] text-muted-foreground/70">{hint}</span>}
    </label>
  )
}

function splitList(s: string): string[] | undefined {
  const items = s.split(',').map((x) => x.trim()).filter(Boolean)
  return items.length ? items : undefined
}

// memToGB converts a stored quantity ("8Gi", "8192Mi") to a plain GB string for editing.
function memToGB(q: string): string {
  if (!q) return ''
  if (q.endsWith('Gi')) return q.slice(0, -2)
  if (q.endsWith('Mi')) return String(Number(q.slice(0, -2)) / 1024)
  return q
}

function diskToGB(q: string): string {
  if (!q) return ''
  if (q.endsWith('Gi') || q.endsWith('GB')) return q.slice(0, -2)
  if (q.endsWith('G')) return q.slice(0, -1)
  return q
}

// warmCostPerMonth estimates the monthly cost of the warm floor from the best
// price signal available: declared hourlyCost for static pools.
function warmCostPerMonth(minWarm: number, hourlyCost: string, pool?: RunnerPool): number | null {
  if (minWarm <= 0) return null
  const rate = hourlyCost.trim() ? Number(hourlyCost) : pool?.hourlyCost
  if (!rate || Number.isNaN(rate)) return null
  return minWarm * rate * 730
}
