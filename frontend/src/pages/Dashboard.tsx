import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  RadialBarChart,
  RadialBar,
  PolarAngleAxis,
  ResponsiveContainer,
  LineChart,
  Line,
} from 'recharts'
import {
  Activity,
  CheckCircle2,
  XCircle,
  Gauge,
  RefreshCw,
  Plus,
  Play,
  ExternalLink,
  AlertTriangle,
  Wrench,
} from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { useMonitors, useTestMonitor } from '@/hooks/useMonitors'
import { formatResponseTime, formatDate } from '@/utils/formatters'
import type { Monitor } from '@/types'

const REFRESH_MS = 30_000

// Static placeholder sparkline until per-monitor 24h series is available.
const sparkData = [98, 99, 97, 100, 99, 96, 100, 99].map((v, i) => ({ i, v }))

function responseColor(ms: number): string {
  if (ms <= 0) return 'text-neutral-400'
  if (ms < 200) return 'text-emerald-500'
  if (ms <= 500) return 'text-amber-500'
  return 'text-red-500'
}

function gaugeColor(pct: number): string {
  if (pct < 90) return '#ef4444'
  if (pct < 99) return '#f59e0b'
  return '#10b981'
}

interface Toast {
  id: number
  message: string
  ok: boolean
}

function StatTile({
  label,
  value,
  icon: Icon,
  tone,
}: {
  label: string
  value: string | number
  icon: typeof Activity
  tone: string
}) {
  return (
    <div className="card flex items-center gap-4 p-5">
      <div className={`rounded-lg p-3 ${tone}`}>
        <Icon className="h-6 w-6" />
      </div>
      <div>
        <div className="text-2xl font-bold">{value}</div>
        <div className="text-sm text-neutral-500 dark:text-neutral-400">{label}</div>
      </div>
    </div>
  )
}

function AvailabilityGauge({ pct }: { pct: number }) {
  const color = gaugeColor(pct)
  const data = [{ name: 'availability', value: pct, fill: color }]
  return (
    <div className="relative h-48">
      <ResponsiveContainer width="100%" height="100%">
        <RadialBarChart
          innerRadius="72%"
          outerRadius="100%"
          data={data}
          startAngle={90}
          endAngle={-270}
        >
          <PolarAngleAxis type="number" domain={[0, 100]} angleAxisId={0} tick={false} />
          <RadialBar background dataKey="value" cornerRadius={8} angleAxisId={0} />
        </RadialBarChart>
      </ResponsiveContainer>
      <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
        <span className="text-3xl font-bold" style={{ color }}>
          {pct.toFixed(1)}%
        </span>
        <span className="text-xs text-neutral-500 dark:text-neutral-400">online now</span>
      </div>
    </div>
  )
}

