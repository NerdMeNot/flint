import { createFileRoute } from '@tanstack/react-router'
import { Settings } from 'lucide-react'

export const Route = createFileRoute('/settings')({
  component: () => (
    <div className="rise-in space-y-4 max-w-4xl">
      <h1 className="display-title text-2xl text-foreground">Settings</h1>
      <p className="text-muted-foreground text-sm">Platform configuration.</p>
      <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
        <Settings size={32} strokeWidth={1.2} />
        <span className="text-sm">Settings page coming soon.</span>
      </div>
    </div>
  ),
})
