import { useState, useEffect, useRef } from 'react'
import {
  Outlet,
  HeadContent,
  Scripts,
  createRootRoute,
  redirect,
  useRouterState,
  useRouter,
} from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import { getAccessToken, clearSession, refreshSession } from '#/lib/auth-token'
import { Sidebar, SidebarProvider, MobileMenuButton, useSidebar } from '#/components/Sidebar'
import { ScopeSelector } from '#/components/ScopeSelector'
import { ScopeProvider } from '#/lib/scope-context'
import { ScopeChips } from '#/components/ScopeChips'
import { AppearanceSync } from '#/components/AppearanceSync'
import { DemoModeBanner } from '#/components/DemoModeBanner'
import { BackendStatusBanner } from '#/components/BackendStatusBanner'
import { CommandPalette, useCommandPalette } from '#/components/CommandPalette'
import { TanStackDevtools } from '@tanstack/react-devtools'
import { TanStackRouterDevtoolsPanel } from '@tanstack/react-router-devtools'
import { Search } from 'lucide-react'
import appCss from '../styles.css?url'
import { FlintMark } from '#/components/FlintMark'

const THEME_INIT_SCRIPT = `(function(){try{var stored=window.localStorage.getItem('theme');var mode=(stored==='light'||stored==='dark'||stored==='auto')?stored:'auto';var prefersDark=window.matchMedia('(prefers-color-scheme: dark)').matches;var resolved=mode==='auto'?(prefersDark?'dark':'light'):mode;if(!stored){resolved='dark'}var root=document.documentElement;root.classList.remove('light','dark');root.classList.add(resolved);if(mode==='auto'){root.removeAttribute('data-theme')}else{root.setAttribute('data-theme',mode)}root.style.colorScheme=resolved;}catch(e){}})();`

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'Flint CI' },
    ],
    links: [
      { rel: 'stylesheet', href: appCss },
      // SVG first: browsers that support it get a crisp mark at any DPI, and it
      // carries its own prefers-color-scheme rule so the tab icon tracks the OS
      // theme. The .ico is the fallback and holds size-specific artwork — the
      // 16px frame is drawn with thicker strokes, because the true geometry has
      // tapers that sub-pixel away at that scale.
      { rel: 'icon', type: 'image/svg+xml', href: '/favicon.svg' },
      { rel: 'icon', type: 'image/png', sizes: '32x32', href: '/favicon-32.png' },
      { rel: 'icon', type: 'image/png', sizes: '16x16', href: '/favicon-16.png' },
      { rel: 'alternate icon', href: '/favicon.ico' },
      { rel: 'apple-touch-icon', sizes: '180x180', href: '/apple-touch-icon.png' },
      { rel: 'manifest', href: '/manifest.json' },
    ],
  }),
  component: RootLayout,
  shellComponent: RootShell,
  errorComponent: RootError,
  // Client-side auth guard: an unauthenticated visit to any page (other than the
  // login screen) goes straight to /login rather than rendering a page whose
  // backend calls would 401. Runs only in the browser — the session token lives
  // in localStorage, which SSR can't read; SSR's own auth comes from the cookie
  // (see backend.ts), and a server-side 401 is caught by RootError below.
  beforeLoad: ({ location }) => {
    if (typeof window === 'undefined') return
    if (location.pathname === '/login') return
    if (!getAccessToken()) {
      throw redirect({ to: '/login' })
    }
  },
})

