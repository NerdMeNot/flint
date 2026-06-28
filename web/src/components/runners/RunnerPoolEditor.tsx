import { Link, useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import {
  ArrowLeft, Server, Boxes, Cpu, HardDrive, Microchip, Check, Cloud, Zap,
  FileCode, Copy, Download, ChevronDown, ChevronRight, Tag, Ban, Plus, X, Trash2, AlertTriangle, Star,
} from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { Modal } from '#/components/Modal'
import { FormSelect } from '#/components/FormSelect'
import { ComboBox, PillSelect, type ComboOption } from '#/components/ComboBox'
import { Badge } from '#/components/Badge'
import type { RunnerPool } from '#/lib/api/types'

const inputClass =
  'w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40 disabled:opacity-60'

const ARCH_KEY = 'kubernetes.io/arch'

type CapacityType = 'spot-preferred' | 'spot' | 'on-demand'
type KV = { key: string; value: string }
type Tol = { key: string; operator: 'Equal' | 'Exists'; value: string; effect: 'NoSchedule' | 'PreferNoSchedule' | 'NoExecute' }

const CAPACITY_OPTIONS: { key: CapacityType; label: string }[] = [
  { key: 'spot-preferred', label: 'Spot, fallback on-demand' },
  { key: 'spot', label: 'Spot only' },
  { key: 'on-demand', label: 'On-demand only' },
]

// ── Preset suggestions (you can always type your own) ──
const FAMILY_PRESETS: ComboOption[] = [
  { value: 'c', hint: 'compute' }, { value: 'm', hint: 'general' }, { value: 'r', hint: 'memory' },
  { value: 'c7i' }, { value: 'c7g', hint: 'graviton' }, { value: 'c6i' },
  { value: 'm7i' }, { value: 'm7g', hint: 'graviton' }, { value: 'm6i' },
  { value: 'r7i' }, { value: 'r7g', hint: 'graviton' }, { value: 'r6i' },
  { value: 't3' }, { value: 't3a' },
  { value: 'g5', hint: 'gpu' }, { value: 'g6', hint: 'gpu' }, { value: 'p4d', hint: 'gpu' }, { value: 'p5', hint: 'gpu' },
]
const AMI_PRESETS: ComboOption[] = [
  { value: 'AL2023' }, { value: 'AL2' }, { value: 'Bottlerocket' }, { value: 'Ubuntu' },
  { value: 'Windows2022' }, { value: 'Windows2019' },
]
const GPU_VENDOR_PRESETS: ComboOption[] = [
  { value: 'nvidia' }, { value: 'amd' }, { value: 'intel' }, { value: 'aws', hint: 'neuron' },
]
const GPU_MODEL_PRESETS: ComboOption[] = [
  { value: 'a100' }, { value: 'h100' }, { value: 'h200' }, { value: 'l4' }, { value: 'l40s' },
  { value: 't4' }, { value: 'a10g' }, { value: 'v100' }, { value: 'mi300x', hint: 'amd' },
]
const CONSOLIDATE_PRESETS: ComboOption[] = [
  { value: '0s', hint: 'immediate' }, { value: '30s' }, { value: '1m' }, { value: '5m' }, { value: '10m' }, { value: '1h' },
]
const NODE_KEY_PRESETS: ComboOption[] = [
  { value: 'karpenter.sh/nodepool', hint: 'Karpenter' },
  { value: 'karpenter.sh/capacity-type', hint: 'spot/on-demand' },
  { value: 'node.kubernetes.io/instance-type' },
  { value: 'topology.kubernetes.io/zone', hint: 'AZ' },
  { value: 'eks.amazonaws.com/nodegroup', hint: 'EKS MNG' },
  { value: 'cloud.google.com/gke-nodepool', hint: 'GKE' },
  { value: 'agentpool', hint: 'AKS' },
  { value: 'kubernetes.io/arch', hint: 'arch' },
  { value: 'kubernetes.io/os' },
]

// RunnerPoolEditor is the full-page create/edit surface for a runner pool. Left
// column = builder, right column = live preview (and, for managed pools in edit
// mode, the rendered Karpenter manifests).
export function RunnerPoolEditor({ pool }: { pool?: RunnerPool }) {
  const navigate = useNavigate()
  const editing = !!pool

  const [name, setName] = useState(pool?.name ?? '')
  const [description, setDescription] = useState(pool?.description ?? '')
  const [cpu, setCpu] = useState(pool?.cpu ?? '')
  // Memory is edited in GB (a plain number) and stored as Gi, so authors never
  // touch Mi/Gi suffixes.
  const [memory, setMemory] = useState(memToGB(pool?.memory ?? ''))
  const [arch, setArch] = useState(pool?.arch ?? 'amd64')
  const [hasGpu, setHasGpu] = useState(!!pool?.gpuVendor)
  const [gpuVendor, setGpuVendor] = useState(pool?.gpuVendor ?? 'nvidia')
  const [gpuModel, setGpuModel] = useState(pool?.gpuModel ?? '')
  const [gpuCount, setGpuCount] = useState(String(pool?.gpuCount ?? 1))

  // Reference node targeting (arch key is derived from `arch`, so it's stripped here).
  const [nodeSelector, setNodeSelector] = useState<KV[]>(
    Object.entries(pool?.nodeSelector ?? {}).filter(([k]) => k !== ARCH_KEY).map(([key, value]) => ({ key, value })),
  )
  const [tolerations, setTolerations] = useState<Tol[]>(
    (pool?.tolerations ?? []).map((t) => ({
      key: t.key ?? '',
      operator: (t.operator as Tol['operator']) || 'Equal',
      value: t.value ?? '',
      effect: (t.effect as Tol['effect']) || 'NoSchedule',
    })),
  )

  const [managed, setManaged] = useState((pool?.mode ?? 'reference') === 'managed')
  // Managed pools need a configured provisioning profile (Karpenter/AWS). If the
  // platform has none, the managed option is unavailable. Default to available
  // while loading so the card doesn't flicker disabled.
  const { data: prov } = useQuery(orpc.runners.provisioning.queryOptions({}))
  const managedAvailable = prov ? prov.configured : true
  const m = pool?.managed
  const [capacityType, setCapacityType] = useState<CapacityType>((m?.capacityType as CapacityType) ?? 'spot-preferred')
  const [scaleToZero, setScaleToZero] = useState(m?.scaleToZero ?? true)
  const [consolidateAfter, setConsolidateAfter] = useState(m?.consolidateAfter ?? '30s')
  const [diskGiB, setDiskGiB] = useState(String(m?.diskGiB ?? 50))
  const [cpuLimit, setCpuLimit] = useState(m?.cpuLimit ? String(m.cpuLimit) : '')
  const [gpuLimit, setGpuLimit] = useState(m?.gpuLimit ? String(m.gpuLimit) : '')
  const [families, setFamilies] = useState<string[]>(m?.instanceFamilies ?? [])
  const [amiFamily, setAmiFamily] = useState(m?.amiFamily ?? '')

  const [showDelete, setShowDelete] = useState(false)

  const save = useAction(
    (data: Parameters<typeof client.runners.create>[0]) =>
      editing ? client.runners.update(data) : client.runners.create(data),
    { invalidate: [orpc.runners.list.key()], onSuccess: () => navigate({ to: '/settings/runners' }) },
  )

  const isDefault = pool?.isDefault ?? false
  // Promoting invalidates the list; the route refeeds an updated pool prop, so the
  // Default badge + delete-guard flip live without leaving the page.
  const setDefault = useAction(client.runners.setDefault, { invalidate: [orpc.runners.list.key()] })

  const valid = name.trim() !== ''

  function handleSave() {
    if (!valid) return
    // Reference pools target existing nodes via nodeSelector + tolerations exactly
    // as entered. Arch is a descriptor — it's only constrained at scheduling if the
    // author explicitly pinned kubernetes.io/arch. Managed pools derive their own.
    const ns: Record<string, string> = {}
    for (const { key, value } of nodeSelector) if (key.trim()) ns[key.trim()] = value.trim()
    const tols = tolerations.filter((t) => t.key.trim()).map((t) => ({
      key: t.key.trim(),
      operator: t.operator,
      value: t.operator === 'Exists' ? undefined : (t.value.trim() || undefined),
      effect: t.effect,
    }))

    save.mutate({
      name: name.trim(),
      description: description.trim() || undefined,
      cpu: cpu.trim() || undefined,
      memory: memory.trim() ? `${memory.trim()}Gi` : undefined,
      arch,
      gpu: hasGpu && gpuVendor.trim()
        ? { vendor: gpuVendor.trim(), model: gpuModel.trim() || undefined, count: Number(gpuCount) || 1 }
        : undefined,
      nodeSelector: managed ? undefined : ns,
      tolerations: managed ? undefined : (tols.length ? tols : undefined),
      mode: managed ? 'managed' : 'reference',
      managed: managed
        ? {
            capacityType,
            scaleToZero,
            consolidateAfter: scaleToZero ? consolidateAfter.trim() || undefined : undefined,
            diskGiB: diskGiB ? Number(diskGiB) : undefined,
            cpuLimit: cpuLimit ? Number(cpuLimit) : undefined,
            gpuLimit: hasGpu && gpuLimit ? Number(gpuLimit) : undefined,
            instanceFamilies: families.length ? families : undefined,
            amiFamily: amiFamily.trim() || undefined,
          }
        : undefined,
    })
  }

  const dupName = String((save.error as Error)?.message ?? '').includes('409')

  return (
    <div className="space-y-6 rise-in">
      {editing && showDelete && <DeletePoolModal name={pool!.name} managed={managed} onClose={() => setShowDelete(false)} />}
      {/* ── Header ── */}
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <Link to="/settings/runners" className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors mb-1.5">
            <ArrowLeft size={13} /> Runner pools
          </Link>
          <h2 className="display-title text-lg text-foreground flex items-center gap-2">
            {editing ? pool!.name : 'New runner pool'}
            <Badge variant={managed ? 'primary' : 'neutral'}>{managed ? 'Managed' : 'Reference'}</Badge>
            {isDefault && <Badge variant="primary">Default</Badge>}
          </h2>
          <p className="text-muted-foreground text-xs mt-0.5">
            {managed
              ? 'Flint renders a Karpenter NodePool for this pool — scale-to-zero, applied via your GitOps.'
              : 'A named pool pipelines target via runner:. Stamps selectors onto existing nodes.'}
          </p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <Link to="/settings/runners" className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
            Cancel
          </Link>
          <button
            type="button" onClick={handleSave}
            disabled={!valid || save.isPending}
            className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
            style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
          >
            {save.isPending ? 'Saving…' : editing ? 'Save changes' : 'Create pool'}
          </button>
        </div>
      </div>

      {dupName && (
        <div className="rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-2.5 text-xs text-destructive">
          A pool named <span className="font-mono">{name}</span> already exists. Pick a different name.
        </div>
      )}

      <div className="grid lg:grid-cols-[1fr_340px] gap-6 items-start">
        {/* ── Left: builder ── */}
        <div className="space-y-6 min-w-0">
          {/* Identity */}
          <section className="island-shell p-5 space-y-4">
            <SectionHead title="Identity" hint="The name is the pool's permanent handle — pipelines reference it via runner:." />
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Name <span className="text-destructive">*</span></label>
              <input
                value={name} onChange={(e) => setName(e.target.value)}
                disabled={editing} autoFocus={!editing} placeholder="gpu-large"
                className={`${inputClass} font-mono`}
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Description</label>
              <textarea
                value={description} onChange={(e) => setDescription(e.target.value)}
                rows={2} placeholder="What is this pool for?"
                className={`${inputClass} resize-none`}
              />
            </div>
          </section>

          {/* Capacity */}
          <section className="island-shell p-5 space-y-4">
            <SectionHead title="Per-job capacity" hint="Optional default requests stamped on each job pod (a job is one pod). Leave blank for unlimited (no request, so the job sizes itself); limits default to requests." />
            <div className="grid grid-cols-3 gap-3">
              <Field label="CPU" hint="cores"><UnitInput value={cpu} onChange={setCpu} suffix="vCPU" placeholder="unlimited" /></Field>
              <Field label="Memory" hint="GB"><UnitInput value={memory} onChange={setMemory} suffix="GB" placeholder="unlimited" /></Field>
              <Field label="Arch"><FormSelect value={arch} onChange={setArch} options={[{ key: 'amd64', label: 'amd64' }, { key: 'arm64', label: 'arm64' }, { key: 'any', label: 'Any' }]} /></Field>
            </div>

            <ToggleRow checked={hasGpu} onChange={setHasGpu} icon={<Microchip size={13} />} label="This pool offers GPUs" />
            {hasGpu && (
              <div className="space-y-1.5 pl-1">
                <div className="grid grid-cols-3 gap-3">
                  <Field label="Vendor"><ComboBox value={gpuVendor} onChange={setGpuVendor} options={GPU_VENDOR_PRESETS} placeholder="nvidia" /></Field>
                  <Field label="Model"><ComboBox value={gpuModel} onChange={setGpuModel} options={GPU_MODEL_PRESETS} placeholder="a100" /></Field>
                  <Field label="Count"><input value={gpuCount} onChange={(e) => setGpuCount(e.target.value)} className={`${inputClass} font-mono`} /></Field>
                </div>
                <p className="text-[10px] text-muted-foreground">Stamps <code className="font-mono">{gpuVendor.trim() || 'nvidia'}.com/gpu={gpuCount || 1}</code> on each pod and validates GPU jobs target this pool.</p>
              </div>
            )}
          </section>

          {/* Node targeting (reference only) */}
          {!managed && (
            <section className="island-shell p-5 space-y-4">
              <SectionHead title="Node targeting" hint="Where this pool's pods land. Stamped onto each job pod as nodeSelector + tolerations." />
              {arch !== 'any' && !nodeSelector.some((r) => r.key.trim() === ARCH_KEY) && (
                <button
                  type="button"
                  onClick={() => setNodeSelector([...nodeSelector, { key: ARCH_KEY, value: arch }])}
                  className="inline-flex w-fit items-center gap-1.5 rounded-lg border border-primary/50 bg-primary/10 px-3 py-1.5 text-xs font-medium text-primary shadow-sm transition-all hover:bg-primary/20 active:scale-[0.98]"
                >
                  <Plus size={13} /> Pin architecture <span className="font-mono opacity-80">({arch})</span>
                </button>
              )}

              <KVEditor label="Node selector" rows={nodeSelector} onChange={setNodeSelector} keyOptions={NODE_KEY_PRESETS} keyPlaceholder="karpenter.sh/nodepool" valuePlaceholder="ci-compute" addLabel="Add label" />
              <TolerationEditor rows={tolerations} onChange={setTolerations} />

              <p className="text-[11px] text-muted-foreground flex items-start gap-1.5">
                <Tag size={12} className="mt-0.5 shrink-0" />
                Point at an existing Karpenter NodePool with <code className="font-mono">karpenter.sh/nodepool</code>, an EKS node group, or any label your nodes carry. Add a toleration to land on dedicated/tainted CI nodes.
              </p>
            </section>
          )}

          {/* Provisioning mode */}
          <section className="island-shell p-5 space-y-4">
            <SectionHead title="Provisioning" hint={editing ? 'Set at creation — fixed for the life of the pool.' : 'How nodes for this pool come to exist. Chosen now and fixed afterwards.'} />
            <div className="grid sm:grid-cols-2 gap-3">
              <ModeCard
                active={!managed} disabled={editing} onClick={() => setManaged(false)}
                icon={<Server size={15} />} title="Reference"
                desc="Targets nodes that already exist. You manage the node group; Flint stamps selectors + tolerations."
              />
              <ModeCard
                active={managed}
                disabled={editing || (!managedAvailable && !managed)}
                onClick={() => setManaged(true)}
                icon={<Boxes size={15} />} title="Managed"
                desc="Flint renders a Karpenter NodePool (AWS) — scale-to-zero, CI-isolated, applied via GitOps."
                note={!editing && !managedAvailable ? 'Unavailable — no provisioning profile configured' : undefined}
              />
            </div>
            {editing && (
              <p className="text-[11px] text-muted-foreground">
                Provisioning mode can't be changed — the two store different scheduling. To switch, delete this pool and create a new one.
              </p>
            )}
            {managed && !managedAvailable && (
              <div className="rounded-lg border border-warning/30 bg-warning/5 px-3 py-2 text-[11px] text-warning">
                No provisioning profile is configured, so this pool's Karpenter manifests can't be rendered. Set <code className="font-mono">provisioning</code> (role + subnet/SG discovery tags) in the server config, with Karpenter installed in-cluster.
              </div>
            )}

            {managed && (
              <div className="rounded-xl border border-primary/20 p-4 space-y-4" style={{ background: 'color-mix(in oklab, var(--primary) 5%, var(--surface))' }}>
                <div className="grid grid-cols-2 gap-3">
                  <Field label="Capacity"><FormSelect value={capacityType} onChange={(v) => setCapacityType(v as CapacityType)} options={CAPACITY_OPTIONS} /></Field>
                  <Field label="Root disk (GiB)"><input value={diskGiB} onChange={(e) => setDiskGiB(e.target.value)} className={`${inputClass} font-mono`} /></Field>
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <Field label="vCPU limit" hint="pool-wide cap">
                    <input value={cpuLimit} onChange={(e) => setCpuLimit(e.target.value)} placeholder="unlimited" className={`${inputClass} font-mono`} />
                  </Field>
                  {hasGpu ? (
                    <Field label="GPU limit" hint="pool-wide cap">
                      <input value={gpuLimit} onChange={(e) => setGpuLimit(e.target.value)} placeholder="unlimited" className={`${inputClass} font-mono`} />
                    </Field>
                  ) : (
                    <Field label="AMI family" hint="overrides default">
                      <ComboBox value={amiFamily} onChange={setAmiFamily} options={AMI_PRESETS} placeholder="AL2023" />
                    </Field>
                  )}
                </div>
                <Field label="Instance families" hint="empty = let Karpenter choose">
                  <PillSelect values={families} onChange={setFamilies} options={FAMILY_PRESETS} addPlaceholder="c8g" />
                </Field>
                <div className="border-t border-primary/15 pt-3 space-y-3">
                  <ToggleRow checked={scaleToZero} onChange={setScaleToZero} icon={<Cloud size={13} />} label="Scale to zero when idle" />
                  {scaleToZero && (
                    <Field label="Consolidate after" hint="how long an empty node lingers">
                      <div className="max-w-[200px]"><ComboBox value={consolidateAfter} onChange={setConsolidateAfter} options={CONSOLIDATE_PRESETS} placeholder="30s" /></div>
                    </Field>
                  )}
                </div>
                <p className="text-[11px] text-muted-foreground flex items-start gap-1.5">
                  <FileCode size={12} className="mt-0.5 shrink-0" />
                  Requires Karpenter in-cluster and a configured provisioning profile. Apply the rendered manifests from the preview.
                </p>
              </div>
            )}
          </section>

          {/* Default pool (edit only) — exactly one pool is the default. */}
          {editing && (
            <section className="island-shell p-5 flex flex-col sm:flex-row sm:items-center justify-between gap-3">
              <div className="min-w-0">
                <h3 className="text-sm font-semibold text-foreground flex items-center gap-2">
                  Default pool {isDefault && <Badge variant="primary">Default</Badge>}
                </h3>
                <p className="text-xs text-muted-foreground mt-0.5">
                  {isDefault
                    ? 'Pipelines that set no runner: use this pool. Promote another pool to change or delete it.'
                    : 'Make this the pool pipelines use when they set no runner:.'}
                </p>
              </div>
              {!isDefault && (
                <button
                  type="button" onClick={() => setDefault.mutate({ name: pool!.name })} disabled={setDefault.isPending}
                  className="shrink-0 inline-flex items-center gap-1.5 rounded-lg border border-primary/40 bg-primary/5 px-3 py-1.5 text-xs font-medium text-primary transition-colors hover:bg-primary/10 disabled:opacity-40"
                >
                  <Star size={13} /> {setDefault.isPending ? 'Setting…' : 'Set as default'}
                </button>
              )}
            </section>
          )}

          {/* Delete (edit only) — destructive, behind a type-the-name confirm. The
              default pool is protected until another is promoted. */}
          {editing && (
            <section
              className="rounded-xl border border-destructive/30 p-5 flex flex-col sm:flex-row sm:items-center justify-between gap-3"
              style={{ background: 'color-mix(in oklab, var(--destructive) 4%, var(--surface))' }}
            >
              <div className="min-w-0">
                <h3 className="text-sm font-semibold text-foreground">Delete this pool</h3>
                <p className="text-xs text-muted-foreground mt-0.5">
                  {isDefault
                    ? "The default pool can't be deleted — set another pool as default first."
                    : <>Permanent. Pipelines that reference <span className="font-mono">{pool!.name}</span> will fail to compile until repointed.</>}
                </p>
              </div>
              <button
                type="button" disabled={isDefault} onClick={() => setShowDelete(true)}
                className="shrink-0 inline-flex items-center gap-1.5 rounded-lg border border-destructive/40 bg-destructive/5 px-3 py-1.5 text-xs font-medium text-destructive transition-colors hover:bg-destructive/10 disabled:opacity-40 disabled:cursor-not-allowed"
              >
                <Trash2 size={13} /> Delete pool
              </button>
            </section>
          )}
        </div>

        {/* ── Right: live preview ── */}
        <aside className="lg:sticky lg:top-20 space-y-4">
          <PreviewPanel
            name={name} arch={arch} cpu={cpu} memory={memory}
            hasGpu={hasGpu} gpuVendor={gpuVendor} gpuModel={gpuModel} gpuCount={gpuCount}
            managed={managed} capacityType={capacityType} scaleToZero={scaleToZero}
            nodeSelector={nodeSelector} tolerations={tolerations}
          />
          {managed && editing && <ManifestsPanel name={pool!.name} />}
        </aside>
      </div>
    </div>
  )
}

// ── Building blocks ──

function SectionHead({ title, hint }: { title: string; hint: string }) {
  return (
    <div>
      <h3 className="text-sm font-semibold text-foreground">{title}</h3>
      <p className="text-xs text-muted-foreground mt-0.5">{hint}</p>
    </div>
  )
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <label className="text-xs font-medium text-foreground flex items-baseline gap-1.5">
        {label}
        {hint && <span className="text-[10px] font-normal text-muted-foreground">{hint}</span>}
      </label>
      {children}
    </div>
  )
}

