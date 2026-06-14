import { RPCHandler } from '@orpc/server/fetch'
import { createFileRoute } from '@tanstack/react-router'
import { appRouter } from '#/lib/api/router'
import { authTokenALS } from '#/lib/api/server-token'

const handler = new RPCHandler(appRouter)

export const Route = createFileRoute('/api/rpc/$')({
  server: {
    handlers: {
      ANY: async ({ request }) => {
        // Carry the browser's Authorization through the handler so backend.ts
        // forwards it to the Go API (the client sets it from localStorage).
        const auth = request.headers.get('authorization') ?? undefined
        return authTokenALS.run(auth, async () => {
          const { response } = await handler.handle(request, {
            prefix: '/api/rpc',
            context: {},
          })
          return response ?? new Response('Not Found', { status: 404 })
        })
      },
    },
  },
})
