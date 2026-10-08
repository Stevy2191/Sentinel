import { Link } from 'react-router-dom'
import type { Monitor } from '@/types'
import { monitorState, VIEW_STATUS_LABEL } from '@/utils/monitoringView'
import { formatResponseTime } from '@/utils/formatters'
import { STATE_DOT } from '@/utils/siteProfile'

/** The uptime checks whose site is this site that the viewer can see. */
export default function SiteChecksList({ monitors, error }: { monitors: Monitor[]; error?: string | null }) {
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-light text-white">Uptime checks ({monitors.length})</h2>
      {error ? (
        <p className="text-sm text-red-400">Could not load uptime checks.</p>
      ) : monitors.length === 0 ? (
        <p className="text-sm text-slate-500">None at this site. A monitor’s site is set in its edit form.</p>
      ) : (
        <ul className="card divide-y divide-white/10">
          {monitors.map((m) => {
            const state = monitorState(m)
            return (
              <li key={m.id}>
                <Link to={`/monitors/${m.id}`} className="flex items-center justify-between gap-3 p-3 text-sm hover:bg-white/5">
                  <span className="flex min-w-0 items-center gap-2">
                    <span className={`h-2 w-2 shrink-0 rounded-full ${STATE_DOT[state]}`} title={VIEW_STATUS_LABEL[state]} aria-label={VIEW_STATUS_LABEL[state]} />
                    <span className="truncate text-slate-200">{m.name}</span>
                  </span>
                  <span className="shrink-0 tabular-nums text-slate-400">
                    {m.last_check_at ? formatResponseTime(m.last_response_time_ms) : '—'}
                  </span>
                </Link>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}
