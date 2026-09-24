import type { HourPoint, HourStatus } from '@/hooks/useMonitorUptime'

/** What Sparkline actually draws for one bucket, whatever it represents (an
 * hour or a day) - deliberately narrow so a trimmed response (e.g. the public
 * status page's, which omits exact downtime clock times) can be drawn by the
 * same component. `label` names the bucket in the tooltip ("14:00", "Jun 27"). */
export interface SparklinePoint {
  label: string
  uptime: number
  status: HourStatus
}

/** Adapts hourly buckets (used by the monitor detail page and the internal
 * Uptime Monitoring table) into Sparkline's generic shape. */
export function hourlySparklinePoints(data: HourPoint[]): SparklinePoint[] {
  return data.map((d) => ({
    label: `${String(d.hour).padStart(2, '0')}:00`,
    uptime: d.uptime,
    status: d.status,
  }))
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
 * Sparkline renders a row of buckets (hourly or daily) coloured by status. Bar
 * height reflects the bucket's uptime, with a floor so down and no-data
 * buckets stay visible rather than collapsing to nothing. Native title
 * tooltips name the bucket and status.
 */
export function Sparkline({ data, className }: { data: SparklinePoint[]; className?: string }) {
  const height = (d: SparklinePoint) => (d.status === 'nodata' ? 15 : Math.max(12, d.uptime))
  return (
    <div className={`flex items-end gap-px ${className ?? ''}`}>
      {data.map((d, i) => (
        <div
          key={i}
          title={`${d.label} — ${d.status}${d.status === 'nodata' ? '' : ` (${d.uptime}%)`}`}
          className="flex-1 rounded-sm"
          style={{ height: `${height(d)}%`, minWidth: 2, backgroundColor: STATUS_COLOR[d.status] }}
        />
      ))}
    </div>
  )
}