// UnitInput is a numeric field with a fixed unit suffix, so authors enter a
// plain number (e.g. memory in GB) and never have to type Mi/Gi.
function UnitInput({ value, onChange, suffix, placeholder }: { value: string; onChange: (v: string) => void; suffix: string; placeholder?: string }) {
  return (
    <div className="flex items-center gap-1.5 rounded-lg border border-border bg-transparent px-3 focus-within:ring-2 focus-within:ring-ring/40">
      <input
        value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder}
        inputMode="decimal"
        className="flex-1 min-w-0 bg-transparent py-2 text-sm font-mono text-foreground outline-none placeholder:text-muted-foreground/50"
      />
      <span className="shrink-0 text-[11px] font-medium text-muted-foreground">{suffix}</span>
    </div>
  )
}

// memToGB renders a stored Kubernetes memory quantity (e.g. "8Gi", "512Mi") as a
// plain GB number for editing. Saving re-appends "Gi".
function memToGB(s: string): string {
  const t = s.trim()
  if (!t) return ''
  const m = t.match(/^([0-9.]+)\s*([A-Za-z]*)$/)
  if (!m) return ''
  const n = parseFloat(m[1])
  if (Number.isNaN(n)) return ''
  let gb: number
  switch (m[2]) {
    case 'Gi': case 'G': case '': gb = n; break
    case 'Mi': gb = n / 1024; break
    case 'M': gb = n / 1000; break
    case 'Ki': gb = n / 1024 / 1024; break
    case 'Ti': case 'T': gb = n * 1024; break
    default: gb = n
  }
  return String(Math.round(gb * 100) / 100)
}