function MonitorCard({
  monitor,
  onDetails,
  onTest,
  testing,
}: {
  monitor: Monitor
  onDetails: (id: string) => void
  onTest: (id: string, name: string) => void
  testing: boolean
}) {
  const inMaintenance = monitor.is_in_maintenance ?? false
  const online = monitor.current_status === 'online'
  const offline = monitor.current_status === 'offline'

  return (
    <div
      onClick={() => onDetails(monitor.id)}
      className={`card relative cursor-pointer p-5 transition duration-150 hover:scale-[1.02] hover:shadow-card-hover ${
        inMaintenance ? 'opacity-90 ring-1 ring-amber-300 dark:ring-amber-700' : ''
      }`}
    >
      {inMaintenance && (
        <div className="absolute right-2 top-2 flex items-center gap-1 rounded-full bg-amber-100 px-2.5 py-0.5 text-xs font-medium text-amber-900 dark:bg-amber-900/50 dark:text-amber-200">
          <Wrench className="h-3 w-3" /> In Maintenance ({monitor.time_remaining_minutes ?? 0}m)
        </div>
      )}
      <div className="flex items-start justify-between">
        <div className="min-w-0">
          <div className="truncate font-semibold">{monitor.name}</div>
          <div className="truncate text-sm text-neutral-500 dark:text-neutral-400">
            {monitor.url}
          </div>
        </div>
        {!inMaintenance && (
          <span className="shrink-0 rounded-md bg-neutral-100 px-2 py-1 text-xs font-semibold uppercase text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400">
            {monitor.type}
          </span>
        )}
      </div>

      <div className="mt-4 flex items-center justify-between">
        {inMaintenance ? (
          <span className="flex items-center gap-1.5 text-sm font-medium text-amber-600 dark:text-amber-400">
            <span className="h-2 w-2 rounded-full bg-amber-500" />
            Maintenance in progress
          </span>
        ) : (
          <span
            className={`flex items-center gap-1.5 text-sm font-medium ${
              online ? 'text-emerald-500' : offline ? 'text-red-500' : 'text-neutral-400'
            }`}
          >
            <span
              className={`h-2 w-2 rounded-full ${
                online ? 'bg-emerald-500' : offline ? 'bg-red-500 animate-pulse' : 'bg-neutral-400'
              }`}
            />
            {online ? 'Online' : offline ? 'Offline' : 'Unknown'}
          </span>
        )}
        <span className={`text-sm font-medium ${responseColor(monitor.last_response_time_ms)}`}>
          {formatResponseTime(monitor.last_response_time_ms)}
        </span>
      </div>

      <div className="mt-3 h-8">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={sparkData}>
            <Line
              type="monotone"
              dataKey="v"
              stroke={online ? '#10b981' : offline ? '#ef4444' : '#94a3b8'}
              strokeWidth={2}
              dot={false}
            />
          </LineChart>
        </ResponsiveContainer>
      </div>

      <div className="mt-3 flex items-center justify-between">
        <span className="text-xs text-neutral-500 dark:text-neutral-400">
          {monitor.last_check_at ? formatDate(monitor.last_check_at) : 'Never'}
        </span>
        <div className="flex gap-1" onClick={(e) => e.stopPropagation()}>
          <button
            className="btn-secondary !px-2 !py-1"
            onClick={() => onTest(monitor.id, monitor.name)}
            disabled={testing}
            title="Test now"
          >
            <Play className="h-4 w-4" />
          </button>
          <button
            className="btn-secondary !px-2 !py-1"
            onClick={() => onDetails(monitor.id)}
            title="Details"
          >
            <ExternalLink className="h-4 w-4" />
          </button>
        </div>
      </div>
    </div>
  )
}

function SkeletonCard() {
  return (
    <div className="card animate-pulse p-5">
      <div className="h-4 w-2/3 rounded bg-neutral-200 dark:bg-neutral-800" />
      <div className="mt-2 h-3 w-1/2 rounded bg-neutral-200 dark:bg-neutral-800" />
      <div className="mt-6 h-8 rounded bg-neutral-200 dark:bg-neutral-800" />
      <div className="mt-4 h-3 w-1/3 rounded bg-neutral-200 dark:bg-neutral-800" />
    </div>
  )
}

