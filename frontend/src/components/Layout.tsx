import { useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { Menu, X } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'

const nav = [
  { to: '/', label: 'Dashboard', end: true },
  { to: '/monitors', label: 'Monitors' },
  { to: '/reports', label: 'Reports' },
  { to: '/status-pages', label: 'Status Pages' },
]

function navClass({ isActive }: { isActive: boolean }) {
  return `rd-nav ${isActive ? 'active' : ''}`
}

// SidebarBody is shared by the persistent desktop sidebar and the mobile drawer.
function SidebarBody({ onNavigate }: { onNavigate?: () => void }) {
  const navigate = useNavigate()
  const { currentUser, logout } = useAuthContext()
  const username = currentUser?.username ?? 'User'
  const role = currentUser?.is_admin ? 'ADMIN' : 'MEMBER'

  const go = (path: string) => {
    onNavigate?.()
    navigate(path)
  }
  const handleLogout = () => {
    onNavigate?.()
    logout()
    navigate('/login')
  }

  return (
    <>
      {/* Logo */}
      <div className="mb-12">
        <h1 className="text-3xl font-black" style={{ color: 'var(--rd-text)' }}>
          SENTINEL
        </h1>
        <p className="mt-1 text-xs tracking-[0.3em]" style={{ color: 'var(--color-accent-primary)' }}>
          MONITOR
        </p>
      </div>

      {/* Nav */}
      <nav className="flex-1 space-y-2">
        {nav.map((item) => (
          <NavLink key={item.to} to={item.to} end={item.end} onClick={onNavigate} className={navClass}>
            {item.label}
          </NavLink>
        ))}
        {currentUser?.is_admin && (
          <NavLink to="/admin/users" onClick={onNavigate} className={navClass}>
            Users
          </NavLink>
        )}
      </nav>

      {/* User menu */}
      <div className="border-t pt-4" style={{ borderColor: 'var(--rd-border)' }}>
        <div
          className="flex w-full items-center gap-3 rounded-lg p-3"
          style={{ backgroundColor: 'var(--color-bg-card)' }}
        >
          <div
            className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-sm font-black"
            style={{ backgroundColor: 'var(--color-accent-online)', color: 'var(--color-bg-dark)' }}
          >
            {username.charAt(0).toUpperCase()}
          </div>
          <div className="min-w-0 flex-1 text-left">
            <p className="truncate text-sm font-bold" style={{ color: 'var(--rd-text)' }}>
              {username}
            </p>
            <p className="text-xs font-bold" style={{ color: 'var(--color-accent-primary)' }}>
              {role}
            </p>
          </div>
        </div>
        <div className="mt-3 space-y-1">
          <button
            className="w-full px-4 py-2 text-left text-xs font-bold transition-colors hover:text-white"
            style={{ color: 'var(--rd-text-muted)' }}
            onClick={() => go('/settings')}
          >
            SETTINGS
          </button>
          <button
            className="w-full px-4 py-2 text-left text-xs font-bold"
            style={{ color: 'var(--color-accent-offline)' }}
            onClick={handleLogout}
          >
            LOG OUT
          </button>
        </div>
      </div>
    </>
  )
}

export default function Layout() {
  const [drawerOpen, setDrawerOpen] = useState(false)

  return (
    <div className="flex min-h-screen" style={{ backgroundColor: 'var(--color-bg-dark)' }}>
      {/* Persistent desktop sidebar (background layer with aggressive right fade) */}
      <aside
        className="hidden w-64 shrink-0 flex-col p-6 md:flex"
        style={{
          backgroundColor: 'var(--color-bg-dark)',
          boxShadow: 'inset -100px 0 100px -40px rgba(0, 0, 0, 1)',
        }}
      >
        <SidebarBody />
      </aside>

      {/* Mobile slide-out drawer */}
      {drawerOpen && (
        <div className="fixed inset-0 z-50 md:hidden">
          <div className="absolute inset-0 bg-black/60" onClick={() => setDrawerOpen(false)} aria-hidden />
          <aside
            className="absolute inset-y-0 left-0 flex w-64 max-w-[80%] flex-col p-6 shadow-xl"
            style={{ backgroundColor: 'var(--color-bg-dark)' }}
          >
            <button
              onClick={() => setDrawerOpen(false)}
              className="absolute right-3 top-3 rounded-md p-1"
              style={{ color: 'var(--rd-text-muted)' }}
              aria-label="Close menu"
            >
              <X className="h-5 w-5" />
            </button>
            <SidebarBody onNavigate={() => setDrawerOpen(false)} />
          </aside>
        </div>
      )}

      {/* Main content — elevated rounded container floating on the outer area */}
      <div className="flex min-w-0 flex-1 flex-col" style={{ backgroundColor: 'var(--color-bg-main)' }}>
        {/* Mobile top bar (hamburger) */}
        <header
          className="flex h-14 items-center gap-2 px-4 md:hidden"
          style={{ backgroundColor: 'var(--color-bg-main)' }}
        >
          <button
            onClick={() => setDrawerOpen(true)}
            className="rounded-md p-1"
            style={{ color: 'var(--rd-text-muted)' }}
            aria-label="Open menu"
          >
            <Menu className="h-6 w-6" />
          </button>
          <span className="text-lg font-black" style={{ color: 'var(--rd-text)' }}>
            SENTINEL
          </span>
        </header>

        <main className="flex-1 overflow-hidden p-3 md:p-8">
          <div className="rd-container h-full overflow-auto p-5 md:p-8">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
