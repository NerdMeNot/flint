import { createRouter as createTanStackRouter } from '@tanstack/react-router'
import { setupRouterSsrQueryIntegration } from '@tanstack/react-router-ssr-query'
import { QueryClient } from '@tanstack/react-query'
import { routeTree } from './routeTree.gen'
import { RoutePending } from './components/RoutePending'

export function getRouter() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        // Resilience: a backend blip should self-heal, not hard-fail. Retry only
        // on the CLIENT (so SSR fails fast to the error boundary's reconnecting
        // fallback instead of hanging on retries), with capped exponential
        // backoff, and refetch when the network/tab comes back.
        retry: (failureCount) => typeof window !== 'undefined' && failureCount < 5,
        retryDelay: (attempt) => Math.min(1000 * 2 ** attempt, 15_000),
        refetchOnReconnect: true,
      },
    },
  })

  const router = createTanStackRouter({
    routeTree,
    scrollRestoration: true,
    // Preload on intent (hover/touch); let React Query own data caching
    // (defaultPreloadStaleTime: 0 defers freshness to the query client).
    defaultPreload: 'intent',
    defaultPreloadStaleTime: 0,
    // Skeleton shown while a route's data is loading (client navigations and the
    // first client render of SSR-deferred data).
    defaultPendingComponent: RoutePending,
    context: { queryClient },
  })

  setupRouterSsrQueryIntegration({ router, queryClient })

  return router
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof getRouter>
  }
}
