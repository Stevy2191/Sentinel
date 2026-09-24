import { useNavigate } from 'react-router-dom'
import { useMonitorUptime } from '@/hooks/useMonitorUptime'
import { Sparkline, uptimeColor } from '@/components/UptimeSparkline'
import MonitorRowActions from '@/components/MonitorRowActions'
import { formatLastResponseTime } from '@/utils/formatters'
import { monitorAccess, badgeToneClass } from '@/utils/monitorAccess'
import type { Monitor } from '@/types'

/** Colour for the response-time cell: fast, acceptable, slow. */
function responseColor(ms: number): string {
  if (ms <= 0) return 'text-slate-500'
  if (ms < 200) return 'text-emerald-400'
  if (ms <= 500) return 'text-yellow-400'
  return 'text-red-400'
}

type Tone = 'up' | 'down' | 'maintenance' | 'paused' | 'pending'

function toneOf(m: Monitor): Tone {
  if (!m.enabled) return 'paused'
  if (m.is_in_maintenance) return 'maintenance'
  if (m.current_status === 'online') return 'up'
  if (m.current_status === 'offline') return 'down'
  return 'pending'
}

// Whole literal class strings per tone — Tailwind only emits CSS for class
// names spelled out in the source, so these cannot be built from a key.
const statusPill: Record<Tone, { cls: string; dot: string; label: string }> = {
  up: {
    cls: 'bg-emerald-500/20 text-emerald-400 border-emerald-500/30',
    dot: 'bg-emerald-400',
    label: 'Up',
  },
  down: {
    cls: 'bg-red-500/20 text-red-400 border-red-500/30',
    dot: 'bg-red-400',
    label: 'Down',
  },
  maintenance: {
    cls: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30',
    dot: 'bg-yellow-400',
    label: 'Maintenance',
  },
  paused: {
    cls: 'bg-slate-500/20 text-slate-400 border-slate-500/30',
    dot: 'bg-slate-400',
    label: 'Paused',
  },
  pending: {
    cls: 'bg-slate-500/20 text-slate-400 border-slate-500/30',
    dot: 'bg-slate-400',
    label: 'Pending',
  },
}

interface RowProps {
  monitor: Monitor
  uptime24h: number | null
  ownerUsername?: string
  onChanged: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
  /** Changes on each dashboard refresh, re-fetching this row's checks. */
  refreshKey?: unknown
}

