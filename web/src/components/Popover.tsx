import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

type Placement = { left: number; width: number; top?: number; bottom?: number; maxHeight: number }

// Popover renders its children in a portal to <body>, positioned under (or above)
// an anchor element with `position: fixed`. Portaling escapes the stacking
// contexts created by the admin's `island-shell` cards (each has backdrop-filter),
// so a long dropdown is never painted over by a sibling card or clipped by one.
// It flips above the anchor when there isn't room below, and repositions on
// scroll/resize. Outside-clicks (anywhere but the anchor or the menu) close it.
export function Popover({ anchorRef, open, onClose, children }: {
  anchorRef: React.RefObject<HTMLElement | null>
  open: boolean
  onClose: () => void
  children: React.ReactNode
}) {
  const menuRef = useRef<HTMLDivElement>(null)
  const [place, setPlace] = useState<Placement | null>(null)

  useLayoutEffect(() => {
    if (!open) return
    const measure = () => {
      const el = anchorRef.current
      if (!el) return
      const r = el.getBoundingClientRect()
      const spaceBelow = window.innerHeight - r.bottom
      const spaceAbove = r.top
      const openUp = spaceBelow < 220 && spaceAbove > spaceBelow
      const maxHeight = Math.max(120, Math.min(260, (openUp ? spaceAbove : spaceBelow) - 12))
      setPlace(openUp
        ? { left: r.left, width: r.width, bottom: window.innerHeight - r.top + 4, maxHeight }
        : { left: r.left, width: r.width, top: r.bottom + 4, maxHeight })
    }
    measure()
    window.addEventListener('scroll', measure, true)
    window.addEventListener('resize', measure)
    return () => {
      window.removeEventListener('scroll', measure, true)
      window.removeEventListener('resize', measure)
    }
  }, [open, anchorRef])

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (anchorRef.current?.contains(t) || menuRef.current?.contains(t)) return
      onClose()
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open, anchorRef, onClose])

  if (!open || !place) return null
  return createPortal(
    <div
      ref={menuRef}
      className="fixed rounded-lg border border-border overlay-edge overflow-hidden overflow-y-auto"
      // Fully opaque (opaque --background base + --popover tint) so nothing shows
      // through, and a high z so it clears every island/card.
      style={{
        left: place.left, width: place.width, top: place.top, bottom: place.bottom,
        maxHeight: place.maxHeight, zIndex: 1000,
        backgroundColor: 'var(--background)', backgroundImage: 'linear-gradient(var(--popover), var(--popover))',
      }}
    >
      {children}
    </div>,
    document.body,
  )
}
