import { useCallback, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, Globe2, Loader2, Maximize2, Pencil, Share2 } from 'lucide-react'
import { useDashboard } from '@/hooks/useDashboards'
import { useDisplayMode } from '@/hooks/useDisplayMode'
import { useAuthContext } from '@/context/AuthContext'
import type { MetricsRange } from '@/hooks/useMetrics'
import DisplayShell from '@/components/dashboards/DisplayShell'
import DashboardGrid from '@/components/dashboards/DashboardGrid'
import WidgetBody from '@/components/dashboards/WidgetBody'
import DashboardEditor from '@/components/dashboards/DashboardEditor'
import RangeOverride from '@/components/dashboards/RangeOverride'
import DashboardSharingPanel from '@/components/dashboards/DashboardSharingPanel'
import PublicLinkPanel from '@/components/dashboards/PublicLinkPanel'
import { isTimeBased } from '@/utils/dashboards'

export default function DashboardPage({ mode }: { mode: 'view' | 'edit' }) {
  const { id } = useParams()
  const { dashboard, setDashboard, loading, notFound, error, refetch } = useDashboard(id)
  const navigate = useNavigate()
  const [override, setOverride] = useState<MetricsRange | ''>('')
  const { currentUser } = useAuthContext()
  const [sharing, setSharing] = useState(false)
  const [linking, setLinking] = useState(false)
  const [display, setDisplay] = useState(false)
  const [lastUpdated, setLastUpdated] = useState<number | null>(null)
  const { fullscreen, enter, exit } = useDisplayMode(display)
  const onUpdated = useCallback((t: number) => setLastUpdated((prev) => (prev != null && prev > t ? prev : t)), [])

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
    if (!dashboard.can_edit) {
      return (
        <p className="text-slate-400">
          {dashboard.published ? 'This dashboard has a public link, so only an admin can change it.' : 'You can view this dashboard but not change it.'}
        </p>
      )
    }
    return (
      <DashboardEditor
        dashboard={dashboard}
        onSaved={(d) => {
          setDashboard(d)
          navigate(`/dashboards/${d.id}`)
        }}
        onCancel={() => navigate(`/dashboards/${dashboard.id}`)}
      />
    )
  }

  const widgets = dashboard.widgets ?? []
  if (display) {
    return (
      <DisplayShell
        title={dashboard.name}
        lastUpdated={lastUpdated}
        fullscreen={fullscreen}
        onFullscreen={() => void enter()}
        onExit={() => {
          void exit()
          setDisplay(false)
        }}
      >
        <DashboardGrid
          items={widgets}
          editing={false}
          render={(w) => (
            <WidgetBody widget={w} display onUpdated={onUpdated} linkable source={{ kind: 'dashboard', dashboardId: dashboard.id, widgetId: w.id, override }} />
          )}
        />
      </DisplayShell>
    )
  }
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
          {dashboard.can_share && !dashboard.site_id && (
            <button className="btn-secondary flex items-center gap-2" onClick={() => setSharing(true)}>
              <Share2 className="h-4 w-4" /> Share
            </button>
          )}
          {currentUser?.is_admin && (
            <button className="btn-secondary flex items-center gap-2" onClick={() => setLinking(true)}>
              <Globe2 className="h-4 w-4" /> {dashboard.published ? 'Public link' : 'Publish'}
            </button>
          )}
          <button
            className="btn-secondary flex items-center gap-2"
            title="For a screen that stays up for days, use a public link: a signed-in session ends after 24 hours"
            onClick={() => {
              setDisplay(true)
              void enter()
            }}
          >
            <Maximize2 className="h-4 w-4" /> Fullscreen
          </button>
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
      {sharing && <DashboardSharingPanel dashboard={dashboard} onClose={() => setSharing(false)} />}
      {linking && <PublicLinkPanel dashboardId={dashboard.id} onClose={() => setLinking(false)} onChanged={refetch} />}
    </div>
  )
}
