import {
  Outlet,
  HeadContent,
  Scripts,
  createRootRoute,
} from '@tanstack/react-router'
import { Sidebar, SidebarProvider, MobileMenuButton, useSidebar } from '#/components/Sidebar'
import ThemeToggle from '#/components/ThemeToggle'
import { ThemeSwitcher } from '#/components/ThemeSwitcher'
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
  return (
    <SidebarProvider>
      <Sidebar />
      <MainContent />
    </SidebarProvider>
  )
}

function MainContent() {
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
        <div className="lg:hidden flex items-center gap-2">
          <div className="flex h-6 w-6 items-center justify-center rounded font-bold text-xs"
            style={{ background: 'linear-gradient(135deg, var(--ring), var(--success))', color: 'white', fontFamily: 'Fraunces, Georgia, serif' }}>
            F
          </div>
          <span className="display-title font-bold text-foreground text-sm tracking-tight">
            Flint
          </span>
        </div>
        <div className="ml-auto flex items-center gap-2">
          <ThemeSwitcher />
          <ThemeToggle />
        </div>
      </header>
      <main className="px-4 py-4 sm:px-6 sm:py-6 lg:px-8">
        <Outlet />
      </main>
    </div>
  )
}