function ToggleRow({ checked, onChange, icon, label }: { checked: boolean; onChange: (b: boolean) => void; icon: React.ReactNode; label: string }) {
  return (
    <label className="flex items-center gap-2 text-xs font-medium text-foreground cursor-pointer select-none">
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} className="accent-primary" />
      <span className="text-muted-foreground">{icon}</span>
      {label}
    </label>
  )
}

function ModeCard({ active, disabled, onClick, icon, title, desc, note }: { active: boolean; disabled?: boolean; onClick: () => void; icon: React.ReactNode; title: string; desc: string; note?: string }) {
  return (
    <button
      type="button" onClick={onClick} disabled={disabled}
      className={`flex flex-col gap-1.5 rounded-xl border p-3.5 text-left transition-colors ${
        disabled ? 'border-border opacity-50 cursor-not-allowed' : active ? 'border-primary/50 bg-primary/5' : 'border-border hover:border-muted-foreground/40'
      }`}
    >
      <span className="flex items-center gap-2">
        <span className={`flex h-7 w-7 items-center justify-center rounded-lg ${active ? 'bg-primary/15 text-primary' : 'bg-muted text-muted-foreground'}`}>{icon}</span>
        <span className="text-sm font-semibold text-foreground">{title}</span>
        {active && <Check size={14} className="text-primary ml-auto" />}
      </span>
      <span className="text-[11px] leading-relaxed text-muted-foreground">{desc}</span>
      {note && <span className="text-[10px] font-medium text-warning">{note}</span>}
    </button>
  )
}

