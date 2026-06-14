import { useEffect, type RefObject } from 'react'

/**
 * Calls `handler` on a pointer-down outside `ref`. Only subscribes while
 * `active` (so closed popovers don't hold a global listener). Standardizes the
 * outside-to-close pattern repeated across dropdowns/popovers.
 *
 *   const ref = useRef<HTMLDivElement>(null)
 *   useClickOutside(ref, () => setOpen(false), open)
 */
export function useClickOutside<T extends HTMLElement>(
  ref: RefObject<T | null>,
  handler: () => void,
  active = true,
) {
  useEffect(() => {
    if (!active) return
    function onDown(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) handler()
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [ref, handler, active])
}