export default function Dashboard() {
  const navigate = useNavigate()
  const { monitors, loading, error, refetch } = useMonitors()
  const { test } = useTestMonitor()
  const [updatedAt, setUpdatedAt] = useState<Date>(new Date())
  const [, setTick] = useState(0)
  const [toasts, setToasts] = useState<Toast[]>([])
  const [testingId, setTestingId] = useState<string | null>(null)

  // Refresh timestamp whenever data changes; tick every second for the label.
  useEffect(() => {
    setUpdatedAt(new Date())
  }, [monitors])
  useEffect(() => {
    const t = window.setInterval(() => setTick((x) => x + 1), 1000)
    return () => window.clearInterval(t)
  }, [])
  // Auto-refresh the monitor list.
  useEffect(() => {
    const t = window.setInterval(() => void refetch(), REFRESH_MS)
    return () => window.clearInterval(t)
  }, [refetch])

  const stats = useMemo(() => {
    const total = monitors.length
    const online = monitors.filter((m) => m.current_status === 'online').length
    const offline = monitors.filter((m) => m.current_status === 'offline').length
    const responders = monitors.filter((m) => m.last_response_time_ms > 0)
    const avg =
      responders.length > 0
        ? Math.round(
            responders.reduce((s, m) => s + m.last_response_time_ms, 0) / responders.length
          )
        : 0
    const availability = total > 0 ? (online / total) * 100 : 100
    return { total, online, offline, avg, availability }
  }, [monitors])

  const activeIncidents = useMemo(
    () => monitors.filter((m) => m.current_status === 'offline').slice(0, 5),
    [monitors]
  )

  const pushToast = useCallback((message: string, ok: boolean) => {
    const id = Date.now() + Math.random()
    setToasts((t) => [...t, { id, message, ok }])
    window.setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4000)
  }, [])

  const handleTest = useCallback(
    async (id: string, name: string) => {
      setTestingId(id)
      try {
        const check = await test(id)
        pushToast(`${name}: ${check.status} (${check.response_time_ms}ms)`, check.status === 'success')
        await refetch()
      } catch {
        pushToast(`${name}: test failed`, false)
      } finally {
        setTestingId(null)
      }
    },
    [test, pushToast, refetch]
  )

  const goToDetails = useCallback((id: string) => navigate(`/monitors/${id}`), [navigate])

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold">Dashboard</h1>
          <p className="text-sm text-neutral-500 dark:text-neutral-400">
            Last updated: {formatDistanceToNow(updatedAt, { addSuffix: true })}
          </p>
        </div>
        <button className="btn-secondary" onClick={() => void refetch()} disabled={loading}>
          <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </button>
      </div>

      {/* Error state */}
      {error && (
        <div className="card flex items-center justify-between border-error-300 p-4">
          <span className="text-error-700 dark:text-error-300">
            Failed to load monitors: {error.message}
          </span>
          <button className="btn-secondary" onClick={() => void refetch()}>
            Retry
          </button>
        </div>
      )}

      {/* Stat tiles + gauge */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:col-span-2">
          <StatTile
            label="Total Monitors"
            value={stats.total}
            icon={Activity}
            tone="bg-info-100 text-info-600 dark:bg-info-900/40 dark:text-info-400"
          />
          <StatTile
            label="Online"
            value={stats.online}
            icon={CheckCircle2}
            tone="bg-primary-100 text-primary-600 dark:bg-primary-900/40 dark:text-primary-400"
          />
          <StatTile
            label="Offline"
            value={stats.offline}
            icon={XCircle}
            tone="bg-error-100 text-error-600 dark:bg-error-900/40 dark:text-error-400"
          />
          <StatTile
            label="Avg Response"
            value={formatResponseTime(stats.avg)}
            icon={Gauge}
            tone="bg-warning-100 text-warning-600 dark:bg-warning-900/40 dark:text-warning-400"
          />
        </div>
        <div className="card p-5">
          <div className="mb-1 text-sm font-medium text-neutral-500 dark:text-neutral-400">
            Availability
          </div>
          <AvailabilityGauge pct={stats.availability} />
        </div>
      </div>

      {/* Active incidents */}
      {activeIncidents.length > 0 && (
        <div className="card p-5">
          <div className="mb-3 flex items-center gap-2 font-semibold">
            <AlertTriangle className="h-4 w-4 text-error-500" /> Active Incidents
          </div>
          <div className="divide-y divide-neutral-100 dark:divide-neutral-800">
            {activeIncidents.map((m) => (
              <div key={m.id} className="flex items-center justify-between py-2 text-sm">
                <span className="font-medium">{m.name}</span>
                <span className="text-neutral-500">
                  Down since{' '}
                  {m.last_check_at ? formatDistanceToNow(new Date(m.last_check_at), { addSuffix: true }) : 'unknown'}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Monitor grid */}
      {loading && monitors.length === 0 ? (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <SkeletonCard key={i} />
          ))}
        </div>
      ) : monitors.length === 0 ? (
        <div className="card flex flex-col items-center gap-3 p-12 text-center">
          <div className="text-lg font-semibold">No monitors yet</div>
          <p className="text-sm text-neutral-500 dark:text-neutral-400">
            Create your first monitor to start tracking uptime.
          </p>
          <button className="btn-primary" onClick={() => navigate('/monitors')}>
            <Plus className="h-4 w-4" />
            Create Your First Monitor
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
          {monitors.map((m) => (
            <MonitorCard
              key={m.id}
              monitor={m}
              onDetails={goToDetails}
              onTest={handleTest}
              testing={testingId === m.id}
            />
          ))}
        </div>
      )}

      {/* Toasts */}
      <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
        {toasts.map((t) => (
          <div
            key={t.id}
            className={`rounded-md px-4 py-2 text-sm text-white shadow-card ${
              t.ok ? 'bg-emerald-600' : 'bg-red-600'
            }`}
          >
            {t.message}
          </div>
        ))}
      </div>
    </div>
  )
}