// ── Node selector key/value editor ──

function KVEditor({ label, rows, onChange, keyOptions, keyPlaceholder, valuePlaceholder, addLabel }: {
  label: string
  rows: KV[]
  onChange: (r: KV[]) => void
  keyOptions: ComboOption[]
  keyPlaceholder: string
  valuePlaceholder: string
  addLabel: string
}) {
  const set = (i: number, patch: Partial<KV>) => onChange(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)))
  const remove = (i: number) => onChange(rows.filter((_, j) => j !== i))
  return (
    <div className="space-y-2">
      <label className="text-xs font-medium text-foreground">{label}</label>
      {rows.map((row, i) => (
        <div key={i} className="flex items-center gap-2">
          <div className="flex-1"><ComboBox value={row.key} onChange={(v) => set(i, { key: v })} options={keyOptions} placeholder={keyPlaceholder} /></div>
          <span className="text-muted-foreground text-xs">=</span>
          <input value={row.value} onChange={(e) => set(i, { value: e.target.value })} placeholder={valuePlaceholder} className={`${inputClass} font-mono flex-1`} />
          <button type="button" onClick={() => remove(i)} className="shrink-0 flex h-8 w-8 items-center justify-center rounded-lg border border-border text-muted-foreground hover:text-destructive hover:border-destructive/40 transition-colors">
            <X size={13} />
          </button>
        </div>
      ))}
      <button type="button" onClick={() => onChange([...rows, { key: '', value: '' }])} className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors">
        <Plus size={13} /> {addLabel}
      </button>
    </div>
  )
}

