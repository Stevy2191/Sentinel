import { Link } from 'react-router-dom'
import { formatDuration } from '@/hooks/useIncidents'
import type { OpenIncidentsData } from '@/types/dashboards'

const SEVERITY: Record<string, string> = {
  critical: 'border-red-500/40 bg-red-500/20 text-red-200',
  high: 'border-red-500/30 bg-red-500/15 text-red-300',
  medium: 'border-amber-500/30 bg-amber-500/15 text-amber-300',
  low: 'border-slate-500/30 bg-slate-500/15 text-slate-300',
}

export default function OpenIncidentsWidget({ data, linkable }: { data: OpenIncidentsData; linkable: boolean }) {
  const incidents = data.incidents ?? []
  if (incidents.length === 0) return <p className="text-emerald-300">No open incidents.</p>
  return (
    <div className="space-y-2">
      <ul className="divide-y divide-white/5">
        {incidents.map((inc, i) => (
          <li key={inc.incident_id ?? `${inc.subject_name}-${i}`} className="flex items-center gap-2 py-1.5">
            <span className={`shrink-0 rounded-full border px-2 py-0.5 text-xs ${SEVERITY[inc.severity] ?? SEVERITY.low}`}>{inc.severity}</span>
            {linkable && inc.incident_id ? (
              <Link to={`/incidents/${inc.incident_id}`} className="min-w-0 flex-1 truncate text-slate-200 hover:text-primary-400">
                {inc.subject_name}
              </Link>
            ) : (
              <span className="min-w-0 flex-1 truncate text-slate-200">{inc.subject_name}</span>
            )}
            {inc.site_name && <span className="hidden shrink-0 text-xs text-slate-500 sm:inline">{inc.site_name}</span>}
            <span className="shrink-0 tabular-nums text-xs text-slate-400">{formatDuration(inc.duration_seconds)}</span>
          </li>
        ))}
      </ul>
      {data.total > incidents.length && <p className="text-xs text-slate-500">and {data.total - incidents.length} more</p>}
    </div>
  )
}
