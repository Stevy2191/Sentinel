import { Link } from 'react-router-dom'
import MapCanvas from '@/components/network/sitemap/MapCanvas'
import { useSiteMap } from '@/hooks/useSiteMap'

const NO_FOLDS = new Set<string>()

/** The site page's map card: the site's routers and switches at card size,
 *  live, opening the map page. Hidden when the site has none. */
export default function SiteMapCard({ siteId }: { siteId: string }) {
  const { map, traffic } = useSiteMap(siteId)
  if (!map || map.devices.length === 0) return null
  return (
    <section className="space-y-2" aria-label="Map">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-light text-white">Map</h2>
        <Link to={`/network/sites/${siteId}/map`} className="text-sm text-sky-400 hover:underline">
          Open map →
        </Link>
      </div>
      <Link to={`/network/sites/${siteId}/map`} className="block h-[260px] overflow-hidden rounded-xl border border-white/10" aria-label="Open the site map">
        <div className="pointer-events-none h-full">
          <MapCanvas map={map} traffic={traffic} view="simple" compact folds={NO_FOLDS} onFold={() => {}} />
        </div>
      </Link>
    </section>
  )
}
