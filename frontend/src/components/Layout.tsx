import { useState } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import {
  LayoutDashboard,
  Activity,
  BarChart3,
  Globe,
  Moon,
  Sun,
  ShieldCheck,
  Menu,
  X,
  Users,
} from 'lucide-react'
import { useTheme } from '@/context/ThemeContext'
import { useAuthContext } from '@/context/AuthContext'
import UserMenu from '@/components/UserMenu'

// Settings (and its Notifications tab) are reached via the user menu, not the
// sidebar, so the sidebar lists only the primary product areas.
const nav = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard, end: true },
  { to: '/monitors', label: 'Monitors', icon: Activity },
  { to: '/reports', label: 'Reports', icon: BarChart3 },
  { to: '/status-pages', label: 'Status Pages', icon: Globe },
]

// SidebarBody is shared by the persistent desktop sidebar and the mobile drawer.
// onNavigate lets the mobile drawer close itself when a link is tapped.
function SidebarBody({ onNavigate }: { onNavigate?: () => void }) {
  const { currentUser } = useAuthContext()
  const linkClass = ({ isActive }: { isActive: boolean }) =>
    `flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors ${
      isActive
        ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
        : 'text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800'
    }`
  return (
    <>
      <div className="mb-8 flex items-center gap-2 px-2">
        <ShieldCheck className="h-7 w-7 text-primary-600" />
        <span className="text-lg font-bold">Sentinel</span>
      </div>
      <nav className="flex flex-col gap-1">
        {nav.map(({ to, label, icon: Icon, end }) => (
          <NavLink key={to} to={to} end={end} onClick={onNavigate} className={linkClass}>
            <Icon className="h-4 w-4" />
            {label}
          </NavLink>
        ))}
        {currentUser?.is_admin && (
          <>
            <div className="my-2 border-t border-neutral-200 dark:border-neutral-800" />
            <NavLink to="/admin/users" onClick={onNavigate} className={linkClass}>
              <Users className="h-4 w-4" />
              Users
            </NavLink>
          </>
        )}
      </nav>
      <div className="mt-auto pt-4">
        <UserMenu />
      </div>
    </>
  )
}

export default function Layout() {
  const { isDark, toggle } = useTheme()
  const [drawerOpen, setDrawerOpen] = useState(false)

  return (
    <div className="flex min-h-screen">
      {/* Persistent desktop sidebar */}
      <aside className="hidden w-60 shrink-0 flex-col border-r border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-900 md:flex">
        <SidebarBody />
      </aside>

      {/* Mobile slide-out drawer */}
      {drawerOpen && (
        <div className="fixed inset-0 z-50 md:hidden">
          <div className="absolute inset-0 bg-black/50" onClick={() => setDrawerOpen(false)} aria-hidden />
          <aside className="absolute inset-y-0 left-0 flex w-64 max-w-[80%] flex-col border-r border-neutral-200 bg-white p-4 shadow-xl dark:border-neutral-800 dark:bg-neutral-900">
            <button
              onClick={() => setDrawerOpen(false)}
              className="absolute right-3 top-3 rounded-md p-1 text-neutral-500 hover:bg-neutral-100 dark:hover:bg-neutral-800"
              aria-label="Close menu"
            >
              <X className="h-5 w-5" />
            </button>
            <SidebarBody onNavigate={() => setDrawerOpen(false)} />
          </aside>
        </div>
      )}

      {/* Main */}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center justify-between gap-3 border-b border-neutral-200 bg-white px-4 dark:border-neutral-800 dark:bg-neutral-900 md:px-6">
          <div className="flex items-center gap-2">
            <button
              onClick={() => setDrawerOpen(true)}
              className="btn-secondary !px-2 md:hidden"
              aria-label="Open menu"
            >
              <Menu className="h-5 w-5" />
            </button>
            <span className="font-semibold md:hidden">Sentinel</span>
          </div>
          <button onClick={toggle} className="btn-secondary !px-2" aria-label="Toggle theme">
            {isDark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          </button>
        </header>
        <main className="flex-1 overflow-y-auto p-4 md:p-6">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
