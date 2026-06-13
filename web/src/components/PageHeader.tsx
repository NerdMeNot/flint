import type { ReactNode } from 'react'

// The standard settings page header: a serif title, an optional count/subtitle,
// and an optional right-aligned action (usually the primary "Add" button).
// Keeps every admin page's header identical in scale and spacing.
export function PageHeader({ title, subtitle, action }: {
  title: string
  subtitle?: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="min-w-0">
        <h2 className="display-title text-lg text-foreground">{title}</h2>
        {subtitle != null && subtitle !== '' && (
          <p className="text-muted-foreground text-xs mt-0.5">{subtitle}</p>
        )}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  )
}
