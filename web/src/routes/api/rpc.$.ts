import { RPCHandler } from '@orpc/server/fetch'
import { createFileRoute } from '@tanstack/react-router'
import { appRouter } from '#/lib/api/router'

const handler = new RPCHandler(appRouter)

export const Route = createFileRoute('/api/rpc/$')({
  server: {
    handlers: {
      ANY: async ({ request }) => {
        const { response } = await handler.handle(request, {
          prefix: '/api/rpc',
          context: {},
        })

        return response ?? new Response('Not Found', { status: 404 })
      },
    },
  },
})
