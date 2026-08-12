import { ChevronDown, ChevronRight, Wrench } from 'lucide-react'
import { useMonitorUptime } from '@/hooks/useMonitorUptime'
import DetailPanel, { uptimeColor } from '@/components/DetailPanel'
import HealthBar24h from '@/components/HealthBar24h'
import { formatResponseTime } from '@/utils/formatters'
import { monitorAccess, badgeToneClass } from '@/utils/monitorAccess'
import type { Monitor, MonitorGroup } from '@/types'

// Two tags is what a dense row can carry without starving the monitor's name;
// the rest fold into a "+N".
const MAX_INLINE_TAGS = 2

function responseColor(ms: number): string {
  if (ms <= 0) return 'text-neutral-500'
  if (ms < 200) return 'text-primary-400'
  if (ms <= 500) return 'text-amber-400'
  return 'text-red-400'
}

// "45m left" / "2h 15m left" from the maintenance window's remaining minutes.
function maintenanceLeft(mins: number | undefined): string | null {
  if (!mins || mins <= 0) return null
  const h = Math.floor(mins / 60)
  const m = mins % 60
  return h > 0 ? `${h}h ${m}m left` : `${m}m left`
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
  // One fetch per card powers both the detail panel and the fallback uptime %.
  const { data: uptime, loading: uptimeLoading } = useMonitorUptime(monitor.id, '24h')

  const access = monitorAccess(monitor)
  const inMaintenance = monitor.is_in_maintenance ?? false
  const maintLeft = maintenanceLeft(monitor.time_remaining_minutes)
  const online = monitor.current_status === 'online'
  const offline = monitor.current_status === 'offline'
  const pct = uptime?.uptime_24h ?? uptime24h
  // Tags share the name's line, so cap how many take room from it; the rest
  // collapse into a "+N" that names them on hover.
  const tags = monitor.tags ?? []
  const shownTags = tags.slice(0, MAX_INLINE_TAGS)
  const hiddenTags = tags.slice(MAX_INLINE_TAGS)
  // Status drives the row's left channel border + fade accent.
  const statusColor = inMaintenance
    ? 'var(--color-accent-warning)'
    : online
      ? 'var(--color-accent-online)'
      : offline
        ? 'var(--color-accent-offline)'
        : 'var(--rd-text-muted)'

  return (
    <div
      className="rd-card overflow-hidden transition"
      style={{ ['--rd-accent' as string]: statusColor }}
    >
      {/* Collapsed row (click to expand) */}
      <button
        onClick={() => onToggle(monitor.id)}
        className="flex w-full items-center gap-4 px-5 py-3.5 text-left"
      >
        {/* LEFT: status + identity */}
        <div className="relative z-10 flex min-w-0 flex-1 items-center gap-3">
          <span
            className={`h-2.5 w-2.5 shrink-0 rounded-full ${offline || inMaintenance ? 'animate-pulse' : ''}`}
            style={{ backgroundColor: statusColor, boxShadow: `0 0 8px ${statusColor}` }}
          />
          <span className="min-w-0">
            {/* Type reads as the silkscreen caption over the name, the way an
                instrument labels the channel it is showing. */}
            <span className="vs-eyebrow block" style={{ color: 'var(--vs-cyan)', letterSpacing: '0.12em' }}>
              {monitor.type}
            </span>
            <span className="flex items-center gap-2">
              <span className="truncate text-[15px] font-semibold" style={{ color: 'var(--vs-text)' }}>
                {monitor.name}
              </span>
              {inMaintenance && (
                <span
                  className="vs-glow inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-bold uppercase"
                  style={{
                    color: 'var(--vs-amber)',
                    backgroundColor: 'rgba(255, 194, 75, 0.12)',
                    border: '1px solid rgba(255, 194, 75, 0.5)',
                    letterSpacing: '0.1em',
                  }}
                  title={maintLeft ? `Maintenance — ${maintLeft}` : 'Maintenance mode active'}
                >
                  <Wrench className="h-3 w-3" /> Maintenance
                  {maintLeft && <span className="vs-readout font-medium normal-case">· {maintLeft}</span>}
                </span>
              )}
              {/* Tags sit beside the name, but never at its expense: the group
                  is hidden until the row is wide enough to hold both, and the
                  outsized shrink factor means any remaining squeeze eats into
                  the tags rather than truncating the name. */}
              {tags.length > 0 && (
                <span
                  className="hidden min-w-0 items-center gap-2 overflow-hidden xl:flex"
                  style={{ flexShrink: 999 }}
                >
                  {shownTags.map((t) => (
                    <span
                      key={t}
                      className="shrink-0 rounded-full bg-primary-100 px-2 py-0.5 text-[10px] font-medium text-primary-700 dark:bg-primary-900/40 dark:text-primary-300"
                    >
                      {t}
                    </span>
                  ))}
                  {hiddenTags.length > 0 && (
                    <span
                      className="shrink-0 rounded-full px-2 py-0.5 text-[10px] font-medium"
                      style={{ backgroundColor: 'rgba(255,255,255,0.05)', color: 'var(--vs-text-dim)' }}
                      title={hiddenTags.join(', ')}
                    >
                      +{hiddenTags.length}
                    </span>
                  )}
                </span>
              )}
              {access.badge && (
                <span className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium ${badgeToneClass[access.badge.tone]}`}>
                  {access.badge.label}
                </span>
              )}
            </span>
            <span className="block truncate text-xs" style={{ color: 'var(--vs-text-dim)' }}>
              {monitor.url}
              {!access.isOwner && ownerUsername && <span className="ml-2">· owned by {ownerUsername}</span>}
            </span>
          </span>
        </div>

        {/* MIDDLE: the last 24 hours, one capsule per hour. Fixed widths rather
            than flex-1, so the bar never competes with the monitor's name for
            room — it appears only once the row is wide enough to carry it. */}
        <div className="relative z-10 hidden w-[220px] shrink-0 lg:block xl:w-[320px]">
          <HealthBar24h hours={uptime?.hourly_data ?? []} loading={uptimeLoading} />
        </div>

        {/* RESP readout */}
        <div className="relative z-10 hidden w-16 shrink-0 text-right sm:block">
          <div className="vs-eyebrow">Resp</div>
          <div className={`vs-readout text-sm font-medium ${responseColor(monitor.last_response_time_ms)}`}>
            {formatResponseTime(monitor.last_response_time_ms)}
          </div>
        </div>

        {/* UPTIME readout */}
        <div className="relative z-10 w-20 shrink-0 text-right">
          <div className="vs-eyebrow">Uptime 24h</div>
          <div className={`vs-readout text-2xl font-medium leading-none ${pct != null ? uptimeColor(pct) : 'text-neutral-500'}`}>
            {pct != null ? `${pct.toFixed(1)}%` : '—'}
          </div>
        </div>

        <span className="relative z-10 shrink-0" style={{ color: 'var(--vs-cyan)' }}>
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
