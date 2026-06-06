import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'

interface ModalProps {
  open: boolean
  onClose: () => void
  title: string
  subtitle?: string
  children: React.ReactNode
  wide?: boolean
}

export function Modal({ open, onClose, title, subtitle, children, wide }: ModalProps) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)

  useEffect(() => {
    if (typeof document === 'undefined') return
    const el = document.createElement('div')
    document.body.appendChild(el)
    setContainer(el)
    return () => {
      document.body.removeChild(el)
    }
  }, [])

  useEffect(() => {
    if (!open) return
    const handleEsc = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', handleEsc)
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', handleEsc)
      document.body.style.overflow = ''
    }
  }, [open, onClose])

  if (!open || !container) return null

  return createPortal(
    <>
      <div
        className="fixed inset-0 z-[100] bg-black/50"
        onClick={onClose}
      />
      <div className="fixed inset-y-0 right-0 left-0 lg:left-[var(--sidebar-width,220px)] z-[100] flex items-start justify-center pt-[12vh] px-4 pointer-events-none">
        <div
          className={`${wide ? 'max-w-lg' : 'max-w-md'} w-full rounded-xl border border-border shadow-2xl rise-in flex flex-col max-h-[75vh] pointer-events-auto`}
          style={{ background: 'var(--surface-strong)' }}
          onClick={(e) => e.stopPropagation()}
        >
          <div className="flex items-center justify-between px-5 py-4 border-b border-border shrink-0">
            <div>
              <h2 className="text-sm font-semibold text-foreground">{title}</h2>
              {subtitle && <p className="text-xs text-muted-foreground mt-0.5">{subtitle}</p>}
            </div>
            <button
              type="button"
              onClick={onClose}
              className="flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
            >
              <X size={15} />
            </button>
          </div>
          {children}
        </div>
      </div>
    </>,
    container,
  )
}
