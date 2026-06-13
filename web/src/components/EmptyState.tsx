import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'

// The canonical top-level empty state: an island card with a large soft icon,
// a message, and an optional action. Standardizes the icon size / padding that
// several admin pages had each reimplemented slightly differently.
export function EmptyState({ icon: Icon, message, action }: {
  icon: LucideIcon
  message: string
  action?: ReactNode
}) {
  return (
    <div className="island-shell p-12 flex flex-col items-center gap-3 text-center text-muted-foreground">
      <Icon size={32} strokeWidth={1.2} />
      <span className="text-sm">{message}</span>
      {action}
    </div>
  )
}
