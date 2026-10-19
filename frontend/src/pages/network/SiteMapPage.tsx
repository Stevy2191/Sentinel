import { useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import BackLink from '@/components/BackLink'
import DisplayShell from '@/components/dashboards/DisplayShell'
import SiteMapView from '@/components/network/sitemap/SiteMapView'
import MapSharePanel from '@/components/network/sitemap/MapSharePanel'
import { useSiteMap } from '@/hooks/useSiteMap'
import { useDisplayMode } from '@/hooks/useDisplayMode'
import { usePageLabel } from '@/utils/navHistory'

/** /network/sites/:id/map: the site's routers and switches and how they
 *  connect, live. ?device=<id> opens on that device; ?display=1 is the wall
 *  display. */
export default function SiteMapPage() {
  const { id } = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const display = searchParams.get('display') === '1'
  const setDisplay = (on: boolean) =>
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (on) next.set('display', '1')
        else next.delete('display')
        return next
      },
      { replace: !on }
    )
  const { fullscreen, enter, exit } = useDisplayMode(display)
  const { map, traffic, live, error } = useSiteMap(id)
  const [sharing, setSharing] = useState(false)
  usePageLabel(map ? `${map.site_name} map` : 'Site map')

  if (!map) {
    return error ? (
      <div className="space-y-3">
        <p className="text-sm text-slate-300">This map isn't available. The site may be gone, or not shared with you.</p>
      </div>
    ) : (
      <Loader2 className="h-6 w-6 animate-spin text-slate-500" />
    )
  }
  const lastUpdated = traffic?.at ? new Date(traffic.at).getTime() : null
  if (display) {
    return (
      <DisplayShell
        title={`${map.site_name} map`}
        lastUpdated={lastUpdated}
        fullscreen={fullscreen}
        onFullscreen={() => void enter()}
        onExit={() => {
          void exit()
          setDisplay(false)
        }}
      >
        <SiteMapView map={map} traffic={traffic} live={live} mapKey={map.site_id} height="calc(100vh - 9rem)" />
      </DisplayShell>
    )
  }
  return (
    <div className="space-y-4">
      <BackLink fallbackTo={`/network/sites/${map.site_id}`} fallbackLabel={map.site_name} className="text-sm text-slate-400 hover:text-white">
        {(label) => <>← {label}</>}
      </BackLink>
      <h1 className="text-4xl font-light text-white">Site map</h1>
      <SiteMapView
        map={map}
        traffic={traffic}
        live={live}
        mapKey={map.site_id}
        initialDevice={searchParams.get('device')}
        onFullscreen={() => setDisplay(true)}
        onShare={map.is_admin ? () => setSharing(true) : undefined}
        height="70vh"
      />
      {sharing && <MapSharePanel siteId={map.site_id} onClose={() => setSharing(false)} />}
    </div>
  )
}
