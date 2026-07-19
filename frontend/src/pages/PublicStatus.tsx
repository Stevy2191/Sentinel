import { useMemo } from 'react'
import { useParams } from 'react-router-dom'
import {
  ShieldCheck,
  CheckCircle2,
  XCircle,
  Gauge,
  Activity,
  AlertTriangle,
} from 'lucide-react'
import { formatDistanceToNow, format, parseISO } from 'date-fns'
import { usePublicStatusPage } from '@/hooks/usePublicStatus'
import { formatResponseTime, formatDowntime } from '@/utils/formatters'
import type { PublicMonitor } from '@/types'

const DEFAULT_THEME = '#10b981'

function uptimeColor(pct: number): string {
  if (pct >= 99) return '#10b981'
  if (pct >= 95) return '#f59e0b'
  return '#ef4444'
}

function relative(iso: string | null | undefined): string {
  if (!iso) return 'never'
  try {
    return formatDistanceToNow(parseISO(iso), { addSuffix: true })
  } catch {
    return '—'
  }
}

function StatTile({
  label,
  value,
  sub,
  icon: Icon,
  tone,
}: {
  label: string
  value: string
  sub?: string
  icon: typeof Activity
  tone: string
}) {
  return (
    <div className="rounded-lg border border-neutral-200 bg-white p-5 dark:border-neutral-800 dark:bg-neutral-900">
      <div className="flex items-center gap-3">
        <div className={`rounded-lg p-2 ${tone}`}>
          <Icon className="h-5 w-5" />
        </div>
        <div>
          <div className="text-xl font-bold">{value}</div>
          <div className="text-xs text-neutral-500 dark:text-neutral-400">
            {label}
            {sub ? ` · ${sub}` : ''}
          </div>
        </div>
      </div>
    </div>
  )
}

function UptimeBar({ label, pct }: { label: string; pct: number }) {
  return (
    <div>
      <div className="flex justify-between text-xs text-neutral-500 dark:text-neutral-400">
        <span>{label}</span>
        <span className="font-medium" style={{ color: uptimeColor(pct) }}>
          {pct.toFixed(2)}%
        </span>
      </div>
      <div className="mt-1 h-1.5 overflow-hidden rounded bg-neutral-200 dark:bg-neutral-700">
        <div
          className="h-full rounded"
          style={{ width: `${Math.max(0, Math.min(100, pct))}%`, backgroundColor: uptimeColor(pct) }}
        />
      </div>
    </div>
  )
}

function MonitorCard({ m }: { m: PublicMonitor }) {
  const online = m.status === 'online'
  const offline = m.status === 'offline'
  return (
    <div className="rounded-lg border border-neutral-200 bg-white p-5 dark:border-neutral-800 dark:bg-neutral-900">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 font-semibold">{m.name}</div>
        <span
          className={`flex shrink-0 items-center gap-1.5 text-sm font-medium ${
            online ? 'text-emerald-500' : offline ? 'text-red-500' : 'text-neutral-400'
          }`}
        >
          <span
            className={`h-2 w-2 rounded-full ${
              online ? 'bg-emerald-500 animate-pulse' : offline ? 'bg-red-500 animate-pulse' : 'bg-neutral-400'
            }`}
          />
          {online ? 'Online' : offline ? 'Offline' : 'Unknown'}
        </span>
      </div>

      <div className="mt-2 flex justify-between text-xs text-neutral-500 dark:text-neutral-400">
        <span>{relative(m.last_check)}</span>
        <span>{formatResponseTime(m.response_time_ms)}</span>
      </div>

      <div className="mt-4 space-y-2">
        <UptimeBar label="Last 7 days" pct={m.uptime.last_7_days} />
        <UptimeBar label="Last 30 days" pct={m.uptime.last_30_days} />
        <UptimeBar label="Last 90 days" pct={m.uptime.last_90_days} />
      </div>
    </div>
  )
}

function Skeleton() {
  return (
    <div className="mx-auto max-w-4xl space-y-6 p-6">
      <div className="h-8 w-1/3 animate-pulse rounded bg-neutral-200 dark:bg-neutral-800" />
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="h-20 animate-pulse rounded-lg bg-neutral-200 dark:bg-neutral-800" />
        ))}
      </div>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {Array.from({ length: 6 }).map((_, i) => (
          <div key={i} className="h-40 animate-pulse rounded-lg bg-neutral-200 dark:bg-neutral-800" />
        ))}
      </div>
    </div>
  )
}

