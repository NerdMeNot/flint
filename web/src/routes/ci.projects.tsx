import { createFileRoute, Outlet } from '@tanstack/react-router'

export const Route = createFileRoute('/ci/projects')({
  component: () => <Outlet />,
})
