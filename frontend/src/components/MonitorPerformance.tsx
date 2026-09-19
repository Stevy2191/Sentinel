import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { Loader2 } from 'lucide-react'
import type { UptimeHistory, UptimeRange } from '@/hooks/useMonitorUptime'

export const MONITOR_RANGES = [
  { value: '24h', label: 'Last 24 hours' },
  { value: '7d', label: 'Last 7 days' },
  { value: '30d', label: 'Last 30 days' },
] as const

/**
 * Response time over the selected range, the monitor counterpart to a server's
 * historical performance.
 *
 * Only successful checks contribute a duration: the time a timeout took
 * measures the timeout setting rather than the service, and averaging it in
 * would make an outage look like a slowdown.
 */
export default function MonitorPerformance({
  uptime,
  loading,
  range,
  onRangeChange,
}: {
  uptime: UptimeHistory | null
  loading: boolean
  range: UptimeRange
  onRangeChange: (r: UptimeRange) => void
}) {
  const data = uptime?.response_time_data ?? []
  const hasData = data.some((p) => p.responseTime > 0)
  const label = MONITOR_RANGES.find((r) => r.value === range)?.label ?? range

  // A long range crowds the axis, so fewer ticks are drawn as it widens.
  const tickInterval = range === '24h' ? 3 : range === '7d' ? 3 : 4

  return (
    <section>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-light text-white">Response Time</h2>
        <select
          value={range}
          onChange={(e) => onRangeChange(e.target.value as UptimeRange)}
          aria-label="Time range"
          className="cursor-pointer rounded-lg border border-white/10 bg-slate-800/50 px-3 py-1.5 text-sm text-white focus:border-white/30 focus:outline-none"
        >
          {MONITOR_RANGES.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </select>
      </div>

      {loading ? (
        <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-slate-800/40 p-10 text-sm text-slate-400">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading response times…
        </div>
      ) : !hasData ? (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-10 text-center text-sm text-slate-400">
          <p>No successful checks in the {label.toLowerCase().replace('last ', '')}.</p>
          <p className="mt-1 text-xs text-slate-500">
            Response times appear here once the monitor has run.
          </p>
        </div>
      ) : (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-4 backdrop-blur-sm">
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              {/* Left margin is 0, not negative: a negative margin shifts the
                  Y axis off the canvas rather than narrowing it, and this
                  chart's widest tick was landing exactly on the edge. */}
              <AreaChart data={data} margin={{ top: 5, right: 10, bottom: 0, left: 0 }}>
                <defs>
                  <linearGradient id="monitor-rt" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#22d3ee" stopOpacity={0.28} />
                    <stop offset="95%" stopColor="#22d3ee" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#16303a" strokeOpacity={0.6} />
                <XAxis
                  dataKey="time"
                  tick={{ fontSize: 10, fill: '#7A8A94' }}
                  interval={tickInterval}
                  minTickGap={16}
                />
                <YAxis tick={{ fontSize: 10, fill: '#7A8A94' }} width={44} />
                <Tooltip
                  formatter={(v: number) => [`${v} ms`, 'response']}
                  contentStyle={{
                    background: '#0f172a',
                    border: '1px solid rgba(255,255,255,0.1)',
                    borderRadius: 8,
                    color: '#e2e8f0',
                  }}
                />
                <Area
                  type="monotone"
                  dataKey="responseTime"
                  stroke="#22d3ee"
                  strokeWidth={2}
                  fill="url(#monitor-rt)"
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
          {/* Named rather than left to be inferred: a gap in the line is a
              bucket with no successful check, not a zero-millisecond response. */}
          <p className="mt-2 text-xs text-slate-500">
            Averaged per {range === '24h' ? 'hour' : range === '7d' ? '6 hours' : 'day'}, from
            successful checks only.
          </p>
        </div>
      )}
    </section>
  )
}
