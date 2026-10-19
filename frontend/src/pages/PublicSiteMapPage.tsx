import { useParams, useSearchParams } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import DisplayShell from '@/components/dashboards/DisplayShell'
import SiteMapView from '@/components/network/sitemap/SiteMapView'
import { useSiteMap } from '@/hooks/useSiteMap'
import { useDisplayMode } from '@/hooks/useDisplayMode'

/** /public/maps/:token: a site map's read-only public link, live, with no
 *  sign-in. Always shown as a wall display. */
export default function PublicSiteMapPage() {
  const { token } = useParams()
  const [searchParams] = useSearchParams()
  const { map, traffic, live, error } = useSiteMap(undefined, { publicToken: token })
  const { fullscreen, enter } = useDisplayMode(true)
  if (!map) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-950 p-6 text-center text-sm text-slate-300">
        {error ? 'This map link is no longer available.' : <Loader2 className="h-6 w-6 animate-spin text-slate-500" />}
      </div>
    )
  }
  return (
    <DisplayShell title={map.site_name} lastUpdated={traffic?.at ? new Date(traffic.at).getTime() : null} fullscreen={fullscreen} onFullscreen={() => void enter()}>
      <SiteMapView
        map={map}
        traffic={traffic}
        live={live}
        mapKey={`public:${token}`}
        publicToken={token}
        initialDevice={searchParams.get('device')}
        height="calc(100vh - 9rem)"
      />
    </DisplayShell>
  )
}
