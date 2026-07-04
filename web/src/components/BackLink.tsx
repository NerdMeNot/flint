import { useRouter } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'

// Returns to the previous page so a filtered list comes back with its filters
// intact (they live in the URL); falls back to a fixed route on deep-link
// arrival, when there's no in-app history to go back to.
export function BackLink({ fallbackTo, label }: { fallbackTo: '/ci/projects' | '/ci/runs' | '/fleet'; label: string }) {
  const router = useRouter()
  const goBack = () => {
    if (router.history.canGoBack()) router.history.back()
    else router.navigate({ to: fallbackTo })
  }
  return (
    <button
      type="button"
      onClick={goBack}
      className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
    >
      <ArrowLeft size={14} /> {label}
    </button>
  )
}
