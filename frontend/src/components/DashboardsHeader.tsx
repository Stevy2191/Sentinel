import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ChevronDown, Plus } from 'lucide-react'

type Tab = 'dashboards' | 'status'

const TABS: { key: Tab; to: string; label: string }[] = [
  { key: 'dashboards', to: '/dashboards', label: 'Dashboards' },
  { key: 'status', to: '/status-pages', label: 'Status pages' },
]

/** DashboardsHeader is the shared top of the Dashboards and Status pages
 *  lists: one sidebar entry, two tabs, and one "+ New" that makes either. A
 *  new dashboard from the Status pages tab goes to /dashboards?new=1, which
 *  opens the new-dashboard dialog there. */
export default function DashboardsHeader({ active, onNewDashboard }: { active: Tab; onNewDashboard?: () => void }) {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)

  const newDashboard = () => {
    setOpen(false)
    if (onNewDashboard) onNewDashboard()
    else navigate('/dashboards?new=1')
  }
  const newStatusPage = () => {
    setOpen(false)
    navigate('/status-pages/create')
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-4xl font-light text-white">Dashboards</h1>
          <p className="mt-2 text-slate-400">
            Your own views, site views, screens for the wall, and public status pages.
          </p>
        </div>
        <div
          className="relative"
          onBlur={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(false)
          }}
          onKeyDown={(e) => {
            if (e.key === 'Escape') setOpen(false)
          }}
        >
          <button
            className="btn-primary flex items-center gap-2"
            aria-haspopup="menu"
            aria-expanded={open}
            onClick={() => setOpen((o) => !o)}
          >
            <Plus className="h-4 w-4" /> New <ChevronDown className="h-4 w-4" />
          </button>
          {open && (
            <div
              role="menu"
              className="absolute right-0 z-20 mt-2 w-48 overflow-hidden rounded-lg border border-white/10 bg-slate-900 shadow-xl"
            >
              <button
                role="menuitem"
                className="block w-full px-4 py-2 text-left text-sm text-slate-200 hover:bg-white/5"
                onClick={newDashboard}
              >
                Dashboard
              </button>
              <button
                role="menuitem"
                className="block w-full px-4 py-2 text-left text-sm text-slate-200 hover:bg-white/5"
                onClick={newStatusPage}
              >
                Status page
              </button>
            </div>
          )}
        </div>
      </div>
      <div className="flex flex-wrap gap-2">
        {TABS.map((t) => (
          <Link
            key={t.key}
            to={t.to}
            aria-current={active === t.key ? 'page' : undefined}
            className={`rounded-md px-4 py-2 text-sm font-medium ${
              active === t.key ? 'bg-primary-600 text-white' : 'bg-white/5 text-slate-300 hover:bg-white/10'
            }`}
          >
            {t.label}
          </Link>
        ))}
      </div>
    </div>
  )
}
