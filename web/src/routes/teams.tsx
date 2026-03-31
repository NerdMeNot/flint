import { createFileRoute } from '@tanstack/react-router'
import { Users } from 'lucide-react'

export const Route = createFileRoute('/teams')({
  component: () => (
    <div className="rise-in space-y-4 max-w-4xl">
      <h1 className="display-title text-2xl text-foreground">Teams</h1>
      <p className="text-muted-foreground text-sm">Teams are synced from your IdP groups on login.</p>
      <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
        <Users size={32} strokeWidth={1.2} />
        <span className="text-sm">Teams will appear here after SSO is configured.</span>
      </div>
    </div>
  ),
})
