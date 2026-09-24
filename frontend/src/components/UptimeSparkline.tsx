import type { HourStatus } from '@/hooks/useMonitorUptime'

/** The subset of an hourly bucket Sparkline actually draws - deliberately
 * narrow so a trimmed response (e.g. the public status page's, which omits
 * exact downtime clock times) can be drawn by the same component. */
export interface SparklinePoint {
  hour: number
  uptime: number
  status: HourStatus
}

/**
 * Colour scale shared by every uptime visualisation, so an hour that reads as
 * degraded in the list reads the same on the detail page.
 */
export const STATUS_COLOR: Record<HourStatus, string> = {
  up: '#10b981', // ECG green
  down: '#ef4444', // flatline red
  partial: '#eab308', // amber
  nodata: '#4E5E68', // dim
}

export function uptimeColor(pct: number): string {
  if (pct >= 95) return 'text-primary-400'
  if (pct >= 80) return 'text-amber-400'
  return 'text-red-400'
}

/**
 * Sparkline renders 24 hourly bars coloured by status. Bar height reflects the
 * hour's uptime, with a floor so down and no-data hours stay visible rather
 * than collapsing to nothing. Native title tooltips name the hour and status.
 */
export function Sparkline({ data, className }: { data: SparklinePoint[]; className?: string }) {
  const height = (d: SparklinePoint) => (d.status === 'nodata' ? 15 : Math.max(12, d.uptime))
  return (
    <div className={`flex items-end gap-px ${className ?? ''}`}>
      {data.map((d, i) => (
        <div
          key={i}
          title={`${String(d.hour).padStart(2, '0')}:00 — ${d.status}${
            d.status === 'nodata' ? '' : ` (${d.uptime}%)`
          }`}
          className="flex-1 rounded-sm"
          style={{ height: `${height(d)}%`, minWidth: 2, backgroundColor: STATUS_COLOR[d.status] }}
        />
      ))}
    </div>
  )
}
