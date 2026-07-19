import { useEffect, useMemo, useState } from 'react'
import {
  AreaChart,
  Area,
  BarChart,
  Bar,
  Cell,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts'
import { format, parseISO } from 'date-fns'
import { Download, ArrowUpDown } from 'lucide-react'
import { useMonitors } from '@/hooks/useMonitors'
import { useTimeline, useSummaryReport } from '@/hooks/useReports'
import { formatResponseTime } from '@/utils/formatters'
import type { SummaryMonitor, TimelineGranularity } from '@/types'

type Tab = 'timeline' | 'summary'
type SummarySort = 'name' | 'uptime'

const PRESETS: { label: string; days: number }[] = [
  { label: 'Last 24 hours', days: 1 },
  { label: 'Last 7 days', days: 7 },
  { label: 'Last 30 days', days: 30 },
  { label: 'Last 90 days', days: 90 },
]

function isoFromDateInput(value: string, endOfDay: boolean): string {
  return `${value}T${endOfDay ? '23:59:59' : '00:00:00'}Z`
}
function dateInput(d: Date): string {
  return format(d, 'yyyy-MM-dd')
}
function respFill(ms: number): string {
  if (ms < 200) return '#10b981'
  if (ms <= 500) return '#f59e0b'
  return '#ef4444'
}
function uptimeTextColor(pct: number): string {
  if (pct < 95) return 'text-red-500'
  if (pct < 99) return 'text-amber-500'
  return 'text-emerald-500'
}

function download(filename: string, content: string) {
  const blob = new Blob([content], { type: 'text/csv' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

export default function Reports() {
  const { monitors } = useMonitors({ limit: 500 })
  const [tab, setTab] = useState<Tab>('timeline')

  // Draft filters (edited by the user) and applied filters (used by hooks).
  const now = useMemo(() => new Date(), [])
  const defaultStart = useMemo(() => new Date(now.getTime() - 7 * 86400_000), [now])
  const [draftStart, setDraftStart] = useState(dateInput(defaultStart))
  const [draftEnd, setDraftEnd] = useState(dateInput(now))
  const [draftMonitor, setDraftMonitor] = useState('')
  const [draftMonitorIds, setDraftMonitorIds] = useState<string[]>([])
  const [granularity, setGranularity] = useState<TimelineGranularity>('hourly')

  const [applied, setApplied] = useState({
    start: defaultStart.toISOString(),
    end: now.toISOString(),
    monitor: '',
    monitorIds: [] as string[],
    granularity: 'hourly' as TimelineGranularity,
  })

  // Default the timeline monitor to the first one once loaded.
  useEffect(() => {
    if (!draftMonitor && monitors.length > 0) {
      setDraftMonitor(monitors[0].id)
      setApplied((a) => ({ ...a, monitor: monitors[0].id }))
    }
  }, [monitors, draftMonitor])

  const apply = () => {
    setApplied({
      start: isoFromDateInput(draftStart, false),
      end: isoFromDateInput(draftEnd, true),
      monitor: draftMonitor,
      monitorIds: draftMonitorIds,
      granularity,
    })
  }

  const usePreset = (days: number) => {
    const end = new Date()
    const start = new Date(end.getTime() - days * 86400_000)
    setDraftStart(dateInput(start))
    setDraftEnd(dateInput(end))
    setApplied((a) => ({ ...a, start: start.toISOString(), end: end.toISOString() }))
  }

  const { timeline, loading: tlLoading, error: tlError } = useTimeline(
    tab === 'timeline' ? applied.monitor || undefined : undefined,
    applied.start,
    applied.end,
    applied.granularity
  )
  const { report: summary, loading: smLoading, error: smError } = useSummaryReport(
    applied.start,
    applied.end,
    applied.monitorIds.length ? applied.monitorIds : undefined
  )

  const buckets = timeline?.timeline ?? []
  const tlStats = useMemo(() => {
    if (buckets.length === 0) return null
    const totalChecks = buckets.reduce((s, b) => s + b.checks_total, 0)
    const failed = buckets.reduce((s, b) => s + b.checks_failed, 0)
    const avgUptime = buckets.reduce((s, b) => s + b.uptime_percent, 0) / buckets.length
    const respBuckets = buckets.filter((b) => b.avg_response_time_ms > 0)
    const avgResp = respBuckets.length
      ? Math.round(respBuckets.reduce((s, b) => s + b.avg_response_time_ms, 0) / respBuckets.length)
      : 0
    return { totalChecks, failed, avgUptime, avgResp }
  }, [buckets])

  const tickFmt = (t: string) => {
    try {
      return format(parseISO(t), applied.granularity === 'daily' ? 'MMM d' : 'HH:mm')
    } catch {
      return t
    }
  }

  // Summary sorting
  const [sortKey, setSortKey] = useState<SummarySort>('uptime')
  const [sortAsc, setSortAsc] = useState(false)
  const sortedMonitors = useMemo(() => {
    const rows: SummaryMonitor[] = summary?.monitors ?? []
    return [...rows].sort((a, b) => {
      const cmp =
        sortKey === 'name'
          ? a.monitor_name.localeCompare(b.monitor_name)
          : a.uptime_percent - b.uptime_percent
      return sortAsc ? cmp : -cmp
    })
  }, [summary, sortKey, sortAsc])
  const toggleSort = (k: SummarySort) => {
    if (sortKey === k) setSortAsc((s) => !s)
    else {
      setSortKey(k)
      setSortAsc(k === 'name')
    }
  }

  const exportCsv = () => {
    if (tab === 'timeline') {
      if (buckets.length === 0) return
      const header = 'timestamp,uptime_percent,avg_response_time_ms,checks_total,checks_failed'
      const rows = buckets.map(
        (b) => `${b.timestamp},${b.uptime_percent},${b.avg_response_time_ms},${b.checks_total},${b.checks_failed}`
      )
      download('timeline.csv', [header, ...rows].join('\n'))
    } else {
      if (!summary || summary.monitors.length === 0) return
      const header = 'monitor,status,uptime_percent,downtime_minutes'
      const rows = summary.monitors.map(
        (m) => `${JSON.stringify(m.monitor_name)},${m.status},${m.uptime_percent},${m.downtime_minutes}`
      )
      download('summary.csv', [header, ...rows].join('\n'))
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold">Reports</h1>
          <p className="text-sm text-neutral-500 dark:text-neutral-400">
            View uptime trends and performance analytics
          </p>
        </div>
        <button className="btn-secondary" onClick={exportCsv}>
          <Download className="h-4 w-4" /> Export as CSV
        </button>
      </div>

      {/* Tabs */}
      <div className="flex gap-2">
        {(['timeline', 'summary'] as Tab[]).map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`rounded-md px-4 py-2 text-sm font-medium capitalize ${
              tab === t
                ? 'bg-primary-600 text-white'
                : 'bg-white text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300'
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      {/* Filters */}
      <div className="card space-y-4 p-4">
        <div className="flex flex-wrap gap-2">
          {PRESETS.map((p) => (
            <button key={p.label} className="btn-secondary !py-1" onClick={() => usePreset(p.days)}>
              {p.label}
            </button>
          ))}
        </div>
        <div className="flex flex-wrap items-end gap-4">
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-neutral-500">Start</span>
            <input
              type="date"
              value={draftStart}
              onChange={(e) => setDraftStart(e.target.value)}
              className="rounded-md border border-neutral-300 bg-white px-3 py-2 dark:border-neutral-700 dark:bg-neutral-800"
            />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-neutral-500">End</span>
            <input
              type="date"
              value={draftEnd}
              onChange={(e) => setDraftEnd(e.target.value)}
              className="rounded-md border border-neutral-300 bg-white px-3 py-2 dark:border-neutral-700 dark:bg-neutral-800"
            />
          </label>

          {tab === 'timeline' ? (
            <>
              <label className="flex flex-col gap-1 text-sm">
                <span className="text-neutral-500">Monitor</span>
                <select
                  value={draftMonitor}
                  onChange={(e) => setDraftMonitor(e.target.value)}
                  className="rounded-md border border-neutral-300 bg-white px-3 py-2 dark:border-neutral-700 dark:bg-neutral-800"
                >
                  {monitors.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="flex flex-col gap-1 text-sm">
                <span className="text-neutral-500">Granularity</span>
                <select
                  value={granularity}
                  onChange={(e) => setGranularity(e.target.value as TimelineGranularity)}
                  className="rounded-md border border-neutral-300 bg-white px-3 py-2 dark:border-neutral-700 dark:bg-neutral-800"
                >
                  <option value="hourly">Hourly</option>
                  <option value="daily">Daily</option>
                </select>
              </label>
            </>
          ) : (
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-neutral-500">Monitors (none = all)</span>
              <select
                multiple
                value={draftMonitorIds}
                onChange={(e) =>
                  setDraftMonitorIds(Array.from(e.target.selectedOptions).map((o) => o.value))
                }
                className="h-24 min-w-[200px] rounded-md border border-neutral-300 bg-white px-3 py-2 dark:border-neutral-700 dark:bg-neutral-800"
              >
                {monitors.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name}
                  </option>
                ))}
              </select>
            </label>
          )}

          <button className="btn-primary" onClick={apply}>
            Apply
          </button>
        </div>
      </div>

      {/* Timeline tab */}
      {tab === 'timeline' && (
        <>
          {tlError && (
            <div className="card border-error-300 p-4 text-error-700 dark:text-error-300">
              {tlError.message}
            </div>
          )}
          {!applied.monitor ? (
            <div className="card p-10 text-center text-neutral-500">Select a monitor.</div>
          ) : tlLoading ? (
            <div className="card animate-pulse p-10 text-center text-neutral-500">Loading…</div>
          ) : buckets.length === 0 ? (
            <div className="card p-10 text-center text-neutral-500">
              No data for this range.
            </div>
          ) : (
            <>
              <div className="card p-4">
                <h3 className="mb-3 font-semibold">Uptime Trend</h3>
                <div className="h-64">
                  <ResponsiveContainer width="100%" height="100%">
                    <AreaChart data={buckets}>
                      <CartesianGrid strokeDasharray="3 3" stroke="rgb(148 163 184 / 0.2)" />
                      <XAxis dataKey="timestamp" tickFormatter={tickFmt} stroke="rgb(148 163 184)" fontSize={12} />
                      <YAxis domain={[0, 100]} stroke="rgb(148 163 184)" fontSize={12} unit="%" />
                      <Tooltip
                        labelFormatter={(l) => tickFmt(String(l))}
                        formatter={(v: number, name) => [name === 'uptime_percent' ? `${v}%` : v, 'Uptime']}
                      />
                      <Area
                        type="monotone"
                        dataKey="uptime_percent"
                        stroke="#10b981"
                        fill="#10b981"
                        fillOpacity={0.2}
                        strokeWidth={2}
                      />
                    </AreaChart>
                  </ResponsiveContainer>
                </div>
              </div>

              <div className="card p-4">
                <h3 className="mb-3 font-semibold">Response Time</h3>
                <div className="h-64">
                  <ResponsiveContainer width="100%" height="100%">
                    <BarChart data={buckets}>
                      <CartesianGrid strokeDasharray="3 3" stroke="rgb(148 163 184 / 0.2)" />
                      <XAxis dataKey="timestamp" tickFormatter={tickFmt} stroke="rgb(148 163 184)" fontSize={12} />
                      <YAxis stroke="rgb(148 163 184)" fontSize={12} unit="ms" />
                      <Tooltip
                        labelFormatter={(l) => tickFmt(String(l))}
                        formatter={(v: number) => [`${v}ms`, 'Avg response']}
                      />
                      <Bar dataKey="avg_response_time_ms">
                        {buckets.map((b, i) => (
                          <Cell key={i} fill={respFill(b.avg_response_time_ms)} />
                        ))}
                      </Bar>
                    </BarChart>
                  </ResponsiveContainer>
                </div>
              </div>

              {tlStats && (
                <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
                  <div className="card p-4 text-center">
                    <div className={`text-2xl font-bold ${uptimeTextColor(tlStats.avgUptime)}`}>
                      {tlStats.avgUptime.toFixed(2)}%
                    </div>
                    <div className="text-xs text-neutral-500">Avg uptime</div>
                  </div>
                  <div className="card p-4 text-center">
                    <div className="text-2xl font-bold">{formatResponseTime(tlStats.avgResp)}</div>
                    <div className="text-xs text-neutral-500">Avg response</div>
                  </div>
                  <div className="card p-4 text-center">
                    <div className="text-2xl font-bold">{tlStats.totalChecks}</div>
                    <div className="text-xs text-neutral-500">Total checks</div>
                  </div>
                  <div className="card p-4 text-center">
                    <div className={`text-2xl font-bold ${tlStats.failed > 0 ? 'text-red-500' : ''}`}>
                      {tlStats.failed}
                    </div>
                    <div className="text-xs text-neutral-500">Failed checks</div>
                  </div>
                </div>
              )}
            </>
          )}
        </>
      )}

      {/* Summary tab */}
      {tab === 'summary' && (
        <>
          {smError && (
            <div className="card border-error-300 p-4 text-error-700 dark:text-error-300">
              {smError.message}
            </div>
          )}
          {smLoading ? (
            <div className="card animate-pulse p-10 text-center text-neutral-500">Loading…</div>
          ) : !summary || summary.monitors.length === 0 ? (
            <div className="card p-10 text-center text-neutral-500">No data for this range.</div>
          ) : (
            <>
              <div className="grid grid-cols-2 gap-4 lg:grid-cols-5">
                <div className="card p-4 text-center">
                  <div className={`text-2xl font-bold ${uptimeTextColor(summary.aggregate.avg_uptime)}`}>
                    {summary.aggregate.avg_uptime.toFixed(2)}%
                  </div>
                  <div className="text-xs text-neutral-500">Avg uptime</div>
                </div>
                <div className="card p-4 text-center">
                  <div className="text-2xl font-bold text-emerald-500">
                    {summary.aggregate.best_uptime.toFixed(2)}%
                  </div>
                  <div className="text-xs text-neutral-500">Best</div>
                </div>
                <div className="card p-4 text-center">
                  <div className="text-2xl font-bold text-red-500">
                    {summary.aggregate.worst_uptime.toFixed(2)}%
                  </div>
                  <div className="text-xs text-neutral-500">Worst</div>
                </div>
                <div className="card p-4 text-center">
                  <div className="text-2xl font-bold">{summary.aggregate.total_incidents}</div>
                  <div className="text-xs text-neutral-500">Incidents</div>
                </div>
                <div className="card p-4 text-center">
                  <div className="text-2xl font-bold">
                    {summary.aggregate.total_downtime_minutes.toFixed(0)}m
                  </div>
                  <div className="text-xs text-neutral-500">Downtime</div>
                </div>
              </div>

              <div className="card overflow-hidden">
                <div className="overflow-x-auto">
                  <table className="w-full text-left text-sm">
                    <thead className="border-b border-neutral-200 text-neutral-500 dark:border-neutral-800 dark:text-neutral-400">
                      <tr>
                        <th className="px-4 py-3 font-medium">
                          <button className="flex items-center gap-1" onClick={() => toggleSort('name')}>
                            Monitor <ArrowUpDown className="h-3 w-3" />
                          </button>
                        </th>
                        <th className="px-4 py-3 font-medium">Status</th>
                        <th className="px-4 py-3 font-medium">
                          <button className="flex items-center gap-1" onClick={() => toggleSort('uptime')}>
                            Uptime <ArrowUpDown className="h-3 w-3" />
                          </button>
                        </th>
                        <th className="px-4 py-3 font-medium">Downtime</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-neutral-100 dark:divide-neutral-800">
                      {sortedMonitors.map((m, i) => (
                        <tr key={m.monitor_id} className={i % 2 ? 'bg-neutral-50/50 dark:bg-neutral-800/30' : ''}>
                          <td className="px-4 py-3 font-medium">{m.monitor_name}</td>
                          <td className="px-4 py-3">
                            <span
                              className={`rounded-md px-2 py-1 text-xs font-medium ${
                                m.status === 'online'
                                  ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                                  : m.status === 'offline'
                                    ? 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
                                    : 'bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300'
                              }`}
                            >
                              {m.status}
                            </span>
                          </td>
                          <td className={`px-4 py-3 font-semibold ${uptimeTextColor(m.uptime_percent)}`}>
                            {m.uptime_percent.toFixed(2)}%
                          </td>
                          <td className="px-4 py-3 text-neutral-500">{m.downtime_minutes.toFixed(1)}m</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            </>
          )}
        </>
      )}
    </div>
  )
}
