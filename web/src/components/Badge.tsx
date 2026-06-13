import type { ReactNode } from 'react'

export type BadgeVariant = 'neutral' | 'primary' | 'success' | 'warning' | 'danger'

// Theme-token-driven pill. Replaces the ad-hoc `island-kicker` + `border-x/20`
// badges scattered across the admin (which silently dropped their border and
// used off-theme raw palette colors). Always carries a real `border` width.
const VARIANTS: Record<BadgeVariant, string> = {
  neutral: 'bg-secondary border-border text-muted-foreground',
  primary: 'bg-primary/10 border-primary/25 text-primary',
  success: 'bg-success/10 border-success/30 text-success',
  warning: 'bg-warning/10 border-warning/30 text-warning',
  danger: 'bg-destructive/10 border-destructive/30 text-destructive',
}

export function Badge({ children, variant = 'neutral', className = '' }: {
  children: ReactNode
  variant?: BadgeVariant
  className?: string
}) {
  return (
    <span className={`inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[11px] font-medium whitespace-nowrap ${VARIANTS[variant]} ${className}`}>
      {children}
    </span>
  )
}
