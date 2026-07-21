import { ChevronDown, ChevronRight } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { useMonitorUptime } from '@/hooks/useMonitorUptime'
import DetailPanel, { Sparkline, uptimeColor } from '@/components/DetailPanel'
import { formatResponseTime } from '@/utils/formatters'
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
  onToggle: (id: string) => void
  onChanged: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

export default function MonitorCard({ monitor, uptime24h, expanded, groups, onToggle, onChanged, push }: Props) {
  // One fetch per card powers both the collapsed sparkline and the detail panel.
  const { data: uptime, loading: uptimeLoading } = useMonitorUptime(monitor.id, '24h')

  const inMaintenance = monitor.is_in_maintenance ?? false
  const online = monitor.current_status === 'online'
  const offline = monitor.current_status === 'offline'
  const pct = uptime?.uptime_24h ?? uptime24h

  return (
    <div className="overflow-hidden rounded-md border border-neutral-200 bg-white transition hover:shadow-card-hover dark:border-neutral-800 dark:bg-neutral-900">
      {/* Collapsed row (click to expand) */}
      <button
        onClick={() => onToggle(monitor.id)}
        className="flex w-full flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 text-left transition hover:bg-neutral-50 dark:hover:bg-neutral-800/40"
      >
        {/* LEFT: status + identity */}
        <div className="flex min-w-0 flex-1 items-center gap-3">
          <span
            className={`h-2.5 w-2.5 shrink-0 rounded-full ${
              inMaintenance ? 'bg-amber-500' : online ? 'bg-emerald-500' : offline ? 'bg-red-500 animate-pulse' : 'bg-neutral-400'
            }`}
          />
          <span className="min-w-0">
            <span className="flex items-center gap-2">
              <span className="truncate font-semibold">{monitor.name}</span>
              <span className="shrink-0 rounded bg-neutral-100 px-1.5 py-0.5 text-[10px] font-semibold uppercase text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400">
                {monitor.type}
              </span>
            </span>
            <span className="block truncate text-xs text-neutral-500 dark:text-neutral-400">{monitor.url}</span>
          </span>
        </div>

        {/* MIDDLE: metrics */}
        <div className="hidden w-36 shrink-0 text-right sm:block">
          <div className={`text-sm font-medium ${responseColor(monitor.last_response_time_ms)}`}>
            {formatResponseTime(monitor.last_response_time_ms)}
          </div>
          <div className="text-xs text-neutral-500 dark:text-neutral-400">
            {monitor.last_check_at ? `${formatDistanceToNow(new Date(monitor.last_check_at))} ago` : 'never'}
          </div>
        </div>

        {/* RIGHT: uptime % + sparkline */}
        <div className="w-28 shrink-0">
          <div className={`text-right text-2xl font-bold leading-none ${pct != null ? uptimeColor(pct) : 'text-neutral-400'}`}>
            {pct != null ? `${pct.toFixed(1)}%` : '—'}
          </div>
          <div className="mt-1 h-[30px]">
            {uptime ? (
              <Sparkline data={uptime.hourly_data} className="h-[30px]" />
            ) : (
              <div className="h-[30px] rounded bg-neutral-100 dark:bg-neutral-800" />
            )}
          </div>
        </div>

        {expanded ? (
          <ChevronDown className="h-5 w-5 shrink-0 text-neutral-400" />
        ) : (
          <ChevronRight className="h-5 w-5 shrink-0 text-neutral-400" />
        )}
      </button>

      {/* Expanded detail */}
      {expanded && (
        <DetailPanel
          monitor={monitor}
          uptime={uptime}
          uptimeLoading={uptimeLoading}
          groups={groups}
          onChanged={onChanged}
          push={push}
        />
      )}
    </div>
  )
}