export default function PublicStatus() {
  const { slug } = useParams<{ slug: string }>()
  const { page, monitors, summary, loading, error } = usePublicStatusPage(slug)

  const groups = useMemo(() => {
    const map = new Map<string, PublicMonitor[]>()
    for (const m of monitors) {
      const key = m.group || ''
      if (!map.has(key)) map.set(key, [])
      map.get(key)!.push(m)
    }
    return Array.from(map.entries())
  }, [monitors])

  const derived = useMemo(() => {
    const total = summary?.total_monitors ?? monitors.length
    const online = summary?.online ?? 0
    const offline = summary?.offline ?? 0
    const responders = monitors.filter((m) => m.response_time_ms > 0)
    const avgResp = responders.length
      ? Math.round(responders.reduce((s, m) => s + m.response_time_ms, 0) / responders.length)
      : 0
    const avg7 = monitors.length
      ? monitors.reduce((s, m) => s + m.uptime.last_7_days, 0) / monitors.length
      : 100
    return { total, online, offline, avgResp, avg7 }
  }, [summary, monitors])

  // Aggregate the most recent incidents across all monitors.
  const recentIncidents = useMemo(() => {
    const all = monitors.flatMap((m) =>
      m.recent_incidents.map((inc) => ({ ...inc, monitorName: m.name }))
    )
    all.sort((a, b) => b.start.localeCompare(a.start))
    return all.slice(0, 5)
  }, [monitors])

  if (loading) return <Skeleton />

  if (error || !page) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-neutral-50 p-6 dark:bg-neutral-950">
        <div className="text-center">
          <ShieldCheck className="mx-auto mb-3 h-10 w-10 text-neutral-400" />
          <h1 className="text-xl font-semibold">This status page is not available</h1>
          <p className="mt-1 text-sm text-neutral-500">
            It may be private or may not exist.
          </p>
        </div>
      </div>
    )
  }

  const theme = page.theme_color || DEFAULT_THEME
  const allOperational = derived.offline === 0

  return (
    <div className="min-h-screen bg-neutral-50 dark:bg-neutral-950">
      <div className="h-1.5" style={{ backgroundColor: theme }} />
      <div className="mx-auto max-w-4xl space-y-8 p-6">
        {/* Branding */}
        <header className="text-center">
          <div className="mb-2 flex items-center justify-center gap-3">
            {page.logo_url ? (
              <img src={page.logo_url} alt="" className="h-10 w-10 rounded object-contain" />
            ) : (
              <ShieldCheck className="h-8 w-8" style={{ color: theme }} />
            )}
            <h1 className="text-3xl font-bold">{page.name}</h1>
          </div>
          {page.description && (
            <p className="text-neutral-500 dark:text-neutral-400">{page.description}</p>
          )}
          <p className="mt-1 text-xs text-neutral-400">
            Last updated {relative(summary?.last_updated ?? page.updated_at)}
          </p>
        </header>

        {/* Overall banner */}
        <div
          className={`rounded-lg p-4 text-center font-medium ${
            allOperational
              ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-200'
              : 'bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-200'
          }`}
        >
          {allOperational
            ? 'All systems operational'
            : `${derived.offline} of ${derived.total} systems experiencing issues`}
        </div>

        {/* Summary tiles */}
        <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
          <StatTile
            label="Online"
            value={String(derived.online)}
            sub={derived.total ? `${Math.round((derived.online / derived.total) * 100)}%` : '—'}
            icon={CheckCircle2}
            tone="bg-emerald-100 text-emerald-600 dark:bg-emerald-900/40 dark:text-emerald-400"
          />
          <StatTile
            label="Offline"
            value={String(derived.offline)}
            sub={derived.total ? `${Math.round((derived.offline / derived.total) * 100)}%` : '—'}
            icon={XCircle}
            tone="bg-red-100 text-red-600 dark:bg-red-900/40 dark:text-red-400"
          />
          <StatTile
            label="Avg Response"
            value={formatResponseTime(derived.avgResp)}
            icon={Gauge}
            tone="bg-amber-100 text-amber-600 dark:bg-amber-900/40 dark:text-amber-400"
          />
          <StatTile
            label="Uptime"
            value={`${derived.avg7.toFixed(2)}%`}
            sub="7 days"
            icon={Activity}
            tone="bg-blue-100 text-blue-600 dark:bg-blue-900/40 dark:text-blue-400"
          />
        </div>

        {/* Monitors grouped */}
        {monitors.length === 0 ? (
          <div className="rounded-lg border border-neutral-200 p-10 text-center text-neutral-500 dark:border-neutral-800">
            No monitors on this status page yet.
          </div>
        ) : (
          groups.map(([groupName, groupMonitors]) => (
            <section key={groupName || 'ungrouped'} className="space-y-3">
              {groupName && <h2 className="text-lg font-semibold">{groupName}</h2>}
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
                {groupMonitors.map((m) => (
                  <MonitorCard key={m.id} m={m} />
                ))}
              </div>
            </section>
          ))
        )}

        {/* Recent incidents */}
        {recentIncidents.length > 0 && (
          <section className="space-y-3">
            <h2 className="flex items-center gap-2 text-lg font-semibold">
              <AlertTriangle className="h-5 w-5 text-amber-500" /> Recent Incidents
            </h2>
            <div className="rounded-lg border border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-900">
              <div className="divide-y divide-neutral-100 dark:divide-neutral-800">
                {recentIncidents.map((inc, i) => (
                  <div key={i} className="flex items-center justify-between gap-4 p-4 text-sm">
                    <div className="min-w-0">
                      <div className="font-medium">{inc.monitorName}</div>
                      <div className="text-xs text-neutral-500">
                        {(() => {
                          try {
                            const start = format(parseISO(inc.start), 'MMM d, HH:mm')
                            const end = inc.end ? format(parseISO(inc.end), 'HH:mm') : 'ongoing'
                            return `${start} – ${end} UTC`
                          } catch {
                            return inc.start
                          }
                        })()}
                      </div>
                    </div>
                    <span className="shrink-0 text-neutral-500">
                      {formatDowntime(inc.duration_minutes)}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          </section>
        )}

        {/* Footer */}
        <footer className="border-t border-neutral-200 pt-4 text-center text-xs text-neutral-400 dark:border-neutral-800">
          Powered by Sentinel · Last updated {relative(summary?.last_updated ?? page.updated_at)}
        </footer>
      </div>
    </div>
  )
}
