import {
  Outlet,
  HeadContent,
  Scripts,
  createRootRoute,
  useRouterState,
} from '@tanstack/react-router'
import { Sidebar, SidebarProvider, MobileMenuButton, useSidebar } from '#/components/Sidebar'
import { ScopeSelector } from '#/components/ScopeSelector'
import { ScopeProvider } from '#/lib/scope-context'
import ThemeToggle from '#/components/ThemeToggle'
import { ThemeSwitcher } from '#/components/ThemeSwitcher'
import { CommandPalette, useCommandPalette } from '#/components/CommandPalette'
import { Search } from 'lucide-react'
import appCss from '../styles.css?url'

const THEME_INIT_SCRIPT = `(function(){try{var stored=window.localStorage.getItem('theme');var mode=(stored==='light'||stored==='dark'||stored==='auto')?stored:'auto';var prefersDark=window.matchMedia('(prefers-color-scheme: dark)').matches;var resolved=mode==='auto'?(prefersDark?'dark':'light'):mode;if(!stored){resolved='dark'}var root=document.documentElement;root.classList.remove('light','dark');root.classList.add(resolved);if(mode==='auto'){root.removeAttribute('data-theme')}else{root.setAttribute('data-theme',mode)}root.style.colorScheme=resolved;}catch(e){}})();`

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'Flint CI' },
    ],
    links: [{ rel: 'stylesheet', href: appCss }],
  }),
  component: RootLayout,
  shellComponent: RootShell,
})

function RootShell({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
        <HeadContent />
      </head>
      <body className="font-sans antialiased [overflow-wrap:anywhere]">
        {children}
        <Scripts />
      </body>
    </html>
  )
}

function RootLayout() {
  const path = useRouterState({ select: (s) => s.location.pathname })
  const isAdmin = path.startsWith('/settings')
  const palette = useCommandPalette()

  if (isAdmin) {
    return (
      <>
        <AdminShell onSearchClick={() => palette.setOpen(true)} />
        <CommandPalette open={palette.open} onClose={() => palette.setOpen(false)} />
      </>
    )
  }

  return (
    <ScopeProvider>
      <SidebarProvider>
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
      collapsed ? 'lg:ml-[60px]' : 'lg:ml-[220px]'
    }`}>
      <header
        className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b border-border px-4 sm:px-6"
        style={{ background: 'var(--surface)', backdropFilter: 'blur(12px)' }}
      >
        <MobileMenuButton />
        {/* Mobile brand */}
        <div className="lg:hidden flex items-center gap-2 shrink-0">
          <div className="flex h-6 w-6 items-center justify-center rounded font-bold text-xs"
            style={{ background: 'linear-gradient(135deg, var(--ring), var(--success))', color: 'white', fontFamily: 'Fraunces, Georgia, serif' }}>
            F
          </div>
          <span className="display-title font-bold text-foreground text-sm tracking-tight">
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
          <div className="hidden sm:block w-px h-5 bg-border shrink-0" />
          <div className="hidden sm:block"><ThemeSwitcher /></div>
          <ThemeToggle />
        </div>
      </header>
      <main className="px-4 py-4 sm:px-6 sm:py-6 lg:px-8">
        <Outlet />
      </main>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Admin shell (clean, no sidebar, no scope filters)
// ---------------------------------------------------------------------------

function AdminShell({ onSearchClick }: { onSearchClick: () => void }) {
  return (
    <div className="min-h-screen">
      <header
        className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b border-border px-4 sm:px-6"
        style={{ background: 'var(--surface)', backdropFilter: 'blur(12px)' }}
      >
        <div className="flex items-center gap-2">
          <div className="flex h-6 w-6 items-center justify-center rounded font-bold text-xs"
            style={{ background: 'linear-gradient(135deg, var(--ring), var(--success))', color: 'white', fontFamily: 'Fraunces, Georgia, serif' }}>
            F
          </div>
          <span className="display-title font-bold text-foreground text-sm tracking-tight">
            Flint
          </span>
          <span className="text-xs font-medium text-muted-foreground opacity-60 ml-1">Admin</span>
        </div>
        <div className="ml-auto flex items-center gap-2 sm:gap-3">
          <SearchTrigger onClick={onSearchClick} />
          <div className="hidden sm:block"><ThemeSwitcher /></div>
          <ThemeToggle />
        </div>
      </header>
      <main className="px-4 py-4 sm:px-6 sm:py-6 lg:px-8">
        <Outlet />
      </main>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Search trigger button (shown in header)
// ---------------------------------------------------------------------------

function SearchTrigger({ onClick }: { onClick: () => void }) {
  return (
    <>
      {/* Mobile: icon only */}
      <button
        type="button"
        onClick={onClick}
        className="sm:hidden flex items-center justify-center w-8 h-8 rounded-lg text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-colors"
        aria-label="Search"
      >
        <Search size={16} />
      </button>
      {/* Desktop: full trigger */}
      <button
        type="button"
        onClick={onClick}
        className="hidden sm:flex items-center gap-2 px-3 py-1.5 text-xs text-muted-foreground/60 border border-border rounded-lg hover:text-muted-foreground hover:border-border/80 hover:bg-accent/30 transition-colors"
      >
        <Search size={13} />
        <span>Search</span>
        <kbd className="ml-1 px-1.5 py-0.5 text-[0.55rem] font-medium border border-border rounded bg-transparent">
          {typeof navigator !== 'undefined' && /Mac/.test(navigator.userAgent) ? '\u2318' : 'Ctrl'}K
        </kbd>
      </button>
    </>
  )
}
