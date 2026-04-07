import { createFileRoute, Navigate } from '@tanstack/react-router'

export const Route = createFileRoute('/teams')({
  component: () => <Navigate to="/settings/teams" />,
})
