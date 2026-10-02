import { Link } from 'react-router-dom'
import type { EventItem, EventLogData } from '@/types/dashboards'
import { CONDITION_LABEL, EVENT_LABEL } from '@/utils/network'

const KIND_LABEL: Record<string, string> = { ...EVENT_LABEL, incident_opened: 'Incident opened', incident_closed: 'Incident closed' }
const DOT: Record<string, string> = {
  incident_opened: 'bg-red-400',
  incident_closed: 'bg-emerald-400',
  link_down: 'bg-red-400',
  link_up: 'bg-emerald-400',
}

function what(e: EventItem): string {
  if (e.kind.startsWith('incident_')) {
    return e.condition && CONDITION_LABEL[e.condition] && !e.subject.includes(CONDITION_LABEL[e.condition])
      ? `${e.subject} · ${CONDITION_LABEL[e.condition]}`
      : e.subject
  }
  return `${e.device_name} · ${e.port_label || e.port_name}`
}

export default function EventLogWidget({ data, linkable }: { data: EventLogData; linkable: boolean }) {
  const items = data.items ?? []
  if (items.length === 0) return <p className="text-slate-500">No events yet.</p>
  return (
    <ul className="divide-y divide-white/5">
      {items.map((e, i) => {
        const href = !linkable
          ? null
          : e.incident_id
            ? `/incidents/${e.incident_id}`
            : e.device_id && e.if_index != null
              ? `/network/devices/${e.device_id}/ports/${e.if_index}`
              : null
        return (
          <li key={`${e.time}-${e.kind}-${i}`} className="flex items-center gap-2 py-1.5">
            <span className={`h-2 w-2 shrink-0 rounded-full ${DOT[e.kind] ?? 'bg-slate-500'}`} aria-hidden />
            <span className="w-24 shrink-0 tabular-nums text-xs text-slate-500">
              {new Date(e.time).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}
            </span>
            <span className="shrink-0 text-slate-300">{KIND_LABEL[e.kind] ?? e.kind}</span>
            {href ? (
              <Link to={href} className="truncate text-slate-200 hover:text-primary-400">
                {what(e)}
              </Link>
            ) : (
              <span className="truncate text-slate-200">{what(e)}</span>
            )}
          </li>
        )
      })}
    </ul>
  )
}
