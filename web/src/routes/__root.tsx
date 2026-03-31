import {
  Outlet,
  HeadContent,
  Scripts,
  createRootRoute,
} from '@tanstack/react-router'
import { Sidebar } from '#/components/Sidebar'
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
    <>
      <Sidebar />
      <div className="ml-[220px] min-h-screen">
        <header className="sticky top-0 z-20 flex h-14 items-center justify-end gap-3 border-b border-border px-6" style={{ background: 'var(--surface)', backdropFilter: 'blur(12px)' }}>
          <ThemeSwitcher />
          <ThemeToggle />
        </header>
        <main className="px-8 py-6">
          <Outlet />
        </main>
      </div>
    </>
  )
}
