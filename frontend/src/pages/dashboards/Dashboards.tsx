import { useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { Globe2, LayoutDashboard, Loader2 } from 'lucide-react'
import DashboardsHeader from '@/components/DashboardsHeader'
import { useAuthContext } from '@/context/AuthContext'
import { useDashboards } from '@/hooks/useDashboards'
import NewDashboardModal from '@/components/dashboards/NewDashboardModal'
import type { Dashboard } from '@/types/dashboards'

function DashboardList({ title, items, empty }: { title: string; items: Dashboard[]; empty: string }) {
  return (
    <section className="space-y-2">
      <h2 className="text-sm font-semibold uppercase tracking-widest text-slate-500">{title}</h2>
      {items.length === 0 ? (
        <p className="text-sm text-slate-500">{empty}</p>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {items.map((d) => (
            <li key={d.id}>
              <Link to={`/dashboards/${d.id}`} className="card block p-4 transition hover:bg-white/5">
                <p className="flex items-center gap-2 font-medium text-slate-100">
                  <LayoutDashboard className="h-4 w-4 text-teal-400" /> {d.name}
                  {d.published && (
                    <span className="ml-auto inline-flex items-center gap-1 rounded-full border border-teal-500/30 bg-teal-500/10 px-2 py-0.5 text-xs text-teal-300">
                      <Globe2 className="h-3 w-3" /> Public
                    </span>
                  )}
                </p>
                <p className="mt-1 text-sm text-slate-400">
                  {d.site_id ? d.site_name : 'Personal'} · {d.widget_count} widget{d.widget_count === 1 ? '' : 's'}
                </p>
                {d.description && <p className="mt-1 line-clamp-2 text-sm text-slate-500">{d.description}</p>}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

export default function Dashboards() {
  const navigate = useNavigate()
  const { currentUser } = useAuthContext()
  const { dashboards, loading, error } = useDashboards()
  const [searchParams, setSearchParams] = useSearchParams()
  // ?new=1 (from the Status pages tab's "+ New → Dashboard") opens the dialog.
  const [creating, setCreating] = useState(() => searchParams.get('new') === '1')
  const closeCreate = () => {
    setCreating(false)
    if (searchParams.has('new')) setSearchParams({}, { replace: true })
  }

  const groups = useMemo(() => {
    const me = currentUser?.user_id
    return {
      mine: dashboards.filter((d) => d.owner_id === me),
      shared: dashboards.filter((d) => d.owner_id && d.owner_id !== me),
      site: dashboards.filter((d) => d.site_id),
    }
  }, [dashboards, currentUser])

  return (
    <div className="space-y-8">
      <DashboardsHeader active="dashboards" onNewDashboard={() => setCreating(true)} />
      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}
      {loading ? (
        <Loader2 className="h-6 w-6 animate-spin text-slate-500" />
      ) : (
        <>
          <DashboardList title="Mine" items={groups.mine} empty="You have no dashboards of your own yet." />
          <DashboardList
            title={currentUser?.is_admin ? "Other people's" : 'Shared with me'}
            items={groups.shared}
            empty="Nothing here yet."
          />
          <DashboardList title="Site dashboards" items={groups.site} empty="No site has a dashboard yet." />
        </>
      )}
      {creating && (
        <NewDashboardModal onClose={closeCreate} onCreated={(d) => navigate(`/dashboards/${d.id}/edit`)} />
      )}
    </div>
  )
}
