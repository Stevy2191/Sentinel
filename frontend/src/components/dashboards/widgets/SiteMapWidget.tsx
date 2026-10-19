import { Link } from 'react-router-dom'
import MapCanvas from '@/components/network/sitemap/MapCanvas'
import type { SiteMapData } from '@/types/dashboards'

const NO_FOLDS = new Set<string>()

/** A site's map on a dashboard: Simple cards, fitted to the widget, live
 *  through the widget's own refresh every 10 s. */
export default function SiteMapWidget({ data, linkable }: { data: SiteMapData; linkable: boolean }) {
  return (
    <div className="relative h-full min-h-[160px] overflow-hidden rounded-lg">
      <MapCanvas map={data.map} traffic={data.traffic} view="simple" compact folds={NO_FOLDS} onFold={() => {}} fitKey={data.map.devices.length} />
      {linkable && data.map.site_id && (
        <Link
          to={`/network/sites/${data.map.site_id}/map`}
          className="absolute right-2 top-2 rounded bg-slate-900/80 px-2 py-0.5 text-xs text-sky-400 hover:underline"
        >
          Open map →
        </Link>
      )}
    </div>
  )
}
