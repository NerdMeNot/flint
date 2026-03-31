import { createFileRoute } from '@tanstack/react-router'
import { Shield } from 'lucide-react'

export const Route = createFileRoute('/gates')({
  component: () => (
    <div className="rise-in space-y-4 max-w-4xl">
      <h1 className="display-title text-2xl text-foreground">Gates</h1>
      <p className="text-muted-foreground text-sm">No pending approvals.</p>
      <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
        <Shield size={32} strokeWidth={1.2} />
        <span className="text-sm">Gate approvals will appear here when pipelines reach deployment gates.</span>
      </div>
    </div>
  ),
})
