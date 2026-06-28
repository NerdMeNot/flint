import { createFileRoute } from '@tanstack/react-router'

// Same-origin proxy for the Go API's public auth endpoints (/auth/login,
// /auth/mfa/verify, /auth/change-password, OIDC callbacks, …). The browser can't
// reach the Go API directly (different origin), so login.tsx posts to /auth/* on
// its own origin and this server route forwards to the Go API verbatim — method,
// body, and relevant response headers (Set-Cookie / Location for SSO redirects).
// Mirrors the SSE proxy in api/sse.$.ts.
const BACKEND_URL = process.env.FLINT_BACKEND_URL || 'http://localhost:5000'

export const Route = createFileRoute('/auth/$')({
  server: {
    handlers: {
      ANY: async ({ request }) => {
        const url = new URL(request.url)
        const target = `${BACKEND_URL}${url.pathname}${url.search}`

        const headers = new Headers(request.headers)
        headers.delete('host')

        const hasBody = request.method !== 'GET' && request.method !== 'HEAD'

        let upstream: Response
        try {
          upstream = await fetch(target, {
            method: request.method,
            headers,
            body: hasBody ? await request.arrayBuffer() : undefined,
            signal: request.signal,
            redirect: 'manual',
          })
        } catch {
          return new Response(JSON.stringify({ error: 'upstream unavailable' }), {
            status: 502,
            headers: { 'Content-Type': 'application/json' },
          })
        }

        // Forward status + body and the headers that carry auth semantics.
        const respHeaders = new Headers()
        for (const h of ['content-type', 'set-cookie', 'location']) {
          const v = upstream.headers.get(h)
          if (v) respHeaders.set(h, v)
        }
        return new Response(upstream.body, { status: upstream.status, headers: respHeaders })
      },
    },
  },
})
