import { createORPCClient, type RouterClient } from '@orpc/client'
import { RPCLink } from '@orpc/client/fetch'
import { createRouterClient } from '@orpc/server'
import { createIsomorphicFn } from '@tanstack/react-start'
import { createTanstackQueryUtils } from '@orpc/tanstack-query'
import type { AppRouter } from '#/lib/api/router'
import { appRouter } from '#/lib/api/router'

const getClient = createIsomorphicFn()
  .client(
    (): RouterClient<AppRouter> =>
      createORPCClient(
        new RPCLink({ url: `${window.location.origin}/api/rpc` }),
      ),
  )
  .server(
    (): RouterClient<AppRouter> =>
      createRouterClient(appRouter, { context: {} }),
  )

export const client = getClient()
export const orpc = createTanstackQueryUtils(client)
