import { createFileRoute } from '@tanstack/react-router'
import { Boxes } from 'lucide-react'

export const Route = createFileRoute('/workspaces')({
  component: () => (
    <div className="rise-in space-y-4 max-w-4xl">
      <h1 className="display-title text-2xl text-foreground">Workspaces</h1>
      <p className="text-muted-foreground text-sm">Organize projects into workspaces for RBAC scoping.</p>
      <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
        <Boxes size={32} strokeWidth={1.2} />
        <span className="text-sm">No workspaces configured yet.</span>
      </div>
    </div>
  ),
})
