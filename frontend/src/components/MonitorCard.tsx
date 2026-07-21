import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  ChevronDown,
  ChevronRight,
  Play,
  Pause,
  Pencil,
  Trash2,
  Loader2,
  Wrench,
} from 'lucide-react'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts'
import { formatDistanceToNow } from 'date-fns'
import api from '@/services/api'
import {
  usePauseMonitor,
  useResumeMonitor,
  useDeleteMonitor,
  useTestMonitor,
} from '@/hooks/useMonitors'
import { useMoveMonitorToGroup } from '@/hooks/useMonitorGroups'
import { formatResponseTime, formatDatetime } from '@/utils/formatters'
import type { Monitor, MonitorGroup } from '@/types'

function responseColor(ms: number): string {
  if (ms <= 0) return 'text-neutral-400'
  if (ms < 200) return 'text-emerald-500'
  if (ms <= 500) return 'text-amber-500'
  return 'text-red-500'
}
function uptimeColor(pct: number): string {
  if (pct >= 95) return 'text-emerald-500'
  if (pct >= 80) return 'text-amber-500'
  return 'text-red-500'
}

interface TimelinePoint {
  timestamp: string
  avg_response_time_ms: number
}
interface IncidentRow {
  id: string
  start_time: string
  end_time: string | null
  duration_seconds: number
}
interface Insights {
  u24: number
  u7: number
  u30: number
  avg24: number
  series: TimelinePoint[]
  incidents: IncidentRow[]
}

const iso = (d: Date) => d.toISOString()

// useInsights lazily loads the expanded-panel data (uptime windows, a 24h
// response-time series, and recent incidents) only once the card is expanded.
function useInsights(monitorID: string, enabled: boolean) {
  const [data, setData] = useState<Insights | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!enabled) return
    let active = true
    setLoading(true)
    const now = new Date()
    const h24 = new Date(now.getTime() - 24 * 3600e3)
    const d7 = new Date(now.getTime() - 7 * 864e5)
    const d30 = new Date(now.getTime() - 30 * 864e5)
    type Report = { data: { uptime: { uptime_percentage: number }; checks: { avg_response_time_ms: number } } }
    Promise.all([
      api.get<Report>(`/monitors/${monitorID}/report`, { params: { start_time: iso(h24), end_time: iso(now) } }),
      api.get<Report>(`/monitors/${monitorID}/report`, { params: { start_time: iso(d7), end_time: iso(now) } }),
      api.get<Report>(`/monitors/${monitorID}/report`, { params: { start_time: iso(d30), end_time: iso(now) } }),
      api.get<{ data: { timeline: TimelinePoint[] } }>('/reports/timeline', {
        params: { monitor_id: monitorID, start: iso(h24), end: iso(now), granularity: 'hourly' },
      }),
      api.get<{ data: { incidents: IncidentRow[] } }>(`/monitors/${monitorID}/incidents`, {
        params: { start_time: iso(d30), end_time: iso(now) },
      }),
    ])
      .then(([r24, r7, r30, tl, inc]) => {
        if (!active) return
        setData({
          u24: r24.data.data.uptime.uptime_percentage,
          u7: r7.data.data.uptime.uptime_percentage,
          u30: r30.data.data.uptime.uptime_percentage,
          avg24: r24.data.data.checks.avg_response_time_ms,
          series: tl.data.data.timeline ?? [],
          incidents: inc.data.data.incidents ?? [],
        })
      })
      .catch(() => active && setData(null))
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [monitorID, enabled])

  return { data, loading }
}

