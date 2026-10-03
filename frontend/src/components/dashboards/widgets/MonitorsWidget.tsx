import { Link } from 'react-router-dom'
import type { MonitorsData, UptimeBucket } from '@/types/dashboards'
import { timeAgo } from '@/utils/network'

const UNKNOWN_DOT = 'bg-slate-500'
const MONITOR_DOT: Record<string, string> = { online: 'bg-emerald-400', offline: 'bg-red-400', paused: UNKNOWN_DOT, unknown: UNKNOWN_DOT, pending: UNKNOWN_DOT }
// Server agents have their own statuses: pending, active or offline.
const AGENT_DOT: Record<string, string> = { active: MONITOR_DOT.online, offline: MONITOR_DOT.offline, pending: UNKNOWN_DOT }
const BAR: Record<UptimeBucket['status'], string> = {
  up: 'bg-emerald-500/80',
  partial: 'bg-amber-400/80',
  down: 'bg-red-500/80',
  nodata: 'bg-slate-700',
}

function Bars({ buckets }: { buckets: UptimeBucket[] }) {
  return (
    <div className="flex h-4 flex-1 gap-px" role="img" aria-label="Uptime history">
      {buckets.map((b, i) => (
        <span key={`${b.label}-${i}`} className={`flex-1 rounded-sm ${BAR[b.status] ?? BAR.nodata}`} title={`${b.label}: ${b.status === 'nodata' ? 'no data' : `${b.uptime}%`}`} />
      ))}
    </div>
  )
}

export default function MonitorsWidget({ data, linkable }: { data: MonitorsData; linkable: boolean }) {
  return (
    <ul className="space-y-1.5">
      {(data.monitors ?? []).map((m, i) => (
        <li key={m.monitor_id ?? `m-${i}`} className="flex items-center gap-2">
          <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${MONITOR_DOT[m.status] ?? UNKNOWN_DOT}`} aria-label={m.status} />
          {linkable && m.monitor_id ? (
            <Link to={`/monitors/${m.monitor_id}`} className="w-40 shrink-0 truncate text-slate-200 hover:text-primary-400">
              {m.name}
            </Link>
          ) : (
            <span className="w-40 shrink-0 truncate text-slate-200">{m.name}</span>
          )}
          {data.style === 'bars' && m.buckets ? (
            <Bars buckets={m.buckets} />
          ) : (
            <span className="flex-1 text-right text-xs text-slate-500">
              {m.status === 'paused' ? 'paused' : `${m.response_time_ms} ms · ${m.last_check_at ? timeAgo(m.last_check_at) : 'never checked'}`}
            </span>
          )}
        </li>
      ))}
      {(data.agents ?? []).map((a, i) => (
        <li key={a.agent_id ?? `a-${i}`} className="flex items-center gap-2">
          <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${AGENT_DOT[a.status] ?? UNKNOWN_DOT}`} aria-label={a.status} />
          <span className="w-40 shrink-0 truncate text-slate-200">{a.name}</span>
          <span className="flex-1 text-right text-xs text-slate-500">
            {a.status} · {a.last_heartbeat ? timeAgo(a.last_heartbeat) : 'never reported'}
          </span>
        </li>
      ))}
    </ul>
  )
}
