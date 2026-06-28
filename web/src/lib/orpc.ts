import { createORPCClient } from '@orpc/client'
import { RPCLink } from '@orpc/client/fetch'
import { createRouterClient, type RouterClient } from '@orpc/server'
import { createIsomorphicFn } from '@tanstack/react-start'
import { createTanstackQueryUtils } from '@orpc/tanstack-query'
import type { AppRouter } from '#/lib/api/router'
import { appRouter } from '#/lib/api/router'
import { getAccessToken } from '#/lib/auth-token'

const getClient = createIsomorphicFn()
  .client(
    (): RouterClient<AppRouter> =>
      createORPCClient(
        new RPCLink({
          url: `${window.location.origin}/api/rpc`,
          // Forward the session token so the /api/rpc route relays it to Go.
          headers: () => {
            const t = getAccessToken()
            return t ? { authorization: `Bearer ${t}` } : {}
          },
        }),
      ),
  )
  .server(
    (): RouterClient<AppRouter> =>
      createRouterClient(appRouter, { context: {} }),
  )

export const client = getClient()
export const orpc = createTanstackQueryUtils(client)
