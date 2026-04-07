import { useState, useCallback, useRef } from 'react'

/**
 * Copy text to clipboard with a temporary "copied" feedback state.
 *
 * Usage:
 *   const { copied, copy } = useCopyToClipboard()
 *   <button onClick={() => copy(token)}>{copied ? 'Copied' : 'Copy'}</button>
 */
export function useCopyToClipboard(resetMs = 2000) {
  const [copied, setCopied] = useState(false)
  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined)

  const copy = useCallback(
    (text: string) => {
      navigator.clipboard.writeText(text)
      setCopied(true)
      clearTimeout(timerRef.current)
      timerRef.current = setTimeout(() => setCopied(false), resetMs)
    },
    [resetMs],
  )

  return { copied, copy }
}
