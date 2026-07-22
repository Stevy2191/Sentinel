import { ChevronDown, ChevronRight } from 'lucide-react'
import { useMonitorUptime } from '@/hooks/useMonitorUptime'
import DetailPanel, { Sparkline, uptimeColor } from '@/components/DetailPanel'
import { formatResponseTime } from '@/utils/formatters'
import { monitorAccess, badgeToneClass } from '@/utils/monitorAccess'
import type { Monitor, MonitorGroup } from '@/types'

function responseColor(ms: number): string {
  if (ms <= 0) return 'text-neutral-400'
  if (ms < 200) return 'text-emerald-500'
  if (ms <= 500) return 'text-amber-500'
  return 'text-red-500'
}

interface Props {
  monitor: Monitor
  uptime24h: number | null // instant value from the summary endpoint (fallback)
  expanded: boolean
  groups: MonitorGroup[]
  ownerUsername?: string
  onToggle: (id: string) => void
  onChanged: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

export default function MonitorCard({ monitor, uptime24h, expanded, groups, ownerUsername, onToggle, onChanged, push }: Props) {
  // One fetch per card powers both the collapsed sparkline and the detail panel.
  const { data: uptime, loading: uptimeLoading } = useMonitorUptime(monitor.id, '24h')

  const access = monitorAccess(monitor)
  const inMaintenance = monitor.is_in_maintenance ?? false
  const online = monitor.current_status === 'online'
  const offline = monitor.current_status === 'offline'
  const pct = uptime?.uptime_24h ?? uptime24h
  // Status drives the row's left border + fade accent.
  const statusColor = inMaintenance
    ? 'var(--color-accent-warning)'
    : online
      ? 'var(--color-accent-online)'
      : offline
        ? 'var(--color-accent-offline)'
        : 'var(--rd-text-muted)'

  return (
    <div
      className="rd-card overflow-hidden transition hover:shadow-card-hover"
      style={{ borderRadius: '28px', ['--rd-accent' as string]: statusColor }}
    >
      {/* Collapsed row (click to expand) */}
      <button
        onClick={() => onToggle(monitor.id)}
        className="flex w-full flex-wrap items-center gap-x-4 gap-y-2 px-5 py-4 text-left"
      >
        {/* LEFT: status + identity */}
        <div className="relative z-10 flex min-w-0 flex-1 items-center gap-3">
          <span
            className={`h-3 w-3 shrink-0 rounded-full ${offline ? 'animate-pulse' : ''}`}
            style={{ backgroundColor: statusColor }}
          />
          <span className="min-w-0">
            <span className="flex items-center gap-2">
              <span className="truncate font-black" style={{ color: 'var(--rd-text)' }}>
                {monitor.name}
              </span>
              <span
                className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase"
                style={{ color: 'var(--color-accent-primary)' }}
              >
                {monitor.type}
              </span>
              {access.badge && (
                <span className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium ${badgeToneClass[access.badge.tone]}`}>
                  {access.badge.label}
                </span>
              )}
            </span>
            <span className="block truncate text-xs" style={{ color: 'var(--color-accent-primary)' }}>
              {monitor.url}
              {!access.isOwner && ownerUsername && (
                <span className="ml-2" style={{ color: 'var(--rd-text-muted)' }}>· Owned by {ownerUsername}</span>
              )}
            </span>
          </span>
        </div>

        {/* MIDDLE: metrics */}
        <div className="relative z-10 hidden w-36 shrink-0 text-right sm:block">
          <div className="text-[10px] uppercase" style={{ color: 'var(--rd-text-muted)' }}>Response time</div>
          <div className={`text-sm font-black ${responseColor(monitor.last_response_time_ms)}`}>
            {formatResponseTime(monitor.last_response_time_ms)}
          </div>
        </div>

        {/* RIGHT: uptime % + sparkline */}
        <div className="relative z-10 w-28 shrink-0">
          <div className="text-[10px] uppercase" style={{ color: 'var(--rd-text-muted)' }}>Uptime</div>
          <div className={`text-2xl font-black leading-none ${pct != null ? uptimeColor(pct) : 'text-neutral-400'}`}>
            {pct != null ? `${pct.toFixed(1)}%` : '—'}
          </div>
          <div className="mt-1 h-[30px]">
            {uptime ? (
              <Sparkline data={uptime.hourly_data} className="h-[30px]" />
            ) : (
              <div className="h-[30px] rounded" style={{ backgroundColor: 'var(--rd-border)' }} />
            )}
          </div>
        </div>

        <span className="relative z-10 shrink-0" style={{ color: 'var(--color-accent-primary)' }}>
          {expanded ? <ChevronDown className="h-5 w-5" /> : <ChevronRight className="h-5 w-5" />}
        </span>
      </button>

      {/* Expanded detail */}
      {expanded && (
        <DetailPanel
          monitor={monitor}
          uptime={uptime}
          uptimeLoading={uptimeLoading}
          groups={groups}
          access={access}
          ownerUsername={ownerUsername}
          onChanged={onChanged}
          push={push}
        />
      )}
    </div>
  )
}
