import { createORPCClient } from '@orpc/client'
import { RPCLink } from '@orpc/client/fetch'
import { createRouterClient, type RouterClient } from '@orpc/server'
import { createIsomorphicFn } from '@tanstack/react-start'
import { createTanstackQueryUtils } from '@orpc/tanstack-query'
import type { AppRouter } from '#/lib/api/router'
import { appRouter } from '#/lib/api/router'
import { getAccessToken, refreshSession } from '#/lib/auth-token'

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
          // Spend the refresh token on the first 401 and replay the request once.
          // Recovering here means an expired access token never reaches the error
          // boundary, so the UI does not flicker through an error state — and,
          // more importantly, a background tab full of polling queries cannot
          // turn a routine 15-minute expiry into a retry storm.
          // oRPC calls this as fetch(request, init, …): the auth header lives on
          // the Request, and `init` carries only { redirect }. The retry copy has
          // to be cloned BEFORE the first send, because a Request body can only
          // be read once.
          fetch: async (request, init) => {
            const retry = request.clone()
            const res = await fetch(request, init)
            if (res.status !== 401) return res
            if (!(await refreshSession())) return res
            const t = getAccessToken()
            if (!t) return res
            retry.headers.set('authorization', `Bearer ${t}`)
            return fetch(retry, init)
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
