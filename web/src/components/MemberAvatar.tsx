// Deterministic initials avatar, themed to the Ocean chart tokens (was three
// duplicated copies using raw Tailwind palette colors that washed out in light
// mode). Color is picked from --chart-1..5 by a stable hash of the name.
const CHART_VARS = ['--chart-1', '--chart-2', '--chart-3', '--chart-4', '--chart-5']

export function MemberAvatar({ name, size = 32 }: { name: string; size?: number }) {
  const label = name || '?'
  const hash = [...label].reduce((acc, c) => acc + c.charCodeAt(0), 0)
  const v = CHART_VARS[hash % CHART_VARS.length]
  const initials =
    label.split(/[\s@]+/).filter(Boolean).slice(0, 2).map((w) => w[0]?.toUpperCase() ?? '').join('') || '?'

  return (
    <div
      className="rounded-full flex items-center justify-center shrink-0 font-semibold"
      style={{
        width: size,
        height: size,
        fontSize: Math.round(size * 0.36),
        background: `color-mix(in oklab, var(${v}) 22%, transparent)`,
        color: `var(${v})`,
      }}
    >
      {initials}
    </div>
  )
}