function MonitorRow({
  monitor,
  uptime24h,
  ownerUsername,
  onChanged,
  push,
  refreshKey,
}: RowProps) {
  const navigate = useNavigate()
  // One fetch per row powers both the bar strip and the uptime percentage.
  const { data: uptime, loading } = useMonitorUptime(monitor.id, '24h', true, refreshKey)

  const access = monitorAccess(monitor)
  const tone = toneOf(monitor)
  const pill = statusPill[tone]
  // Uptime over the same 24 hourly buckets the strip draws, so the cell is
  // internally consistent. Falls back to the summary endpoint's 24h figure
  // only until this row's own request lands. The 7d/30d windows are still
  // shown, labelled, in the detail panel that opens beneath the row.
  const pct = uptime ? uptime.uptime_24h : uptime24h
  const checked = monitor.last_check_at
    ? new Date(monitor.last_check_at).toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
      })
    : '—'

  return (
    <>
      <tr
        className="cursor-pointer transition hover:bg-white/5"
        onClick={() => navigate(`/monitors/${monitor.id}`)}
      >
        <td className="px-4 py-3">
          <div className="flex items-center gap-2">
            <span className="truncate font-medium text-slate-200">{monitor.name}</span>
            {access.badge && (
              <span
                className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium ${badgeToneClass[access.badge.tone]}`}
              >
                {access.badge.label}
              </span>
            )}
          </div>
          <div className="truncate text-xs text-slate-500">
            {monitor.url}
            {!access.isOwner && ownerUsername && <span className="ml-2">· {ownerUsername}</span>}
          </div>
        </td>

        <td className="px-4 py-3">
          <span className="text-xs font-medium uppercase text-slate-400">{monitor.type}</span>
        </td>

        <td className="px-4 py-3">
          <span className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium ${pill.cls}`}>
            <span className={`h-1.5 w-1.5 rounded-full ${pill.dot}`} />
            {pill.label}
          </span>
        </td>

        <td className={`px-4 py-3 tabular-nums ${responseColor(monitor.last_response_time_ms)}`}>
          {formatLastResponseTime(monitor.last_response_time_ms, monitor.current_status)}
        </td>

        <td className="px-4 py-3">
          <div className="flex items-center gap-2">
            {loading && !uptime ? (
              <div className="flex h-6 w-24 items-end gap-px">
                {Array.from({ length: 24 }).map((_, i) => (
                  <div key={i} className="w-full flex-1 animate-pulse rounded-sm bg-slate-700" style={{ height: '40%' }} />
                ))}
              </div>
            ) : (
              <Sparkline data={uptime?.hourly_data ?? []} className="h-6 w-24" />
            )}
            <span
              className={`whitespace-nowrap text-xs font-medium tabular-nums ${
                pct != null ? uptimeColor(pct) : 'text-slate-500'
              }`}
            >
              {pct != null ? `${pct.toFixed(1)}%` : '\u2014'}
            </span>
          </div>
          {/* The strip and the percentage describe the same sample: the last
              24 hourly buckets, whatever their check activity. */}
          <div className="text-xs text-slate-500">Last 24 hours</div>
        </td>

        <td className="px-4 py-3 text-xs text-slate-500">{checked}</td>

        {/* Stops propagation so using the menu does not also navigate away
            from the list it was opened in. */}
        <td className="px-4 py-3 text-slate-400" onClick={(e) => e.stopPropagation()}>
          <MonitorRowActions
            monitor={monitor}
            access={access}
            onChanged={onChanged}
            push={push}
          />
        </td>
      </tr>

    </>
  )
}

interface Props {
  monitors: Monitor[]
  uptimeById: Map<string, number>
  usernameFor: (id: string | null | undefined) => string | undefined
  onChanged: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
  /** Changes on each dashboard refresh, keeping every row's strip current. */
  refreshKey?: unknown
}

/**
 * MonitorTable — the reference's "Monitored Services" table: name, type,
 * status, response time, a 24-hour uptime strip, last check, actions.
 * A row expands in place into the full monitor detail panel.
 */
export default function MonitorTable({
  monitors,
  uptimeById,
  usernameFor,
  onChanged,
  push,
  refreshKey,
}: Props) {
  return (
    <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40 backdrop-blur-sm">
      {/* max-h + overflow-auto on this same element, not a separate wrapper -
          position:sticky below only sticks relative to its nearest actual
          scrolling ancestor, and an outer div scrolling vertically while this
          one only scrolled horizontally would leave the header unstuck,
          scrolling away with the rest of the table (confirmed empirically:
          splitting the two axes across nested divs breaks it). One capped,
          both-axes-scrollable box is what actually keeps the header pinned
          while the rows scroll past it. */}
      <div className="max-h-[65vh] overflow-auto">
        <table className="w-full text-sm">
          <thead className="sticky top-0 z-10">
            <tr className="border-b border-white/10 bg-slate-900">
              <th className="px-4 py-3 text-left text-xs font-medium text-slate-400">Service Name</th>
              <th className="px-4 py-3 text-left text-xs font-medium text-slate-400">Service Type</th>
              <th className="px-4 py-3 text-left text-xs font-medium text-slate-400">Service Status</th>
              <th className="px-4 py-3 text-left text-xs font-medium text-slate-400">Response Time</th>
              <th className="px-4 py-3 text-left text-xs font-medium text-slate-400">Uptime</th>
              <th className="px-4 py-3 text-left text-xs font-medium text-slate-400">Last Checked</th>
              <th className="px-4 py-3 text-left text-xs font-medium text-slate-400">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5">
            {monitors.map((m) => (
              <MonitorRow
                key={m.id}
                monitor={m}
                uptime24h={uptimeById.get(m.id) ?? null}
                ownerUsername={usernameFor(m.owner_id)}
                onChanged={onChanged}
                push={push}
                refreshKey={refreshKey}
              />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