function TolerationEditor({ rows, onChange }: { rows: Tol[]; onChange: (r: Tol[]) => void }) {
  const set = (i: number, patch: Partial<Tol>) => onChange(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)))
  const remove = (i: number) => onChange(rows.filter((_, j) => j !== i))
  return (
    <div className="space-y-2">
      <label className="text-xs font-medium text-foreground">Tolerations</label>
      {rows.map((row, i) => (
        <div key={i} className="flex items-center gap-2 flex-wrap sm:flex-nowrap">
          <input value={row.key} onChange={(e) => set(i, { key: e.target.value })} placeholder="dedicated" className={`${inputClass} font-mono flex-1 min-w-[120px]`} />
          <div className="w-[112px] shrink-0">
            <FormSelect value={row.operator} onChange={(v) => set(i, { operator: v as Tol['operator'] })} options={[{ key: 'Equal', label: 'Equal' }, { key: 'Exists', label: 'Exists' }]} />
          </div>
          <input value={row.value} disabled={row.operator === 'Exists'} onChange={(e) => set(i, { value: e.target.value })} placeholder="ci" className={`${inputClass} font-mono flex-1 min-w-[100px]`} />
          <div className="w-[148px] shrink-0">
            <FormSelect value={row.effect} onChange={(v) => set(i, { effect: v as Tol['effect'] })} options={[{ key: 'NoSchedule', label: 'NoSchedule' }, { key: 'PreferNoSchedule', label: 'PreferNoSchedule' }, { key: 'NoExecute', label: 'NoExecute' }]} />
          </div>
          <button type="button" onClick={() => remove(i)} className="shrink-0 flex h-8 w-8 items-center justify-center rounded-lg border border-border text-muted-foreground hover:text-destructive hover:border-destructive/40 transition-colors">
            <X size={13} />
          </button>
        </div>
      ))}
      <button type="button" onClick={() => onChange([...rows, { key: '', operator: 'Equal', value: '', effect: 'NoSchedule' }])} className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors">
        <Plus size={13} /> Add toleration
      </button>
    </div>
  )
}

