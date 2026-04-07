import { createFileRoute, Link, Outlet } from '@tanstack/react-router'

export const Route = createFileRoute('/settings/users')({
  component: () => <Outlet />,
})
