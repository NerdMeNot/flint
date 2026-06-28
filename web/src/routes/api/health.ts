import { createFileRoute } from '@tanstack/react-router'

// Same-origin backend-reachability probe. The browser can't hit the Go API
// directly (different origin), so the reconnect banner and the root error
// boundary poll /api/health here and this route forwards to the Go server's
// public /health/ready. 204 = backend up, 503 = unreachable. No auth.
const BACKEND_URL = process.env.FLINT_BACKEND_URL || 'http://localhost:5000'

export const Route = createFileRoute('/api/health')({
  server: {
    handlers: {
      GET: async () => {
        try {
          const res = await fetch(`${BACKEND_URL}/health/ready`, {
            signal: AbortSignal.timeout(2000),
          })
          return new Response(null, { status: res.ok ? 204 : 503 })
        } catch {
          return new Response(null, { status: 503 })
        }
      },
    },
  },
})