// ── Live preview ──

function PreviewPanel({
  name, arch, cpu, memory, hasGpu, gpuVendor, gpuModel, gpuCount, managed, capacityType, scaleToZero, nodeSelector, tolerations,
}: {
  name: string; arch: string; cpu: string; memory: string
  hasGpu: boolean; gpuVendor: string; gpuModel: string; gpuCount: string
  managed: boolean; capacityType: CapacityType; scaleToZero: boolean
  nodeSelector: KV[]; tolerations: Tol[]
}) {
  const poolName = name.trim() || 'pool-name'
  // Reference pools don't control capacity-type — it's whatever the targeted
  // nodes are. Surface it only if a capacity-type label is in the selector.
  const capacity = managed
    ? (capacityType === 'on-demand' ? 'On-demand' : capacityType === 'spot' ? 'Spot only' : 'Spot, on-demand fallback')
    : (referenceCapacity(nodeSelector) ?? '—')
  const selectors = nodeSelector.filter((r) => r.key.trim())
  const tolCount = tolerations.filter((t) => t.key.trim()).length
  const compute = [cpu.trim() && `${cpu.trim()} vCPU`, memory.trim() && `${memory.trim()} GB`].filter(Boolean).join(' · ')

  return (
    <div className="island-shell p-4 space-y-3.5">
      <div className="flex items-center gap-2">
        {managed ? <Boxes size={14} className="text-primary" /> : <Server size={14} className="text-primary" />}
        <h3 className="text-sm font-semibold text-foreground">Pool preview</h3>
      </div>

      <dl className="space-y-2">
        <Row icon={<Cpu size={12} />} label="Compute">
          {compute ? <span className="font-mono text-foreground">{compute}</span> : <span className="text-muted-foreground/70">set per job</span>}
        </Row>
        <Row icon={<HardDrive size={12} />} label="Arch">
          <span className="font-mono">{arch === 'any' ? 'any' : arch}</span>
        </Row>
        <Row icon={<Microchip size={12} />} label="GPU">
          {hasGpu
            ? <span>{Number(gpuCount) > 1 ? `${gpuCount}× ` : ''}<span className="font-mono">{gpuVendor || 'gpu'}{gpuModel ? ` ${gpuModel}` : ''}</span></span>
            : <span className="text-muted-foreground/70 inline-flex items-center gap-1"><Ban size={11} /> none</span>}
        </Row>
        <Row icon={managed ? <Cloud size={12} /> : <Zap size={12} />} label="Capacity">{capacity}</Row>
      </dl>

      {managed ? (
        <div className="border-t border-border/60 pt-3 space-y-2">
          <p className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Derived scheduling</p>
          <SchedLine icon={<Tag size={11} />} text={`flint.dev/pool=${poolName}`} />
          <SchedLine icon={<Ban size={11} />} text={`flint.dev/ci=${poolName}:NoSchedule`} />
          <p className="text-[11px] text-muted-foreground pt-0.5">
            {scaleToZero ? 'Scales to zero when no jobs are queued.' : 'Keeps at least one node warm.'}
          </p>
        </div>
      ) : (
        <div className="border-t border-border/60 pt-3 space-y-2">
          <p className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Targets nodes matching</p>
          {selectors.length > 0
            ? selectors.map((s) => <SchedLine key={s.key} icon={<Tag size={11} />} text={`${s.key}=${s.value || '…'}`} />)
            : <p className="text-[11px] text-muted-foreground/70">Any node (no selector set)</p>}
          <p className="text-[11px] text-muted-foreground pt-0.5">
            {tolCount > 0 ? `Tolerates ${tolCount} taint${tolCount > 1 ? 's' : ''}.` : 'No taints tolerated.'}
          </p>
        </div>
      )}
    </div>
  )
}

