import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import axios from 'axios'
import { Loader2 } from 'lucide-react'
import type { ApiResponse } from '@/types'
import type { PublicDashboard as PublicLayout } from '@/types/dashboards'
import { useDisplayMode } from '@/hooks/useDisplayMode'
import DisplayShell from '@/components/dashboards/DisplayShell'
import DashboardGrid from '@/components/dashboards/DashboardGrid'
import WidgetBody from '@/components/dashboards/WidgetBody'

const LAYOUT_EVERY_MS = 5 * 60 * 1000

/** A dashboard's public link: no login, straight into display mode. The
 *  layout is fetched again every 5 minutes so an admin's edits reach the TV. */
export default function PublicDashboardPage() {
  const { token } = useParams()
  const [layout, setLayout] = useState<PublicLayout | null>(null)
  const [gone, setGone] = useState(false)
  const [lastUpdated, setLastUpdated] = useState<number | null>(null)
  const { fullscreen, enter } = useDisplayMode(true)
  const onUpdated = useCallback((t: number) => setLastUpdated((prev) => (prev != null && prev > t ? prev : t)), [])

  useEffect(() => {
    if (!token) return
    let cancelled = false
    const load = () =>
      axios
        .get<ApiResponse<PublicLayout>>(`/api/v1/public/dashboards/${token}`, { timeout: 60000 })
        .then((res) => {
          if (cancelled) return
          setLayout(res.data.data)
          setGone(false)
        })
        .catch((err: unknown) => {
          // A 404 is final (revoked, regenerated, or its admin was demoted);
          // anything else is a blip, and the next attempt may succeed.
          if (!cancelled && axios.isAxiosError(err) && err.response?.status === 404) setGone(true)
        })
    void load()
    const t = window.setInterval(() => void load(), LAYOUT_EVERY_MS)
    return () => {
      cancelled = true
      window.clearInterval(t)
    }
  }, [token])

  if (gone) {
    return <div className="flex min-h-screen items-center justify-center bg-slate-950 p-8 text-center text-2xl font-light text-slate-300">This dashboard link is no longer available.</div>
  }
  if (!layout || !token) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-950">
        <Loader2 className="h-8 w-8 animate-spin text-slate-500" />
      </div>
    )
  }
  return (
    <DisplayShell title={layout.name} lastUpdated={lastUpdated} fullscreen={fullscreen} onFullscreen={() => void enter()}>
      <DashboardGrid
        items={layout.widgets}
        editing={false}
        render={(w) => <WidgetBody widget={w} display onUpdated={onUpdated} linkable={false} source={{ kind: 'public', token, widgetId: w.id }} />}
      />
    </DisplayShell>
  )
}
