import { Activity, CheckCircle2, XCircle, Gauge } from 'lucide-react'
import { useMonitors } from '@/hooks/useMonitors'
import { formatMs, formatRelative, statusBadge } from '@/utils/format'
import type { Monitor } from '@/types'

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

function MonitorCard({ monitor }: { monitor: Monitor }) {
  return (
    <div className="card p-5 transition-shadow hover:shadow-card-hover">
      <div className="flex items-start justify-between">
        <div className="min-w-0">
          <div className="truncate font-semibold">{monitor.name}</div>
          <div className="truncate text-sm text-neutral-500 dark:text-neutral-400">
            {monitor.url}
          </div>
        </div>
        <span
          className={`shrink-0 rounded-md px-2 py-1 text-xs font-medium ${statusBadge(
            monitor.current_status
          )}`}
        >
          {monitor.current_status}
        </span>
      </div>
      <div className="mt-4 flex justify-between text-sm text-neutral-500 dark:text-neutral-400">
        <span>{formatMs(monitor.last_response_time_ms)}</span>
        <span>{formatRelative(monitor.last_check_at)}</span>
      </div>
    </div>
  )
}

export default function Dashboard() {
  const { monitors, loading, error } = useMonitors()

  const total = monitors.length
  const online = monitors.filter((m) => m.current_status === 'online').length
  const offline = monitors.filter((m) => m.current_status === 'offline').length
  const responders = monitors.filter((m) => m.last_response_time_ms > 0)
  const avgResponse =
    responders.length > 0
      ? Math.round(
          responders.reduce((sum, m) => sum + m.last_response_time_ms, 0) /
            responders.length
        )
      : 0

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <p className="text-sm text-neutral-500 dark:text-neutral-400">
          System health at a glance
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile
          label="Total Monitors"
          value={total}
          icon={Activity}
          tone="bg-info-100 text-info-600 dark:bg-info-900/40 dark:text-info-400"
        />
        <StatTile
          label="Online"
          value={online}
          icon={CheckCircle2}
          tone="bg-primary-100 text-primary-600 dark:bg-primary-900/40 dark:text-primary-400"
        />
        <StatTile
          label="Offline"
          value={offline}
          icon={XCircle}
          tone="bg-error-100 text-error-600 dark:bg-error-900/40 dark:text-error-400"
        />
        <StatTile
          label="Avg Response"
          value={formatMs(avgResponse)}
          icon={Gauge}
          tone="bg-warning-100 text-warning-600 dark:bg-warning-900/40 dark:text-warning-400"
        />
      </div>

      {error && (
        <div className="card border-error-300 p-4 text-error-700 dark:text-error-300">
          Failed to load monitors: {error.message}
        </div>
      )}

      {loading ? (
        <div className="text-neutral-500 dark:text-neutral-400">Loading monitors…</div>
      ) : total === 0 ? (
        <div className="card p-10 text-center text-neutral-500 dark:text-neutral-400">
          No monitors yet. Create one to get started.
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
          {monitors.map((m) => (
            <MonitorCard key={m.id} monitor={m} />
          ))}
        </div>
      )}
    </div>
  )
}