function Row({ icon, label, children }: { icon: React.ReactNode; label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-2 text-xs">
      <span className="text-muted-foreground shrink-0">{icon}</span>
      <dt className="text-muted-foreground w-20 shrink-0">{label}</dt>
      <dd className="text-foreground min-w-0 truncate">{children}</dd>
    </div>
  )
}

// referenceCapacity reads a capacity-type (spot/on-demand) from the pool's node
// selector if it carries a known cloud capacity-type label — that's the only way
// a reference pool's spot-ness is knowable to Flint.
function referenceCapacity(ns: KV[]): string | undefined {
  const get = (k: string) => ns.find((r) => r.key.trim().toLowerCase() === k)?.value.trim().toLowerCase()
  const ct = get('karpenter.sh/capacity-type') ?? get('eks.amazonaws.com/capacitytype')
  if (ct === 'spot') return 'Spot'
  if (ct === 'on-demand' || ct === 'on_demand') return 'On-demand'
  if (get('cloud.google.com/gke-spot') === 'true') return 'Spot'
  if (get('kubernetes.azure.com/scalesetpriority') === 'spot') return 'Spot'
  return undefined
}

function SchedLine({ icon, text }: { icon: React.ReactNode; text: string }) {
  return (
    <div className="flex items-center gap-1.5 text-[11px] text-foreground">
      <span className="text-muted-foreground shrink-0">{icon}</span>
      <code className="font-mono truncate">{text}</code>
    </div>
  )
}

