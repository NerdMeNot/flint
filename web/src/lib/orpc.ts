import { createORPCClient } from '@orpc/client'
import { RPCLink } from '@orpc/client/fetch'
import { createIsomorphicFn } from '@tanstack/react-start'
import { createTanstackQueryUtils } from '@orpc/tanstack-query'

const getLink = createIsomorphicFn()
  .client(
    () =>
      new RPCLink({
        url: `${window.location.origin}/api/rpc`,
      }),
  )
  .server(
    () =>
      new RPCLink({
        url: `http://localhost:8080/api/rpc`,
      }),
  )

export const client = createORPCClient(getLink())
export const orpc = createTanstackQueryUtils(client)
