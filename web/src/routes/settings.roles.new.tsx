import { createFileRoute } from '@tanstack/react-router'
import { RoleEditor } from '#/components/roles/RoleEditor'

export const Route = createFileRoute('/settings/roles/new')({
  component: () => <RoleEditor />,
})
