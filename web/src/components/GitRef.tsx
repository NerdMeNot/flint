// Git refs are the most reliably layout-breaking data in the product: branch
// names come from ticket templates and revert-of-a-hotfix chains (52+ chars in
// our own fixtures), and a commit SHA is always exactly 40. Both were being
// rendered raw in dense metadata rows, where they pushed durations, timestamps
// and actors past the right edge — overflow hides data more quietly than
// wrapping does, so it survived several passes unnoticed.
//
// These exist so the rule is enforced by construction rather than remembered at
// each call site: a branch always truncates with its full value in the title, a
// SHA always shows the 7 characters every forge shows, never all 40.

import { GitBranch, GitCommitHorizontal } from 'lucide-react'

const SHORT_SHA = 7

export function ShortSha({ sha, className = '' }: { sha: string; className?: string }) {
  return (
    <span className={`font-mono shrink-0 ${className}`} title={sha}>
      {sha.slice(0, SHORT_SHA)}
    </span>
  )
}

export function BranchLabel({
  branch,
  icon = true,
  /** Cap in the flex row; the label truncates inside it. */
  max = '18rem',
  className = '',
}: {
  branch: string
  icon?: boolean
  max?: string
  className?: string
}) {
  return (
    <span className={`flex items-center gap-1 min-w-0 ${className}`} style={{ maxWidth: max }}>
      {icon && <GitBranch size={11} className="shrink-0" />}
      <span className="font-mono truncate" title={branch}>
        {branch}
      </span>
    </span>
  )
}

export { GitCommitHorizontal }
