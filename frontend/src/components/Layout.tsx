import { NavLink, Outlet } from 'react-router-dom'
import {
  LayoutDashboard,
  Activity,
  BarChart3,
  Globe,
  Moon,
  Sun,
  ShieldCheck,
} from 'lucide-react'
import { useTheme } from '@/context/ThemeContext'
import UserMenu from '@/components/UserMenu'

// Settings (and its Notifications tab) are reached via the user menu, not the
// sidebar, so the sidebar lists only the primary product areas.
const nav = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard, end: true },
  { to: '/monitors', label: 'Monitors', icon: Activity },
  { to: '/reports', label: 'Reports', icon: BarChart3 },
  { to: '/status-pages', label: 'Status Pages', icon: Globe },
]

export default function Layout() {
  const { isDark, toggle } = useTheme()

  return (
    <div className="flex min-h-screen">
      {/* Sidebar */}
      <aside className="hidden w-60 shrink-0 flex-col border-r border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-900 md:flex">
        <div className="mb-8 flex items-center gap-2 px-2">
          <ShieldCheck className="h-7 w-7 text-primary-600" />
          <span className="text-lg font-bold">Sentinel</span>
        </div>
        <nav className="flex flex-col gap-1">
          {nav.map(({ to, label, icon: Icon, end }) => (
            <NavLink
              key={to}
              to={to}
              end={end}
              className={({ isActive }) =>
                `flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                  isActive
                    ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
                    : 'text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800'
                }`
              }
            >
              <Icon className="h-4 w-4" />
              {label}
            </NavLink>
          ))}
        </nav>

        {/* User profile menu, pinned to the bottom. */}
        <div className="mt-auto pt-4">
          <UserMenu />
        </div>
      </aside>

      {/* Main */}
      <div className="flex flex-1 flex-col">
        <header className="flex h-14 items-center justify-between border-b border-neutral-200 bg-white px-6 dark:border-neutral-800 dark:bg-neutral-900">
          <div className="font-semibold md:hidden">Sentinel</div>
          <div className="flex flex-1 items-center justify-end gap-3">
            <button onClick={toggle} className="btn-secondary !px-2" aria-label="Toggle theme">
              {isDark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
            </button>
          </div>
        </header>
        <main className="flex-1 overflow-y-auto p-6">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
