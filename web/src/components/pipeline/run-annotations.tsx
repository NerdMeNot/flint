import { lazy, Suspense } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Info, CheckCircle, AlertTriangle, XCircle } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import type { RunAnnotation } from '#/lib/api/types'

// RunAnnotations — markdown panels steps publish onto the run page (test
// summaries, coverage deltas, links to reports). Rendered between the run
// header and the step workspace so the highest-signal output reads first,
// without opening any logs. Absent (renders nothing) for runs without them.

// Markdown rendering is lazy: the run page shouldn't pay for react-markdown
// unless a run actually has annotations.
const MarkdownBody = lazy(async () => {
  const [{ default: ReactMarkdown }, { default: remarkGfm }] = await Promise.all([
    import('react-markdown'),
    import('remark-gfm'),
  ])
  function Body({ body }: { body: string }) {
    return (
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          a: (props) => <a {...props} className="text-primary hover:underline" target="_blank" rel="noreferrer" />,
          code: (props) => <code {...props} className="rounded bg-muted/60 px-1 py-0.5 font-mono text-[0.85em]" />,
          p: (props) => <p {...props} className="my-1 first:mt-0 last:mb-0" />,
          ul: (props) => <ul {...props} className="my-1 list-disc pl-5" />,
          ol: (props) => <ol {...props} className="my-1 list-decimal pl-5" />,
          table: (props) => (
            <div className="overflow-x-auto my-2">
              <table {...props} className="text-xs border-collapse" />
            </div>
          ),
          th: (props) => <th {...props} className="border border-border px-2 py-1 text-left font-semibold" />,
          td: (props) => <td {...props} className="border border-border px-2 py-1" />,
        }}
      >
        {body}
      </ReactMarkdown>
    )
  }
  return { default: Body }
})

const STYLE_META: Record<
  RunAnnotation['style'],
  { icon: React.ComponentType<{ size?: number | string; className?: string }>; accent: string; iconColor: string }
> = {
  info: { icon: Info, accent: 'border-l-sky-400/60', iconColor: 'text-sky-400' },
  success: { icon: CheckCircle, accent: 'border-l-success/70', iconColor: 'text-success' },
  warning: { icon: AlertTriangle, accent: 'border-l-warning/70', iconColor: 'text-warning' },
  error: { icon: XCircle, accent: 'border-l-destructive/70', iconColor: 'text-destructive' },
}

export function RunAnnotations({ runId, live }: { runId: string; live: boolean }) {
  const { data } = useQuery({
    ...orpc.runs.annotations.queryOptions({ input: { runId } }),
    refetchInterval: live ? 10_000 : false,
  })
  const annotations = data?.annotations ?? []
  if (annotations.length === 0) return null

  // Errors first, then warnings — the same severity-first ordering the rest
  // of the run page uses.
  const order: RunAnnotation['style'][] = ['error', 'warning', 'success', 'info']
  const sorted = [...annotations].sort((a, b) => order.indexOf(a.style) - order.indexOf(b.style))

  return (
    <div className="space-y-2 mb-4 lg:mb-5">
      {sorted.map((a) => {
        const meta = STYLE_META[a.style]
        const Icon = meta.icon
        return (
          <div key={a.id} className={`island-shell !p-0 overflow-hidden border-l-2 ${meta.accent}`}>
            <div className="flex items-start gap-3 px-4 py-3">
              <Icon size={15} className={`${meta.iconColor} shrink-0 mt-0.5`} />
              <div className="min-w-0 flex-1 text-sm text-foreground/90">
                <Suspense fallback={<span className="whitespace-pre-wrap">{a.body}</span>}>
                  <MarkdownBody body={a.body} />
                </Suspense>
              </div>
              <span className="shrink-0 rounded-full border border-border px-2 py-0.5 text-[10px] font-mono text-muted-foreground">
                {a.context}
              </span>
            </div>
          </div>
        )
      })}
    </div>
  )
}
