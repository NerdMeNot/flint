import { createFileRoute } from '@tanstack/react-router'
import { RunnerPoolEditor } from '#/components/runners/RunnerPoolEditor'

export const Route = createFileRoute('/settings/runners/new')({
  component: () => <RunnerPoolEditor />,
})
