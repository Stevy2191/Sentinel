import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard,
  Activity,
  BarChart3,
  Globe,
  Bell,
  Settings as SettingsIcon,
  Moon,
  Sun,
  ShieldCheck,
  LogOut,
} from 'lucide-react'
import { useTheme } from '@/context/ThemeContext'
import { useAuthContext } from '@/context/AuthContext'

const nav = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard, end: true },
  { to: '/monitors', label: 'Monitors', icon: Activity },
  { to: '/reports', label: 'Reports', icon: BarChart3 },
  { to: '/status-pages', label: 'Status Pages', icon: Globe },
  { to: '/notifications', label: 'Notifications', icon: Bell },
  { to: '/settings', label: 'Settings', icon: SettingsIcon },
]

export default function Layout() {
  const { isDark, toggle } = useTheme()
  const { currentUser, logout } = useAuthContext()
  const navigate = useNavigate()

  const handleLogout = () => {
    logout()
    navigate('/login')
  }

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
      </aside>

      {/* Main */}
      <div className="flex flex-1 flex-col">
        <header className="flex h-14 items-center justify-between border-b border-neutral-200 bg-white px-6 dark:border-neutral-800 dark:bg-neutral-900">
          <div className="font-semibold md:hidden">Sentinel</div>
          <div className="flex flex-1 items-center justify-end gap-3">
            {currentUser && (
              <span className="hidden text-sm text-neutral-500 dark:text-neutral-400 sm:inline">
                {currentUser.username}
              </span>
            )}
            <button
              onClick={toggle}
              className="btn-secondary !px-2"
              aria-label="Toggle theme"
            >
              {isDark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
            </button>
            <button onClick={handleLogout} className="btn-secondary !px-2" aria-label="Log out" title="Log out">
              <LogOut className="h-4 w-4" />
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
