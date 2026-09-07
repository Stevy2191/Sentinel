import { useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { Menu, X, RefreshCw, Settings } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'

const nav = [
  { to: '/', label: 'Dashboard', end: true },
  { to: '/monitors', label: 'Monitors' },
  { to: '/ssl', label: 'SSL & Domains' },
  { to: '/server-monitoring', label: 'Instance Monitoring' },
  { to: '/status-pages', label: 'Status Pages' },
  { to: '/reports', label: 'Reports' },
]

function navClass({ isActive }: { isActive: boolean }) {
  return `rd-nav ${isActive ? 'active' : ''}`
}

/** "Good morning" / "Good afternoon" / "Good evening" for the top bar. */
function greeting(): string {
  const h = new Date().getHours()
  if (h < 12) return 'Good morning'
  if (h < 18) return 'Good afternoon'
  return 'Good evening'
}

// SidebarBody is shared by the persistent desktop sidebar and the mobile drawer.
function SidebarBody({ onNavigate }: { onNavigate?: () => void }) {
  const navigate = useNavigate()
  const { currentUser, logout } = useAuthContext()
  const username = currentUser?.username ?? 'User'
  const role = currentUser?.is_admin ? 'Admin' : 'Member'

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
      {/* Wordmark */}
      <div className="border-b border-white/10 p-6">
        <div className="text-xl font-light tracking-wide text-white">Sentinel</div>
        <div className="mt-2 text-xs text-slate-400">Uptime Monitor</div>
      </div>

      {/* Nav */}
      <nav className="flex-1 space-y-1 overflow-y-auto p-4">
        {nav.map((item) => (
          <NavLink key={item.to} to={item.to} end={item.end} onClick={onNavigate} className={navClass}>
            {item.label}
          </NavLink>
        ))}
      </nav>

      {/* User footer */}
      <div className="border-t border-white/10 p-4">
        <div className="flex w-full items-center gap-3 rounded-lg p-3">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-emerald-500 text-sm font-semibold text-slate-900">
            {username.charAt(0).toUpperCase()}
          </div>
          <div className="min-w-0 flex-1 text-left">
            <div className="truncate text-sm font-medium text-white">{username}</div>
            <div className="text-xs text-slate-500">{role}</div>
          </div>
        </div>
        <div className="mt-1 space-y-0.5">
          {currentUser?.is_admin && (
            <button
              className="w-full rounded-lg px-4 py-2 text-left text-xs text-slate-400 transition hover:bg-white/5 hover:text-slate-300"
              onClick={() => go('/admin/users')}
            >
              Users
            </button>
          )}
          {/* Not admin-gated: this page holds password change and 2FA for every
              user. The registration toggle inside it is admin-only on its own. */}
          <button
            className="w-full rounded-lg px-4 py-2 text-left text-xs text-slate-400 transition hover:bg-white/5 hover:text-slate-300"
            onClick={() => go('/settings/security')}
          >
            Security
          </button>
          <button
            className="w-full rounded-lg px-4 py-2 text-left text-xs text-slate-400 transition hover:bg-white/5 hover:text-slate-300"
            onClick={() => go('/settings')}
          >
            Settings
          </button>
          <button
            className="w-full rounded-lg px-4 py-2 text-left text-xs text-red-400 transition hover:bg-red-500/10 hover:text-red-300"
            onClick={handleLogout}
          >
            Log out
          </button>
        </div>
      </div>
    </>
  )
}

export default function Layout() {
  const [drawerOpen, setDrawerOpen] = useState(false)
  const navigate = useNavigate()

  return (
    // The gradient ground lives on <body> (index.css) so it also backs the
    // overscroll area; this wrapper only establishes the sidebar offset.
    <div className="min-h-screen">
      {/* Persistent desktop sidebar — fixed, so page content scrolls under it. */}
      <aside className="fixed left-0 top-0 z-40 hidden h-screen w-56 flex-col border-r border-white/10 bg-slate-900/50 backdrop-blur-xl md:flex">
        <SidebarBody />
      </aside>

      {/* Mobile slide-out drawer */}
      {drawerOpen && (
        <div className="fixed inset-0 z-50 md:hidden">
          <div className="absolute inset-0 bg-black/60" onClick={() => setDrawerOpen(false)} aria-hidden />
          <aside className="absolute inset-y-0 left-0 flex w-56 max-w-[80%] flex-col border-r border-white/10 bg-slate-900 shadow-xl">
            <button
              onClick={() => setDrawerOpen(false)}
              className="absolute right-3 top-3 rounded-md p-1 text-slate-400 transition hover:text-white"
              aria-label="Close menu"
            >
              <X className="h-5 w-5" />
            </button>
            <SidebarBody onNavigate={() => setDrawerOpen(false)} />
          </aside>
        </div>
      )}

      <div className="p-4 md:ml-56 md:p-8">
        {/* Top bar: greeting on the left, refresh and settings on the right. */}
        <div className="mb-8 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <button
              onClick={() => setDrawerOpen(true)}
              className="rounded-md p-1 text-slate-400 transition hover:text-white md:hidden"
              aria-label="Open menu"
            >
              <Menu className="h-6 w-6" />
            </button>
            <div className="text-sm text-slate-500">{greeting()}</div>
          </div>
          <div className="flex items-center gap-4">
            <button
              className="text-slate-400 transition hover:text-slate-300"
              onClick={() => window.location.reload()}
              aria-label="Refresh"
            >
              <RefreshCw className="h-5 w-5" />
            </button>
            <button
              className="text-slate-400 transition hover:text-slate-300"
              onClick={() => navigate('/settings')}
              aria-label="Settings"
            >
              <Settings className="h-5 w-5" />
            </button>
          </div>
        </div>

        <div className="max-w-7xl">
          <Outlet />
        </div>
      </div>
    </div>
  )
}