// RootError catches anything an underlying route throws — most commonly a failed
// call to the Flint backend. Without it a backend blip white-screens the app
// ("wasn't caught by any route"); here it shows a clear message and a retry.
function RootError({ error, reset }: { error: Error; reset: () => void }) {
  const router = useRouter()
  const queryClient = useQueryClient()
  const msg = error?.message ?? String(error)
  const unreachable =
    /fetch failed|Failed to fetch|ECONNREFUSED|NetworkError|Backend 5\d\d/i.test(msg)
  // A 401 means the session is missing or expired — send the user to login
  // rather than showing a raw error envelope.
  const unauthorized = /Backend 401|UNAUTHORIZED|Unauthorized/i.test(msg)

  // A 401 is usually just the 15-minute access token expiring, so try to refresh
  // and recover in place before destroying the session.
  //
  // The guard ref is load-bearing. This boundary re-renders on every router
  // update, and a page holds many queries that all 401 within the same instant;
  // the previous version ran clearSession + navigate on each of those renders.
  // Navigating re-rendered the boundary, which fired the effect again — a spin
  // that allocated router state, error objects and query entries without bound.
  // That is what took a tab to 8 GB and pinned a core. Recovery must run once.
  const recovering = useRef(false)
  useEffect(() => {
    if (!unauthorized || recovering.current) return
    recovering.current = true
    void (async () => {
      if (await refreshSession()) {
        await queryClient.resetQueries()
        reset()
        router.invalidate()
      } else {
        clearSession()
        router.navigate({ to: '/login' })
      }
      recovering.current = false
    })()
  }, [unauthorized, router, queryClient, reset])

  // Backend unreachable (initial load failed after the queries' own retries):
  // poll the same-origin health probe and recover automatically when the API is
  // back — no manual refresh. This keeps a startup race or a blip from being a
  // dead-end error screen.
  useEffect(() => {
    if (!unreachable) return
    let cancelled = false
    const id = setInterval(async () => {
      try {
        const res = await fetch('/api/health')
        if (!res.ok || cancelled) return
        clearInterval(id)
        // Clear the CACHED query errors (not just the router state), otherwise
        // re-rendering re-reads the failed results and throws straight back here.
        await queryClient.resetQueries()
        reset()
        router.invalidate()
      } catch {
        // still down — keep polling
      }
    }, 2_000)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [unreachable, reset, router, queryClient])

  if (unauthorized) {
    return (
      <div className="min-h-screen flex items-center justify-center p-6">
        <p className="text-sm text-muted-foreground">Redirecting to sign in…</p>
      </div>
    )
  }

  return (
    <div className="min-h-screen flex items-center justify-center p-6">
      <div className="max-w-md w-full text-center space-y-4">
        <div className="text-3xl">{unreachable ? '🔌' : '⚠️'}</div>
        <h1 className="display-title text-xl font-bold text-foreground">
          {unreachable ? 'Reconnecting to Flint…' : 'Something went wrong'}
        </h1>
        <p className="text-sm text-muted-foreground">
          {unreachable
            ? "The API isn't responding yet — this page will recover on its own as soon as it's reachable."
            : msg}
        </p>
        <button
          type="button"
          onClick={() => {
            reset()
            router.invalidate()
          }}
          className="px-4 py-2 text-sm font-medium rounded-lg border border-border hover:bg-accent transition-colors text-foreground"
        >
          Retry now
        </button>
      </div>
    </div>
  )
}

function RootShell({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
        <HeadContent />
      </head>
      <body className="font-sans antialiased [overflow-wrap:anywhere]">
        {children}
        {/* The @tanstack/devtools-vite plugin strips this from production builds. */}
        <TanStackDevtools
          config={{ position: 'bottom-right' }}
          plugins={[
            { name: 'TanStack Router', render: <TanStackRouterDevtoolsPanel /> },
          ]}
        />
        <Scripts />
      </body>
    </html>
  )
}

function RootLayout() {
  const path = useRouterState({ select: (s) => s.location.pathname })
  const isAdmin = path.startsWith('/settings')
  const isProfile = path === '/profile' || path.startsWith('/profile/')
  const isAuth = path === '/login'
  const palette = useCommandPalette()

  // Auth pages render without any shell (no sidebar, no header).
  if (isAuth) {
    return <Outlet />
  }

  // Admin and the personal account area both use a focused shell (no product
  // sidebar) — each route supplies its own left sub-nav.
  if (isAdmin || isProfile) {
    return (
      <>
        <AppearanceSync />
        <FocusedShell label={isAdmin ? 'Admin' : 'Account'} onSearchClick={() => palette.setOpen(true)} />
        <CommandPalette open={palette.open} onClose={() => palette.setOpen(false)} />
      </>
    )
  }

  return (
    <ScopeProvider>
      <SidebarProvider>
        <AppearanceSync />
        <Sidebar />
        <MainContent onSearchClick={() => palette.setOpen(true)} />
        <CommandPalette open={palette.open} onClose={() => palette.setOpen(false)} />
      </SidebarProvider>
    </ScopeProvider>
  )
}

// ---------------------------------------------------------------------------
// Main app shell (sidebar + scope filters)
// ---------------------------------------------------------------------------

function MainContent({ onSearchClick }: { onSearchClick: () => void }) {
  const { collapsed } = useSidebar()

  return (
    <div className={`min-h-screen transition-all duration-200 ease-out ${
      collapsed ? 'lg:ml-[64px]' : 'lg:ml-[240px]'
    }`}>
      <DemoModeBanner />
      <BackendStatusBanner />
      <header
        className="sticky top-0 z-20 flex h-14 lg:h-16 items-center gap-3 border-b border-border px-4 sm:px-6 lg:px-8"
        style={{ background: 'var(--surface)', backdropFilter: 'blur(12px)' }}
      >
        <MobileMenuButton />
        {/* Mobile brand */}
        <div className="lg:hidden flex items-center gap-2 shrink-0">
          <FlintMark size={24} className="text-primary shrink-0" />
          <span className="display-title font-bold text-foreground text-base tracking-tight">
            Flint
          </span>
        </div>
        {/* Scope context — left side */}
        <div className="hidden sm:block min-w-0">
          <ScopeSelector />
        </div>
        {/* Actions — right side */}
        <div className="ml-auto flex items-center gap-2 sm:gap-3 shrink-0">
          <SearchTrigger onClick={onSearchClick} />
        </div>
      </header>
      <ScopeChips />
      <main className="mx-auto w-full max-w-[1440px] px-4 py-4 sm:px-6 sm:py-6 lg:px-10 lg:py-8 xl:px-12">
        <Outlet />
      </main>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Focused shell (no product sidebar) — used by Admin and the Account area. The
// `label` distinguishes the two; each route renders its own left sub-nav.
// ---------------------------------------------------------------------------

function FocusedShell({ onSearchClick, label }: { onSearchClick: () => void; label: string }) {
  return (
    <div className="min-h-screen">
      <DemoModeBanner />
      <BackendStatusBanner />
      <header
        className="sticky top-0 z-20 flex h-14 lg:h-16 items-center gap-3 border-b border-border px-4 sm:px-6 lg:px-8"
        style={{ background: 'var(--surface)', backdropFilter: 'blur(12px)' }}
      >
        <div className="flex items-center gap-2">
          <FlintMark size={26} className="text-primary shrink-0" />
          <span className="display-title font-bold text-foreground text-base lg:text-lg tracking-tight">
            Flint
          </span>
          <span className="text-xs font-medium text-muted-foreground opacity-60 ml-1">{label}</span>
        </div>
        <div className="ml-auto flex items-center gap-2 sm:gap-3">
          <SearchTrigger onClick={onSearchClick} />
        </div>
      </header>
      <main className="mx-auto w-full max-w-[1440px] px-4 py-4 sm:px-6 sm:py-6 lg:px-10 lg:py-8 xl:px-12">
        <Outlet />
      </main>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Search trigger button (shown in header)
// ---------------------------------------------------------------------------

function SearchTrigger({ onClick }: { onClick: () => void }) {
  // Render the platform-neutral 'Ctrl' on the server and first client render so
  // SSR markup matches; swap to \u2318 on Mac only after mount to avoid a hydration
  // mismatch.
  const [isMac, setIsMac] = useState(false)
  useEffect(() => {
    setIsMac(/Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent))
  }, [])

  return (
    <>
      {/* Mobile: icon only */}
      <button
        type="button"
        onClick={onClick}
        className="sm:hidden flex items-center justify-center w-8 h-8 rounded-lg text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
        aria-label="Search"
      >
        <Search size={16} />
      </button>
      {/* Desktop: full trigger */}
      <button
        type="button"
        onClick={onClick}
        className="hidden sm:flex items-center gap-2 px-3 py-1.5 text-xs text-muted-foreground/60 border border-border rounded-lg hover:text-muted-foreground hover:border-border hover:bg-accent transition-colors"
      >
        <Search size={13} />
        <span>Search</span>
        <kbd className="ml-1 px-1.5 py-0.5 text-[11px] font-medium border border-border rounded bg-transparent">
          {isMac ? '\u2318' : 'Ctrl'}K
        </kbd>
      </button>
    </>
  )
}
