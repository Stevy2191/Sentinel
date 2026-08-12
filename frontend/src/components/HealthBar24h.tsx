import { useCallback, useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import type { HourPoint } from '@/hooks/useMonitorUptime'

export type PillStatus = 'up' | 'down' | 'maintenance' | 'unknown'

const PILL_COLOR: Record<PillStatus, string> = {
  up: '#22c55e',
  down: '#ef4444',
  maintenance: '#f97316',
  unknown: '#3a4750', // hour the monitor wasn't around for / reported nothing
}

const STATUS_LABEL: Record<PillStatus, string> = {
  up: 'Up',
  down: 'Down',
  maintenance: 'Maintenance',
  unknown: 'No data',
}

// One period of the wave. Pill n takes HEIGHTS[n % 4], so the bar rises and
// falls short → medium → tall → medium across every four hours.
const HEIGHTS = [25, 42.5, 60, 42.5]
const BAR_HEIGHT = Math.max(...HEIGHTS)
const GAP = 5
const HOURS = 24

const pad = (n: number) => String(n).padStart(2, '0')
const hhmm = (d: Date) => `${pad(d.getHours())}:${pad(d.getMinutes())}`

/** "GMT-5" / "GMT+5:30" for the viewer's own timezone. */
function gmtLabel(d: Date): string {
  const minutesEastOfUTC = -d.getTimezoneOffset()
  const sign = minutesEastOfUTC < 0 ? '-' : '+'
  const abs = Math.abs(minutesEastOfUTC)
  const h = Math.floor(abs / 60)
  const m = abs % 60
  return `GMT${sign}${h}${m ? `:${pad(m)}` : ''}`
}

/**
 * An hour is down if any downtime was recorded in it, in maintenance if the
 * window covered it, and up otherwise. Downtime wins over maintenance when both
 * apply: a real outage is the fact, and the tooltip still names the window.
 */
export function pillStatusOf(h: HourPoint): PillStatus {
  if (!h.observed) return 'unknown'
  if (h.down_start) return 'down'
  if (h.maintenance_start) return 'maintenance'
  if (h.status === 'nodata') return 'unknown'
  return 'up'
}

/**
 * "00:00 - 01:00" for a down/maintenance span, in the viewer's timezone. A span
 * that begins and ends in the same minute — a lone failed check — is reported as
 * the one time rather than as a range from a minute to itself.
 */
function spanLabel(start: string | null, end: string | null): string | null {
  if (!start) return null
  const from = hhmm(new Date(start))
  const to = end ? hhmm(new Date(end)) : from
  return from === to ? from : `${from} - ${to}`
}

interface Hovered {
  hour: HourPoint
  x: number // viewport px, centre of the pill
  y: number // viewport px, top of the pill
}

function Tooltip({ hovered }: { hovered: Hovered }) {
  const { hour } = hovered
  const start = new Date(hour.bucket_start)
  const end = new Date(start.getTime() + 3600_000)
  const status = pillStatusOf(hour)
  const down = spanLabel(hour.down_start, hour.down_end)
  const maintenance = spanLabel(hour.maintenance_start, hour.maintenance_end)

  // Keep the bubble clear of the viewport edges; it is centred on the pill.
  const x = Math.min(Math.max(hovered.x, 110), window.innerWidth - 110)

  return createPortal(
    <div
      role="tooltip"
      className="vs-readout pointer-events-none fixed z-[100] -translate-x-1/2 -translate-y-full rounded-lg px-3 py-2 text-xs leading-relaxed shadow-xl"
      style={{
        left: x,
        top: hovered.y - 8,
        backgroundColor: 'var(--vs-panel)',
        border: '1px solid var(--vs-line)',
        color: 'var(--vs-text)',
        whiteSpace: 'nowrap',
      }}
    >
      <div style={{ color: 'var(--vs-text-dim)' }}>
        {start.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}, {hhmm(start)} -{' '}
        {hhmm(end)} {gmtLabel(start)}
      </div>
      <div className="mt-0.5 font-medium" style={{ color: PILL_COLOR[status] }}>
        {/* Only downtime and maintenance need their minutes spelled out — a full
            hour of uptime is fully described by the hour range above. */}
        {status === 'down' && down
          ? `Down: ${down}`
          : status === 'maintenance' && maintenance
            ? `Maintenance: ${maintenance}`
            : STATUS_LABEL[status]}
      </div>
      {/* A window that also saw an outage reports both, downtime first. */}
      {status === 'down' && maintenance && (
        <div className="mt-0.5" style={{ color: PILL_COLOR.maintenance }}>
          Maintenance: {maintenance}
        </div>
      )}
    </div>,
    document.body
  )
}

interface Props {
  /** Hourly buckets oldest-first, as returned by the uptime-history endpoint. */
  hours: HourPoint[]
  loading?: boolean
  className?: string
}

/**
 * HealthBar24h — the previous 24 hours as one capsule per hour, coloured by
 * what actually happened in it (incidents and maintenance windows, not just
 * check counts). Hovering an hour names the range and the minutes lost.
 */
export default function HealthBar24h({ hours, loading = false, className }: Props) {
  const [hovered, setHovered] = useState<Hovered | null>(null)

  // The tooltip is placed from a rect captured on hover, so scrolling would
  // leave it floating away from its pill. Dismiss it instead.
  useEffect(() => {
    if (!hovered) return
    const dismiss = () => setHovered(null)
    window.addEventListener('scroll', dismiss, true)
    return () => window.removeEventListener('scroll', dismiss, true)
  }, [hovered])

  const show = useCallback((hour: HourPoint, el: HTMLElement) => {
    const r = el.getBoundingClientRect()
    setHovered({ hour, x: r.left + r.width / 2, y: r.top })
  }, [])

  // Guard against a short or over-long series so the bar is always 24 pills.
  const series = hours.slice(-HOURS)
  const placeholders = HOURS - series.length

  const counts = series.reduce<Record<PillStatus, number>>(
    (acc, h) => {
      acc[pillStatusOf(h)]++
      return acc
    },
    { up: 0, down: 0, maintenance: 0, unknown: 0 }
  )
  const label = loading
    ? 'Loading the last 24 hours of health data'
    : `Last 24 hours: ${counts.up} up, ${counts.down} down, ${counts.maintenance} in maintenance, ${counts.unknown} unknown`

  return (
    <div
      className={`flex w-full items-center ${className ?? ''}`}
      style={{ gap: GAP, height: BAR_HEIGHT }}
      role="img"
      aria-label={label}
      onMouseLeave={() => setHovered(null)}
    >
      {/* Missing leading hours render as inert wells, so the wave keeps its
          shape while data loads or a young monitor has a short history. */}
      {Array.from({ length: placeholders }).map((_, i) => (
        <div
          key={`placeholder-${i}`}
          className="min-w-[3px] flex-1 rounded-[10px]"
          style={{ height: HEIGHTS[i % HEIGHTS.length], backgroundColor: PILL_COLOR.unknown, opacity: 0.4 }}
        />
      ))}
      {series.map((h, i) => {
        const status = pillStatusOf(h)
        const color = PILL_COLOR[status]
        const active = hovered?.hour.bucket_start === h.bucket_start
        return (
          <div
            key={h.bucket_start}
            className="min-w-[3px] flex-1 rounded-[10px] transition-opacity"
            style={{
              height: HEIGHTS[(placeholders + i) % HEIGHTS.length],
              backgroundColor: color,
              opacity: status === 'unknown' ? 0.4 : active ? 1 : 0.85,
              boxShadow: active ? `0 0 10px -2px ${color}` : undefined,
            }}
            onMouseEnter={(e) => show(h, e.currentTarget)}
          />
        )
      })}
      {hovered && <Tooltip hovered={hovered} />}
    </div>
  )
}