interface Props {
  monitor: Monitor
  uptime24h: number | null
  expanded: boolean
  groups: MonitorGroup[]
  onToggle: (id: string) => void
  onChanged: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

function Metric({ label, value, tone }: { label: string; value: string; tone?: string }) {
  return (
    <div className="rounded-md bg-neutral-50 p-2 dark:bg-neutral-800/50">
      <div className="text-xs text-neutral-500 dark:text-neutral-400">{label}</div>
      <div className={`text-sm font-semibold ${tone ?? ''}`}>{value}</div>
    </div>
  )
}

export default function MonitorCard({
  monitor,
  uptime24h,
  expanded,
  groups,
  onToggle,
  onChanged,
  push,
}: Props) {
  const navigate = useNavigate()
  const { pause, loading: pausing } = usePauseMonitor(monitor.id)
  const { resume, loading: resuming } = useResumeMonitor(monitor.id)
  const { delete: del, loading: deleting } = useDeleteMonitor(monitor.id)
  const { test, loading: testing } = useTestMonitor(monitor.id)
  const { move } = useMoveMonitorToGroup()
  const { data: insights, loading: insightsLoading } = useInsights(monitor.id, expanded)
  const [confirmDelete, setConfirmDelete] = useState(false)

  const inMaintenance = monitor.is_in_maintenance ?? false
  const online = monitor.current_status === 'online'
  const offline = monitor.current_status === 'offline'
  const busy = pausing || resuming || deleting || testing

  const act = async (fn: () => Promise<unknown>, okMsg: string) => {
    try {
      await fn()
      push(okMsg, 'success')
      onChanged()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Action failed', 'error')
    }
  }

  const chartData = (insights?.series ?? []).map((p) => ({
    t: new Date(p.timestamp).toLocaleTimeString([], { hour: '2-digit' }),
    ms: p.avg_response_time_ms,
  }))

  return (
    <div className="overflow-hidden rounded-md border border-neutral-200 bg-white transition hover:shadow-card-hover dark:border-neutral-800 dark:bg-neutral-900">
      {/* ---- Collapsed row (click to expand) ---- */}
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
        <div className="hidden w-40 shrink-0 text-right sm:block">
          <div className={`text-sm font-medium ${responseColor(monitor.last_response_time_ms)}`}>
            {formatResponseTime(monitor.last_response_time_ms)}
          </div>
          <div className="text-xs text-neutral-500 dark:text-neutral-400">
            {monitor.last_check_at ? `${formatDistanceToNow(new Date(monitor.last_check_at))} ago` : 'never'}
          </div>
        </div>

        {/* RIGHT: uptime */}
        <div className="w-24 shrink-0 text-right">
          <div className={`text-2xl font-bold leading-none ${uptime24h != null ? uptimeColor(uptime24h) : 'text-neutral-400'}`}>
            {uptime24h != null ? `${uptime24h.toFixed(1)}%` : '—'}
          </div>
          <div className="text-[10px] uppercase tracking-wide text-neutral-400">24h uptime</div>
        </div>

        {/* Expand chevron */}
        {expanded ? (
          <ChevronDown className="h-5 w-5 shrink-0 text-neutral-400" />
        ) : (
          <ChevronRight className="h-5 w-5 shrink-0 text-neutral-400" />
        )}
      </button>

      {/* ---- Expanded detail panel ---- */}
      {expanded && (
        <div className="space-y-4 border-t border-neutral-200 p-4 dark:border-neutral-800">
          {/* Top row: name + status + actions */}
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <span className="text-lg font-semibold">{monitor.name}</span>
              <span
                className={`rounded-md px-2 py-0.5 text-xs font-medium ${
                  inMaintenance
                    ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
                    : online
                      ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                      : offline
                        ? 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
                        : 'bg-neutral-100 text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400'
                }`}
              >
                {inMaintenance ? 'Maintenance' : online ? 'Online' : offline ? 'Offline' : 'Unknown'}
              </span>
            </div>
            <div className="flex flex-wrap gap-1.5">
              <button className="btn-secondary !py-1" disabled={busy} onClick={() => void act(() => test(), 'Test complete')}>
                <Play className="h-4 w-4" /> Test
              </button>
              {monitor.enabled ? (
                <button className="btn-secondary !py-1" disabled={busy} onClick={() => void act(() => pause(), 'Monitor paused')}>
                  <Pause className="h-4 w-4" /> Pause
                </button>
              ) : (
                <button className="btn-secondary !py-1" disabled={busy} onClick={() => void act(() => resume(), 'Monitor resumed')}>
                  <Play className="h-4 w-4" /> Resume
                </button>
              )}
              <button className="btn-secondary !py-1" onClick={() => navigate(`/monitors/${monitor.id}/edit`)}>
                <Pencil className="h-4 w-4" /> Edit
              </button>
              <button
                className="btn !py-1 border border-error-300 text-error-600 hover:bg-error-50 dark:hover:bg-error-900/20"
                disabled={busy}
                onClick={() => setConfirmDelete(true)}
              >
                <Trash2 className="h-4 w-4" /> Delete
              </button>
            </div>
          </div>

          {confirmDelete && (
            <div className="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-800 dark:bg-amber-900/30">
              <p className="mb-2 text-amber-800 dark:text-amber-200">Delete “{monitor.name}” and all its history?</p>
              <div className="flex justify-end gap-2">
                <button className="btn-secondary !py-1" onClick={() => setConfirmDelete(false)}>
                  Cancel
                </button>
                <button
                  className="btn bg-error-600 !py-1 text-white hover:bg-error-700"
                  disabled={deleting}
                  onClick={() =>
                    void act(async () => {
                      await del()
                      setConfirmDelete(false)
                    }, 'Monitor deleted')
                  }
                >
                  {deleting ? 'Deleting…' : 'Delete'}
                </button>
              </div>
            </div>
          )}

          {/* Metrics */}
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <Metric label="Response (now)" value={formatResponseTime(monitor.last_response_time_ms)} tone={responseColor(monitor.last_response_time_ms)} />
            <Metric label="Avg (24h)" value={insights ? formatResponseTime(insights.avg24) : '—'} />
            <Metric label="Check interval" value={`every ${monitor.interval_seconds}s`} />
            <Metric label="Last check" value={monitor.last_check_at ? formatDatetime(monitor.last_check_at) : 'Never'} />
            <Metric label="Uptime 24h" value={insights ? `${insights.u24.toFixed(2)}%` : '—'} tone={insights ? uptimeColor(insights.u24) : ''} />
            <Metric label="Uptime 7d" value={insights ? `${insights.u7.toFixed(2)}%` : '—'} tone={insights ? uptimeColor(insights.u7) : ''} />
            <Metric label="Uptime 30d" value={insights ? `${insights.u30.toFixed(2)}%` : '—'} tone={insights ? uptimeColor(insights.u30) : ''} />
            <Metric label="Group" value={groups.find((g) => g.id === monitor.group_id)?.name ?? 'Ungrouped'} />
          </div>

          {/* Response chart (24h) */}
          <div>
            <div className="mb-1 text-xs font-medium text-neutral-500 dark:text-neutral-400">Response time (24h)</div>
            <div className="h-40">
              {insightsLoading ? (
                <div className="flex h-full items-center justify-center text-neutral-400">
                  <Loader2 className="h-5 w-5 animate-spin" />
                </div>
              ) : chartData.length > 0 ? (
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={chartData} margin={{ top: 5, right: 10, bottom: 0, left: -10 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#e2e8f0" strokeOpacity={0.4} />
                    <XAxis dataKey="t" tick={{ fontSize: 10 }} interval="preserveStartEnd" />
                    <YAxis tick={{ fontSize: 10 }} width={40} />
                    <Tooltip formatter={(v: number) => [`${v} ms`, 'avg']} />
                    <Line type="monotone" dataKey="ms" stroke="#10b981" strokeWidth={2} dot={false} />
                  </LineChart>
                </ResponsiveContainer>
              ) : (
                <div className="flex h-full items-center justify-center text-sm text-neutral-400">
                  No response data in the last 24h.
                </div>
              )}
            </div>
          </div>

          {/* Tags + move-to-group */}
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-xs text-neutral-500 dark:text-neutral-400">Tags:</span>
              {monitor.tags && monitor.tags.length > 0 ? (
                monitor.tags.map((t) => (
                  <span key={t} className="rounded-full bg-primary-100 px-2 py-0.5 text-xs text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
                    {t}
                  </span>
                ))
              ) : (
                <span className="text-xs text-neutral-400">none</span>
              )}
            </div>
            <label className="flex items-center gap-2 text-xs">
              <span className="text-neutral-500 dark:text-neutral-400">Group:</span>
              <select
                className="rounded-md border border-neutral-300 bg-white px-2 py-1 dark:border-neutral-700 dark:bg-neutral-800"
                value={monitor.group_id ?? ''}
                onChange={(e) => void act(() => move(monitor.id, e.target.value || null), 'Monitor group updated')}
              >
                <option value="">Ungrouped</option>
                {groups.map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.name}
                  </option>
                ))}
              </select>
            </label>
          </div>

          {/* Incident history */}
          <div>
            <div className="mb-1 text-xs font-medium text-neutral-500 dark:text-neutral-400">Recent incidents</div>
            {insights && insights.incidents.length > 0 ? (
              <div className="divide-y divide-neutral-100 dark:divide-neutral-800">
                {insights.incidents.slice(0, 3).map((inc) => (
                  <div key={inc.id} className="flex items-center justify-between py-1.5 text-xs">
                    <span className="flex items-center gap-1.5">
                      <Wrench className="h-3 w-3 text-red-400" />
                      {formatDatetime(inc.start_time)}
                    </span>
                    <span className="text-neutral-500 dark:text-neutral-400">
                      {inc.end_time ? `${Math.max(1, Math.round(inc.duration_seconds / 60))} min` : 'ongoing'}
                    </span>
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-xs text-neutral-400">No incidents in the last 30 days.</p>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