// ── Manifests (managed + edit only — server renders from the saved pool) ──

function download(filename: string, text: string) {
  const blob = new Blob([text], { type: 'text/yaml' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

function ManifestsPanel({ name }: { name: string }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="island-shell overflow-hidden">
      <button
        type="button" onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 px-4 py-3 text-left hover:bg-accent/30 transition-colors"
      >
        {open ? <ChevronDown size={14} className="text-muted-foreground" /> : <ChevronRight size={14} className="text-muted-foreground" />}
        <FileCode size={14} className="text-primary" />
        <span className="text-sm font-semibold text-foreground">Karpenter manifests</span>
        <span className="text-[11px] text-muted-foreground ml-auto">render-to-GitOps</span>
      </button>
      {open && <ManifestsBody name={name} />}
    </div>
  )
}

function ManifestsBody({ name }: { name: string }) {
  const { data, isLoading, isError } = useQuery(orpc.runners.manifests.queryOptions({ input: { name } }))
  return (
    <div className="px-4 pb-4 pt-1 border-t border-border/50 space-y-3">
      {isLoading && <p className="text-xs text-muted-foreground">Rendering…</p>}
      {isError && <p className="text-xs text-destructive">Could not render — is the provisioning profile configured?</p>}
      {data && (
        <>
          <div className="flex items-center justify-end gap-2">
            <button
              type="button" onClick={() => navigator.clipboard?.writeText(data.combined)}
              className="flex items-center gap-1.5 rounded-md border border-border px-2.5 py-1 text-xs text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
            >
              <Copy size={12} /> Copy
            </button>
            <button
              type="button" onClick={() => download(`${name}-karpenter.yaml`, data.combined)}
              className="flex items-center gap-1.5 rounded-md border border-border px-2.5 py-1 text-xs text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
            >
              <Download size={12} /> Download
            </button>
          </div>
          <pre className="max-h-[420px] overflow-auto rounded-lg border border-border bg-muted/40 p-3 text-[11px] font-mono leading-relaxed">{data.combined}</pre>
        </>
      )}
    </div>
  )
}

// DeletePoolModal — AWS-style destructive confirm: the Delete button only enables
// once the operator types the pool's exact name.
function DeletePoolModal({ name, managed, onClose }: { name: string; managed: boolean; onClose: () => void }) {
  const navigate = useNavigate()
  const [confirm, setConfirm] = useState('')
  const del = useAction(client.runners.delete, {
    invalidate: [orpc.runners.list.key()],
    onSuccess: () => navigate({ to: '/settings/runners' }),
  })
  const match = confirm.trim() === name

  return (
    <Modal open onClose={onClose} title="Delete runner pool" subtitle="This action can't be undone.">
      <div className="px-6 py-5 space-y-5">
        <div className="flex items-start gap-3 rounded-lg border border-destructive/30 bg-destructive/5 p-4">
          <AlertTriangle size={16} className="text-destructive mt-0.5 shrink-0" />
          <p className="text-sm leading-relaxed text-foreground">
            Pipelines that reference <span className="font-mono">{name}</span> via <code className="font-mono">runner:</code> will fail validation until repointed.
            {managed && <> Its rendered Karpenter manifests stay in your GitOps until you remove them there.</>}
          </p>
        </div>
        <div className="space-y-2">
          <label className="text-sm font-medium text-foreground">
            Type <span className="font-mono text-destructive">{name}</span> to confirm
          </label>
          <input
            value={confirm} onChange={(e) => setConfirm(e.target.value)} autoFocus
            placeholder={name}
            onKeyDown={(e) => { if (e.key === 'Enter' && match && !del.isPending) del.mutate({ name }) }}
            className="w-full rounded-lg border border-border bg-transparent px-3.5 py-2.5 text-sm font-mono text-foreground placeholder:text-muted-foreground/40 focus:outline-none focus:ring-2 focus:ring-destructive/40"
          />
        </div>
        {del.isError && <p className="text-sm text-destructive">Could not delete the pool — try again.</p>}
      </div>
      <div className="flex items-center justify-end gap-2.5 px-6 py-4 border-t border-border">
        <button type="button" onClick={onClose} className="rounded-lg border border-border px-4 py-2 text-sm font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
          Cancel
        </button>
        <button
          type="button" disabled={!match || del.isPending} onClick={() => del.mutate({ name })}
          className="inline-flex items-center gap-1.5 rounded-lg bg-destructive px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-destructive/90 disabled:opacity-40"
        >
          <Trash2 size={14} /> {del.isPending ? 'Deleting…' : 'Delete pool'}
        </button>
      </div>
    </Modal>
  )
}
