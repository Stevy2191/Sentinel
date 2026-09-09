import { useMemo } from 'react'
import {
  Area,
  AreaChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { Loader2 } from 'lucide-react'
import type { AgentMetric } from '@/hooks/useAgents'

export const RANGES = [
  { value: '1h', label: 'Last 60 minutes' },
  { value: '6h', label: 'Last 6 hours' },
  { value: '24h', label: 'Last 24 hours' },
  { value: '168h', label: 'Last 7 days' },
] as const

export type RangeValue = (typeof RANGES)[number]['value']

interface Point {
  time: string
  cpu: number | null
  memory: number | null
  disk: number | null
  netIn: number | null
  netOut: number | null
}

/**
 * Turns samples into chart points, converting network counters into a rate.
 *
 * The agent reports the kernel's cumulative byte counts, which only ever climb,
 * so plotting them directly draws a line rising forever and says nothing about
 * traffic. The difference between neighbouring samples divided by the time
 * between them is the throughput someone is actually looking for.
 *
 * A counter that goes backwards means the host rebooted and the kernel started
 * again from zero. That is not negative traffic, so the point is dropped rather
 * than drawn as a spike downward.
 */
function toPoints(metrics: AgentMetric[], range: RangeValue): Point[] {
  const long = range === '24h' || range === '168h'
  const fmt: Intl.DateTimeFormatOptions = long
    ? { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }
    : { hour: '2-digit', minute: '2-digit' }

  return metrics.map((m, i) => {
    const point: Point = {
      time: new Date(m.timestamp).toLocaleString([], fmt),
      cpu: m.cpu_percent,
      memory: m.memory_percent,
      disk: m.disk_percent,
      netIn: null,
      netOut: null,
    }

    const prev = metrics[i - 1]
    if (prev) {
      const seconds = (new Date(m.timestamp).getTime() - new Date(prev.timestamp).getTime()) / 1000
      if (seconds > 0) {
        const rate = (now: number | null, before: number | null) =>
          now == null || before == null || now < before ? null : (now - before) / seconds
        point.netIn = rate(m.network_in_bytes, prev.network_in_bytes)
        point.netOut = rate(m.network_out_bytes, prev.network_out_bytes)
      }
    }
    return point
  })
}

function formatBytesPerSecond(v: number): string {
  if (v >= 1e9) return `${(v / 1e9).toFixed(1)} GB/s`
  if (v >= 1e6) return `${(v / 1e6).toFixed(1)} MB/s`
  if (v >= 1e3) return `${(v / 1e3).toFixed(1)} kB/s`
  return `${Math.round(v)} B/s`
}

const AXIS = { stroke: '#64748b', fontSize: 11 }

function Chart({
  title,
  data,
  series,
  unit,
}: {
  title: string
  data: Point[]
  series: { key: keyof Point; name: string; colour: string }[]
  unit: 'percent' | 'bytes'
}) {
  const percent = unit === 'percent'
  return (
    <div className="rounded-lg border border-white/10 bg-slate-800/40 p-4 backdrop-blur-sm">
      <h3 className="mb-3 text-sm font-medium text-white">{title}</h3>
      <div className="h-48">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={data} margin={{ top: 4, right: 8, bottom: 0, left: -12 }}>
            <defs>
              {series.map((s) => (
                <linearGradient key={s.key} id={`fill-${s.key}`} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={s.colour} stopOpacity={0.35} />
                  <stop offset="100%" stopColor={s.colour} stopOpacity={0} />
                </linearGradient>
              ))}
            </defs>
            <CartesianGrid stroke="#ffffff12" vertical={false} />
            <XAxis dataKey="time" tick={AXIS} tickLine={false} axisLine={false} minTickGap={40} />
            <YAxis
              tick={AXIS}
              tickLine={false}
              axisLine={false}
              domain={percent ? [0, 100] : [0, 'auto']}
              tickFormatter={(v: number) => (percent ? `${v}%` : formatBytesPerSecond(v))}
              width={percent ? 40 : 68}
            />
            <Tooltip
              contentStyle={{
                background: '#0f172a',
                border: '1px solid rgba(255,255,255,0.1)',
                borderRadius: 8,
                fontSize: 12,
              }}
              labelStyle={{ color: '#94a3b8' }}
              formatter={(value: number, name: string) => [
                percent ? `${value.toFixed(1)}%` : formatBytesPerSecond(value),
                name,
              ]}
            />
            {series.length > 1 && <Legend wrapperStyle={{ fontSize: 11, color: '#94a3b8' }} />}
            {series.map((s) => (
              <Area
                key={s.key}
                type="monotone"
                dataKey={s.key}
                name={s.name}
                stroke={s.colour}
                fill={`url(#fill-${s.key})`}
                strokeWidth={1.5}
                dot={false}
                // Samples arrive only while the agent is running, so a gap is
                // real. Joining across it would draw a straight line through
                // time nothing was measured.
                connectNulls={false}
                isAnimationActive={false}
              />
            ))}
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
}

export default function HistoricalPerformance({
  metrics,
  loading,
  range,
  onRangeChange,
}: {
  metrics: AgentMetric[]
  loading: boolean
  range: RangeValue
  onRangeChange: (r: RangeValue) => void
}) {
  const data = useMemo(() => toPoints(metrics, range), [metrics, range])
  const label = RANGES.find((r) => r.value === range)?.label ?? range

  return (
    <section>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-light text-white">Historical Performance</h2>
        <select
          value={range}
          onChange={(e) => onRangeChange(e.target.value as RangeValue)}
          aria-label="Time range"
          className="cursor-pointer rounded-lg border border-white/10 bg-slate-800/50 px-3 py-1.5 text-sm text-white focus:border-white/30 focus:outline-none"
        >
          {RANGES.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </select>
      </div>

      {loading ? (
        <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-slate-800/40 p-10 text-sm text-slate-400">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading metrics…
        </div>
      ) : data.length < 2 ? (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-10 text-center text-sm text-slate-400">
          <p>No historical data for the {label.toLowerCase().replace('last ', '')}.</p>
          <p className="mt-1 text-xs text-slate-500">
            {data.length === 1
              ? 'Only one sample so far — a chart needs at least two points.'
              : 'Metrics appear here once the agent has reported.'}
          </p>
        </div>
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          <Chart
            title="CPU Usage"
            data={data}
            unit="percent"
            series={[{ key: 'cpu', name: 'CPU', colour: '#38bdf8' }]}
          />
          <Chart
            title="Memory Usage"
            data={data}
            unit="percent"
            series={[{ key: 'memory', name: 'Memory', colour: '#34d399' }]}
          />
          <Chart
            title="Disk Usage"
            data={data}
            unit="percent"
            series={[{ key: 'disk', name: 'Disk', colour: '#fbbf24' }]}
          />
          <Chart
            title="Network I/O"
            data={data}
            unit="bytes"
            series={[
              { key: 'netIn', name: 'In', colour: '#a78bfa' },
              { key: 'netOut', name: 'Out', colour: '#f472b6' },
            ]}
          />
        </div>
      )}
    </section>
  )
}
