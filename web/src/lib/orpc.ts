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
          fetch: async (input, init) => {
            const res = await fetch(input, init)
            if (res.status !== 401) return res
            if (!(await refreshSession())) return res
            const t = getAccessToken()
            const headers = new Headers(init?.headers)
            if (t) headers.set('authorization', `Bearer ${t}`)
            return fetch(input, { ...init, headers })
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
