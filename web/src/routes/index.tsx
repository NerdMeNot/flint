import { createFileRoute, redirect } from '@tanstack/react-router'
import { client } from '#/lib/orpc'

// The root path is not a page of its own — it lands the user on the first
// enabled product section. Capabilities are deploy-time config, so this is
// resolved on every visit from GET /api/v1/capabilities.
const SECTION_PATH = {
  ci: '/ci',
  workflows: '/workflows',
  loadtest: '/loadtest',
} as const

type SectionPath = (typeof SECTION_PATH)[keyof typeof SECTION_PATH]

export const Route = createFileRoute('/')({
  beforeLoad: async () => {
    let target: SectionPath = '/ci'
    try {
      const caps = await client.capabilities.get()
      const first = caps.products.find((p) => p.enabled)
      const path = first && SECTION_PATH[first.id as keyof typeof SECTION_PATH]
      if (path) target = path
    } catch {
      // Capabilities unreachable — fall back to CI, the default section.
    }
    throw redirect({ to: target })
  },
})
