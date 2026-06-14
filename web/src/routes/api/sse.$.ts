import { createFileRoute } from '@tanstack/react-router'

// Streaming proxy for Server-Sent Events. The browser can't reach the Go API
// directly (and EventSource can't set headers), so it opens
//   /api/sse/<path>?access_token=<jwt>
// on its own origin; this server route forwards to the Go API at
//   /api/v1/<path>
// with the token as a Bearer header, and streams the SSE body straight back.
// Used for run-state (/runs/:id/stream) and step-log streaming.
const BACKEND_URL = process.env.FLINT_BACKEND_URL || 'http://localhost:5000'

export const Route = createFileRoute('/api/sse/$')({
  server: {
    handlers: {
      GET: async ({ request }) => {
        const url = new URL(request.url)
        const rest = url.pathname.replace(/^\/api\/sse\//, '')
        const target = `${BACKEND_URL}/api/v1/${rest}${url.search}`

        const token = url.searchParams.get('access_token')
        let upstream: Response
        try {
          upstream = await fetch(target, {
            headers: token ? { Authorization: `Bearer ${token}` } : {},
            signal: request.signal,
          })
        } catch {
          return new Response('upstream unavailable', { status: 502 })
        }

        if (!upstream.ok || !upstream.body) {
          return new Response('upstream error', { status: upstream.status || 502 })
        }

        return new Response(upstream.body, {
          status: 200,
          headers: {
            'Content-Type': 'text/event-stream',
            'Cache-Control': 'no-cache',
            Connection: 'keep-alive',
            'X-Accel-Buffering': 'no',
          },
        })
      },
    },
  },
})
