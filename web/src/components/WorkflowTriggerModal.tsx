import { useRef, useState } from 'react'
import { Upload, Play, Loader2, AlertTriangle } from 'lucide-react'
import { Modal } from './Modal'
import { client } from '#/lib/orpc'

const PLACEHOLDER = `name: my-workflow
steps:
  - name: prepare
    run: echo "preparing"
  - name: execute
    run: echo "working"
    dependsOn: [prepare]`

export function WorkflowTriggerModal({
  open,
  onClose,
  onTriggered,
}: {
  open: boolean
  onClose: () => void
  onTriggered: (runId: string) => void
}) {
  const [yaml, setYaml] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  async function handleFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    setYaml(await file.text())
    setError(null)
    e.target.value = '' // allow re-uploading the same file
  }

  async function submit() {
    if (!yaml.trim()) {
      setError('Paste or upload a workflow definition first.')
      return
    }
    setSubmitting(true)
    setError(null)
    try {
      const res = await client.workflows.trigger({ definition: yaml })
      setYaml('')
      onTriggered(res.runId)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to trigger workflow.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title="Run a workflow" subtitle="Paste or upload a workflow definition (YAML)" wide>
      <div className="flex flex-col gap-3 px-5 py-4 min-h-0">
        <div className="flex items-center justify-between">
          <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground/50">
            Definition
          </span>
          <button
            type="button"
            onClick={() => fileRef.current?.click()}
            className="inline-flex items-center gap-1.5 rounded-md border border-border px-2.5 py-1 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent/40 transition-colors"
          >
            <Upload size={13} /> Upload .yaml
          </button>
          <input
            ref={fileRef}
            type="file"
            accept=".yaml,.yml,text/yaml"
            onChange={handleFile}
            className="hidden"
          />
        </div>

        <textarea
          value={yaml}
          onChange={(e) => { setYaml(e.target.value); setError(null) }}
          placeholder={PLACEHOLDER}
          spellCheck={false}
          rows={12}
          className="w-full resize-none rounded-lg border border-border bg-[var(--surface)] px-3 py-2.5 font-mono text-xs leading-relaxed text-foreground placeholder:text-muted-foreground/40 focus:outline-none focus:ring-2 focus:ring-[var(--ring)]/40"
        />

        {error && (
          <div className="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
            <AlertTriangle size={14} className="mt-px shrink-0" />
            <span className="font-mono break-all">{error}</span>
          </div>
        )}

        <div className="flex items-center justify-end gap-2 pt-1">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg px-3 py-2 text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={submitting}
            className="inline-flex items-center gap-1.5 rounded-lg px-3.5 py-2 text-sm font-medium text-white shadow-sm disabled:opacity-60"
            style={{ background: 'linear-gradient(135deg, var(--primary), color-mix(in oklab, var(--primary), black 12%))' }}
          >
            {submitting ? <Loader2 size={15} className="animate-spin" /> : <Play size={15} strokeWidth={2.2} />}
            {submitting ? 'Starting…' : 'Run workflow'}
          </button>
        </div>
      </div>
    </Modal>
  )
}
