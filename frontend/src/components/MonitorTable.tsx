import { useMemo } from 'react'
import { useMonitorUptime, type RecentCheck } from '@/hooks/useMonitorUptime'
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

// Bar colour per check outcome. A timeout is distinguished from an outright
// failure: both are down, but they fail differently and it is worth seeing.
const barClass: Record<RecentCheck['status'], string> = {
  success: 'bg-emerald-500',
  failed: 'bg-red-500',
  timeout: 'bg-yellow-500',
}

/** "2:05 PM" — enough to place a check without crowding the tooltip. */
function checkTitle(c: RecentCheck): string {
  const when = new Date(c.timestamp).toLocaleString([], {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
  const detail =
    c.status === 'success' ? `${c.response_time_ms}ms` : c.error_message || c.status
  return `${when} · ${detail}`
}

function UptimeBars({ checks, loading }: { checks: RecentCheck[]; loading: boolean }) {
  // Oldest-to-newest left-to-right (the API already orders them that way), with
  // empty slots padding the left when the monitor has run fewer than BARS times.
  const cells = useMemo(() => {
    const recent = checks.slice(-BARS)
    const pad = Array.from({ length: Math.max(0, BARS - recent.length) }, () => null)
    return [...pad, ...recent]
  }, [checks])

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
      {cells.map((c, i) => (
        <div
          key={i}
          className={`h-3 w-1 rounded-sm ${c ? barClass[c.status] : 'bg-slate-800'}`}
          title={c ? checkTitle(c) : 'no check yet'}
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
  /** Changes on each dashboard refresh, re-fetching this row's checks. */
  refreshKey?: unknown
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
  refreshKey,
}: RowProps) {
  // One fetch per row powers both the bar strip and the uptime percentage,
  // and feeds the detail panel when the row is opened.
  const { data: uptime, loading } = useMonitorUptime(monitor.id, '24h', true, refreshKey)

  const access = monitorAccess(monitor)
  const tone = toneOf(monitor)
  const pill = statusPill[tone]
  // Pass rate over the same checks the strip draws, so the cell is internally
  // consistent. Falls back to the summary endpoint's 24h figure only until this
  // row's own request lands. The 24h/7d/30d windows are still shown, labelled,
  // in the detail panel that opens beneath the row.
  const recent = uptime?.recent_checks ?? []
  const checkCount = Math.min(recent.length, BARS)
  const pct = uptime ? uptime.recent_uptime : uptime24h
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
            <UptimeBars checks={uptime?.recent_checks ?? []} loading={loading} />
            <span
              className={`whitespace-nowrap text-xs font-medium tabular-nums ${
                pct != null ? uptimeColor(pct) : 'text-slate-500'
              }`}
            >
              {pct != null ? `${pct.toFixed(1)}%` : '\u2014'}
            </span>
          </div>
          {/* Both the strip and the percentage describe the same sample: the
              last N checks, whenever they happened. The caption names the real
              count so a monitor with only six checks does not claim twenty. */}
          <div className="text-xs text-slate-500">
            {checkCount > 0 ? `Last ${checkCount} check${checkCount === 1 ? '' : 's'}` : 'No checks yet'}
          </div>
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
  /** Changes on each dashboard refresh, keeping every row's strip current. */
  refreshKey?: unknown
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
  refreshKey,
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
                refreshKey={refreshKey}
              />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
