import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, Loader2, Pencil } from 'lucide-react'
import { useDashboard } from '@/hooks/useDashboards'
import type { MetricsRange } from '@/hooks/useMetrics'
import DashboardGrid from '@/components/dashboards/DashboardGrid'
import WidgetBody from '@/components/dashboards/WidgetBody'
import RangeOverride from '@/components/dashboards/RangeOverride'
import { isTimeBased } from '@/utils/dashboards'

export default function DashboardPage({ mode }: { mode: 'view' | 'edit' }) {
  const { id } = useParams()
  const { dashboard, loading, notFound, error } = useDashboard(id)
  const [override, setOverride] = useState<MetricsRange | ''>('')

  if (loading) return <Loader2 className="h-6 w-6 animate-spin text-slate-500" />
  if (notFound || !dashboard) {
    return (
      <div className="space-y-3">
        <p className="text-slate-400">{error ?? 'Dashboard not found.'}</p>
        <Link to="/dashboards" className="text-primary-400 hover:underline">
          Back to dashboards
        </Link>
      </div>
    )
  }
  if (mode === 'edit') {
    // Task 23 replaces this branch with the editor.
    return <p className="text-slate-400">The editor is not built yet.</p>
  }

  const widgets = dashboard.widgets ?? []
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <Link to="/dashboards" className="inline-flex items-center gap-1 text-sm text-slate-400 hover:text-slate-200">
            <ArrowLeft className="h-4 w-4" /> Dashboards
          </Link>
          <h1 className="mt-1 break-words text-3xl font-light text-white">{dashboard.name}</h1>
          <p className="text-sm text-slate-400">
            {dashboard.site_id ? (
              <Link to={`/network/sites/${dashboard.site_id}`} className="hover:text-slate-200">
                {dashboard.site_name}
              </Link>
            ) : (
              'Personal'
            )}
            {dashboard.description && ` · ${dashboard.description}`}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {widgets.some((w) => isTimeBased(w.type)) && <RangeOverride value={override} onChange={setOverride} />}
          {dashboard.can_edit && (
            <Link to={`/dashboards/${dashboard.id}/edit`} className="btn-secondary flex items-center gap-2">
              <Pencil className="h-4 w-4" /> Edit
            </Link>
          )}
        </div>
      </div>
      {dashboard.published && !dashboard.can_edit && dashboard.access === 'edit' && (
        <p className="rounded-lg border border-teal-500/30 bg-teal-500/10 p-3 text-sm text-teal-200">
          This dashboard has a public link, so only an admin can change it.
        </p>
      )}
      {widgets.length === 0 ? (
        <p className="text-slate-500">No widgets yet.{dashboard.can_edit && ' Choose Edit to add some.'}</p>
      ) : (
        <DashboardGrid
          items={widgets}
          editing={false}
          render={(w) => (
            <WidgetBody widget={w} linkable source={{ kind: 'dashboard', dashboardId: dashboard.id, widgetId: w.id, override }} />
          )}
        />
      )}
    </div>
  )
}
