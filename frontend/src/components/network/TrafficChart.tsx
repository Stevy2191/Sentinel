import { useId, useMemo, useState } from 'react'
import { Area, AreaChart, CartesianGrid, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { Loader2 } from 'lucide-react'
import { METRICS_RANGES, useMetricsQuery, type MetricsQuery, type MetricsRange } from '@/hooks/useMetrics'
import { formatBps } from '@/utils/network'

export interface ChartLine {
  metric: string
  label: string
  colour: string
}

export type Unit = 'bps' | 'pct' | 'per_min' | 'min' | 'custom'

interface Props {
  title: string
  query: Omit<MetricsQuery, 'range' | 'metrics'>
  lines: ChartLine[]
  unit: Unit
  /** unit 'custom' only: the units suffix, e.g. "°C". */
  unitLabel?: string
  /** Drawn as a dashed line, e.g. the nearly-full threshold. */
  threshold?: number
  defaultRange?: MetricsRange
}

function formatValue(v: number, unit: Unit, unitLabel?: string): string {
  if (unit === 'bps') return formatBps(v)
  if (unit === 'pct') return `${Math.round(v)}%`
  if (unit === 'min') return `${Math.round(v)} min`
  if (unit === 'custom') return `${+v.toFixed(1)} ${unitLabel ?? ''}`.trim()
  return `${+v.toFixed(1)}/min`
}

/** 1h/6h/24h show the time of day; longer ranges show the date. */
const SHORT: MetricsRange[] = ['1h', '6h', '24h']

function tick(t: number, shortTicks: boolean): string {
  const d = new Date(t)
  return shortTicks
    ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    : d.toLocaleDateString([], { month: 'short', day: 'numeric' })
}

export interface AreaSeriesChartProps {
  /** Each row has `t` (epoch ms) plus one number per line's metric key. */
  data: Record<string, number>[]
  lines: ChartLine[]
  unit: Unit
  /** unit 'custom' only: the units suffix, e.g. "°C". */
  unitLabel?: string
  /** Drawn as a dashed line, e.g. the nearly-full threshold. */
  threshold?: number
  /** The query's step, for the "Averaged per N minutes" note. */
  step?: number
  /** Whether the caller's selected range is short (1h/6h/24h): decides the
   *  x-axis tick format (time of day vs. date). Defaults to false (date). */
  shortTicks?: boolean
  /** Fill the parent instead of drawing a fixed-height card: dashboard
   *  widgets, whose frame is already the card. */
  bare?: boolean
}

/** The chart body: a gradient-filled area chart plus its legend. Assumes
 *  data.length > 0; callers handle their own loading/error/empty states. */
export function AreaSeriesChart({ data, lines, unit, unitLabel, threshold, step, shortTicks = false, bare = false }: AreaSeriesChartProps) {
  const gid = useId().replace(/:/g, '')
  return (
    <div className={bare ? 'flex h-full flex-col' : 'rounded-lg border border-white/10 bg-slate-800/40 p-4'}>
      <div className={bare ? 'min-h-0 flex-1' : 'h-64'}>
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={data} margin={{ top: 5, right: 10, bottom: 0, left: 0 }}>
            <defs>
              {lines.map((l) => (
                <linearGradient key={l.metric} id={`${gid}-${l.metric}`} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor={l.colour} stopOpacity={0.25} />
                  <stop offset="95%" stopColor={l.colour} stopOpacity={0} />
                </linearGradient>
              ))}
            </defs>
            <CartesianGrid strokeDasharray="3 3" stroke="#16303a" strokeOpacity={0.6} />
            <XAxis dataKey="t" type="number" scale="time" domain={['dataMin', 'dataMax']} tickFormatter={(t: number) => tick(t, shortTicks)}
              tick={{ fontSize: 10, fill: '#7A8A94' }} minTickGap={24} />
            <YAxis tickFormatter={(v: number) => formatValue(v, unit, unitLabel)} tick={{ fontSize: 10, fill: '#7A8A94' }} width={68} />
            <Tooltip
              labelFormatter={(t: number) => new Date(t).toLocaleString()}
              formatter={(v: number, name) => [formatValue(v, unit, unitLabel), lines.find((l) => l.metric === String(name))?.label ?? name]}
              contentStyle={{ background: '#0f172a', border: '1px solid rgba(255,255,255,0.1)', borderRadius: 8, color: '#e2e8f0' }}
            />
            {threshold != null && <ReferenceLine y={threshold} stroke="#eab308" strokeDasharray="4 4" />}
            {lines.map((l) => (
              <Area key={l.metric} type="monotone" dataKey={l.metric} stroke={l.colour} strokeWidth={2}
                fill={`url(#${gid}-${l.metric})`} connectNulls={false} isAnimationActive={false} />
            ))}
          </AreaChart>
        </ResponsiveContainer>
      </div>
      <div className="mt-2 flex flex-wrap gap-4 text-xs text-slate-400">
        {lines.map((l) => (
          <span key={l.metric} className="flex items-center gap-1.5">
            <span className="h-0.5 w-4" style={{ background: l.colour }} aria-hidden /> {l.label}
          </span>
        ))}
        {step != null && step > 60 && <span className="ml-auto text-slate-500">Averaged per {Math.round(step / 60)} minutes</span>}
      </div>
    </div>
  )
}

/** A metrics chart with its own range picker. Several instances of one
 *  metric in the result (a device's ports) are added together. */
export default function TrafficChart({ title, query, lines, unit, unitLabel, threshold, defaultRange = '24h' }: Props) {
  const [range, setRange] = useState<MetricsRange>(defaultRange)
  const metrics = lines.map((l) => l.metric)
  // Keyed by value: callers pass a fresh object every render.
  const key = JSON.stringify({ ...query, metrics, range })
  const q = useMemo(() => JSON.parse(key) as MetricsQuery, [key])
  const { result, loading, error } = useMetricsQuery(q)

  const data = useMemo(() => {
    const rows = new Map<number, Record<string, number>>()
    for (const s of result?.series ?? []) {
      for (const p of s.points) {
        const t = new Date(p.t).getTime()
        const row = rows.get(t) ?? { t }
        row[s.metric] = (row[s.metric] ?? 0) + p.avg
        rows.set(t, row)
      }
    }
    return [...rows.values()].sort((a, b) => a.t - b.t)
  }, [result])

  return (
    <section>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-light text-white">{title}</h2>
        <select
          value={range}
          onChange={(e) => setRange(e.target.value as MetricsRange)}
          aria-label={`${title} time range`}
          className="cursor-pointer rounded-lg border border-white/10 bg-slate-800/50 px-3 py-1.5 text-sm text-white focus:border-white/30 focus:outline-none"
        >
          {METRICS_RANGES.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </select>
      </div>
      {loading && !result ? (
        <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-slate-800/40 p-10 text-sm text-slate-400">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading…
        </div>
      ) : error ? (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-400">{error}</div>
      ) : data.length === 0 ? (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-10 text-center text-sm text-slate-400">
          No data in this range yet. Figures appear a minute or two after the first stats poll.
        </div>
      ) : (
        <AreaSeriesChart data={data} lines={lines} unit={unit} unitLabel={unitLabel} threshold={threshold} step={result?.step_seconds} shortTicks={SHORT.includes(range)} />
      )}
    </section>
  )
}
