// The Flint mark — six struck sparks radiating from a single point of impact.
//
// Drawn as flat polygons with `currentColor`, so it takes whichever accent the
// active palette supplies: set the colour on a parent (or pass a text-* class)
// and the mark follows. That is why there is no per-theme copy of this file —
// Ember's orange and Verdigris' teal are the same component, coloured by token.
//
// Geometry is traced from the source artwork and normalised into a 100x100
// viewBox with 10% padding. Static exports for docs live in public/brand/.

export function FlintMark({ size = 28, className }: { size?: number; className?: string }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 100 100"
      fill="currentColor"
      role="img"
      aria-label="Flint"
      className={className}
    >
      <path d="M84.69 10.0 60.66 48.8 48.08 54.94 51.61 41.52Z" />
      <path d="M28.31 21.55 43.71 45.89 43.81 54.84 37.15 48.7Z" />
      <path d="M42.77 61.6 40.48 70.13 20.3 90.0 34.86 65.76Z" />
      <path d="M56.61 56.92 81.78 64.72 55.77 63.58 49.01 59.21Z" />
      <path d="M46.62 62.12 51.2 67.11 55.46 88.02 45.89 68.57Z" />
      <path d="M35.07 54.63 35.8 54.63 41.52 57.54 35.8 59.73 15.31 57.54Z" />
      <circle cx="45.36" cy="58.38" r="1.46" />
    </svg>
  )
}
