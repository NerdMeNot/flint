import { createFileRoute } from '@tanstack/react-router'
import { PoolEditor } from '#/components/runners/PoolEditor'

export const Route = createFileRoute('/settings/runners/new')({
  component: () => <PoolEditor />,
})
