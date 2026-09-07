import { useMemo } from 'react'
import { useMonitorUptime, type HourPoint } from '@/hooks/useMonitorUptime'
import DetailPanel, { uptimeColor } from '@/components/DetailPanel'
import { formatResponseTime } from '@/utils/formatters'
import { monitorAccess, badgeToneClass } from '@/utils/monitorAccess'
import type { Monitor, MonitorGroup } from '@/types'

// The reference draws twenty bars in the uptime column.
const BARS = 20

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

// Bar colour per hourly bucket. `nodata` and unobserved hours read as an empty
// slot rather than a passing check, so a new monitor does not show a solid
// green history it never earned.
const barClass: Record<string, string> = {
  up: 'bg-emerald-500',
  down: 'bg-red-500',
  partial: 'bg-yellow-500',
  nodata: 'bg-slate-700',
}

function UptimeBars({ hours, loading }: { hours: HourPoint[]; loading: boolean }) {
  // Take the most recent BARS buckets, oldest-to-newest left-to-right, and pad
  // the left with empty slots when the monitor has less history than that.
  const cells = useMemo(() => {
    const recent = hours.slice(-BARS)
    const pad = Array.from({ length: Math.max(0, BARS - recent.length) }, () => null)
    return [...pad, ...recent]
  }, [hours])

  if (loading) {
    return (
      <div className="flex gap-px">
        {Array.from({ length: BARS }).map((_, i) => (
          <div key={i} className="h-3 w-1 animate-pulse rounded-sm bg-slate-700" />
        ))}
      </div>
    )
  }

  return (
    <div className="flex gap-px">
      {cells.map((h, i) => (
        <div
          key={i}
          className={`h-3 w-1 rounded-sm ${
            h && h.observed ? (barClass[h.status] ?? 'bg-slate-700') : 'bg-slate-800'
          }`}
          title={h ? `${h.status} · ${h.uptime.toFixed(1)}%` : 'no data'}
        />
      ))}
    </div>
  )
}

interface RowProps {
  monitor: Monitor
  uptime24h: number | null
  expanded: boolean
  groups: MonitorGroup[]
  ownerUsername?: string
  onToggle: (id: string) => void
  onChanged: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

function MonitorRow({
  monitor,
  uptime24h,
  expanded,
  groups,
  ownerUsername,
  onToggle,
  onChanged,
  push,
}: RowProps) {
  // One fetch per row powers both the bar strip and the uptime percentage,
  // and feeds the detail panel when the row is opened.
  const { data: uptime, loading } = useMonitorUptime(monitor.id, '24h')

  const access = monitorAccess(monitor)
  const tone = toneOf(monitor)
  const pill = statusPill[tone]
  const pct = uptime?.uptime_24h ?? uptime24h
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
        onClick={() => onToggle(monitor.id)}
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
          {formatResponseTime(monitor.last_response_time_ms)}
        </td>

        <td className="px-4 py-3">
          <div className="flex items-center gap-2">
            <UptimeBars hours={uptime?.hourly_data ?? []} loading={loading} />
            <span
              className={`whitespace-nowrap text-xs font-medium tabular-nums ${
                pct != null ? uptimeColor(pct) : 'text-slate-500'
              }`}
            >
              {pct != null ? `${pct.toFixed(1)}%` : '—'}
            </span>
          </div>
          {/* The reference labels this "Last 20 checks". The API exposes hourly
              buckets rather than individual checks, so the caption names what
              the bars actually are. */}
          <div className="text-xs text-slate-500">Last {BARS} hours</div>
        </td>

        <td className="px-4 py-3 text-xs text-slate-500">{checked}</td>

        <td className="px-4 py-3 text-slate-400">
          <button
            className="rounded px-1 leading-none transition hover:text-slate-200"
            aria-label={expanded ? `Collapse ${monitor.name}` : `Expand ${monitor.name}`}
            aria-expanded={expanded}
            onClick={(e) => {
              e.stopPropagation()
              onToggle(monitor.id)
            }}
          >
            ⋯
          </button>
        </td>
      </tr>

      {expanded && (
        <tr>
          {/* Full-width drawer under the row, carrying the same detail panel
              the card layout used, so opening a row keeps every action
              (edit, pause, group, delete) that was there before. */}
          <td colSpan={7} className="bg-slate-900/40 p-0">
            <DetailPanel
              monitor={monitor}
              uptime={uptime}
              uptimeLoading={loading}
              groups={groups}
              access={access}
              ownerUsername={ownerUsername}
              onChanged={onChanged}
              push={push}
            />
          </td>
        </tr>
      )}
    </>
  )
}

interface Props {
  monitors: Monitor[]
  uptimeById: Map<string, number>
  groups: MonitorGroup[]
  expandedId: string | null
  onToggle: (id: string) => void
  usernameFor: (id: string | null | undefined) => string | undefined
  onChanged: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

/**
 * MonitorTable — the reference's "Monitored Services" table: name, type,
 * status, response time, a twenty-bar uptime strip, last check, actions.
 * A row expands in place into the full monitor detail panel.
 */
export default function MonitorTable({
  monitors,
  uptimeById,
  groups,
  expandedId,
  onToggle,
  usernameFor,
  onChanged,
  push,
}: Props) {
  return (
    <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40 backdrop-blur-sm">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-white/10 bg-slate-800/20">
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
                expanded={expandedId === m.id}
                groups={groups}
                ownerUsername={usernameFor(m.owner_id)}
                onToggle={onToggle}
                onChanged={onChanged}
                push={push}
              />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
