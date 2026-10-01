import { Link } from 'react-router-dom'
import type { PortEvent } from '@/hooks/usePorts'
import { formatDuration } from '@/hooks/useIncidents'
import { EVENT_LABEL, formatBps, portTitle } from '@/utils/network'

const DOT: Record<string, string> = {
  link_up: 'bg-emerald-500',
  link_down: 'bg-slate-500',
  flapping: 'bg-yellow-500',
  speed_change: 'bg-sky-500',
  errors: 'bg-yellow-500',
  saturated: 'bg-yellow-500',
  slow_link: 'bg-yellow-500',
  admin_up: 'bg-slate-400',
  admin_down: 'bg-slate-700',
}
const SPANS = new Set(['flapping', 'errors', 'saturated', 'slow_link'])

function describe(e: PortEvent): string {
  const d = e.detail ?? {}
  const num = (k: string) => (typeof d[k] === 'number' ? (d[k] as number) : null)
  // Span events (flapping, errors, saturated, slow_link) nest their END
  // figures under detail.end; the START figures stay at the top level of
  // detail. The keys used below (per_minute, util_pct, speed_bps /
  // usual_speed_bps) are the start figures, which is what this function
  // reports for every kind except 'flapping', where the final transition
  // count (if the span has ended) is more useful than the count at the
  // start of the span.
  const end = (d.end && typeof d.end === 'object' ? (d.end as Record<string, unknown>) : {}) ?? {}
  switch (e.kind) {
    case 'speed_change':
      return `${formatBps(num('from_bps'))} → ${formatBps(num('to_bps'))}`
    case 'link_up':
      return d.between_polls ? 'went down and came back between polls' : num('speed_bps') ? `at ${formatBps(num('speed_bps'))}` : ''
    case 'link_down':
      return d.between_polls ? 'went down and came back between polls' : ''
    case 'flapping': {
      const transitions = typeof end.transitions === 'number' ? end.transitions : num('transitions')
      return transitions != null ? `${transitions} link changes` : ''
    }
    case 'errors':
      return num('per_minute') != null ? `${num('per_minute')} per minute` : ''
    case 'saturated':
      return num('util_pct') != null ? `${num('util_pct')}% busy` : ''
    case 'slow_link':
      return num('speed_bps') != null && num('usual_speed_bps') != null
        ? `${formatBps(num('speed_bps'))} instead of ${formatBps(num('usual_speed_bps'))}`
        : ''
    default:
      return ''
  }
}

export default function PortEventList({
  events,
  showDevice = false,
  emptyText = 'No port events yet.',
}: {
  events: PortEvent[]
  showDevice?: boolean
  emptyText?: string
}) {
  if (events.length === 0) return <p className="text-sm text-slate-500">{emptyText}</p>
  return (
    <ul className="card divide-y divide-white/10">
      {events.map((e) => {
        const span = SPANS.has(e.kind)
        const lasted =
          span && e.ended_at ? formatDuration((new Date(e.ended_at).getTime() - new Date(e.started_at).getTime()) / 1000) : null
        return (
          <li key={e.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
            <span className={`h-2 w-2 shrink-0 rounded-full ${DOT[e.kind] ?? 'bg-slate-500'}`} aria-hidden />
            <span className="w-44 shrink-0 tabular-nums text-slate-400">{new Date(e.started_at).toLocaleString()}</span>
            <span className="font-medium text-slate-200">{EVENT_LABEL[e.kind] ?? e.kind}</span>
            <Link to={`/network/devices/${e.device_id}/ports/${e.if_index}`} className="text-slate-300 hover:text-primary-400">
              {showDevice ? `${e.device_name} · ` : ''}
              {portTitle({ number: e.port_number, alias: e.port_alias, stack_unit: e.stack_unit })}
            </Link>
            <span className="text-slate-500">{describe(e)}</span>
            {span && <span className="ml-auto text-xs text-slate-500">{lasted ? `lasted ${lasted}` : 'ongoing'}</span>}
          </li>
        )
      })}
    </ul>
  )
}
