import { useEffect, useMemo, useState } from 'react'
import { useMonitors } from '@/hooks/useMonitors'
import { useSummaryReport } from '@/hooks/useReports'
import { useCardShimmer } from '@/hooks/useCardShimmer'
import ShimmerStatCard from '@/components/ShimmerStatCard'
import ShimmerTypeCard from '@/components/ShimmerTypeCard'
import { REPORT_PERIODS, type ReportPeriod } from '@/utils/reportPeriods'

const REFRESH_MS = 30_000

/**
 * Overview is a read-only snapshot of how everything stands right now.
 *
 * Deliberately without the monitor table or any create action: those live on
 * Uptime Monitoring. This page answers "is anything wrong" at a glance and
 * nothing else, so it stays legible on a wall display.
 */
export default function Overview() {
  const { monitors, refetch } = useMonitors()
  const [refreshedAt, setRefreshedAt] = useState(() => Date.now())
  const [period, setPeriod] = useState<ReportPeriod>('30d')

  useEffect(() => {
    const t = window.setInterval(() => {
      void refetch()
      setRefreshedAt(Date.now())
    }, REFRESH_MS)
    return () => window.clearInterval(t)
  }, [refetch])

  const window24h = useMemo(() => {
    const end = new Date()
    return { start: new Date(end.getTime() - 24 * 3600e3).toISOString(), end: end.toISOString() }
  }, [])
  const { report: summary } = useSummaryReport(window24h.start, window24h.end)

  const periodRange = useMemo(() => {
    const hours = REPORT_PERIODS.find((p) => p.key === period)?.hours ?? 24 * 30
    const end = new Date()
    return { start: new Date(end.getTime() - hours * 3600e3).toISOString(), end: end.toISOString() }
  }, [period])
  const { report: periodSummary, loading: periodLoading } = useSummaryReport(
    periodRange.start,
    periodRange.end
  )

  const shimmer = useCardShimmer([
    'operational',
    'responseTime',
    'incidents',
    'agents',
    'dns',
    'http',
    'ping',
    'tcp',
  ])

  // "Paused" is a configuration state, so a disabled monitor counts as paused
  // rather than by whatever its last known status happened to be.
  const counts = useMemo(() => {
    let down = 0
    let up = 0
    let paused = 0
    for (const m of monitors) {
      if (!m.enabled) paused++
      else if (m.current_status === 'offline') down++
      else if (m.current_status === 'online') up++
    }
    return { down, up, paused, active: monitors.length - paused, total: monitors.length }
  }, [monitors])

  // Averaged from each monitor's last recorded value rather than a separate
  // call, so the number always agrees with the rows it is drawn from.
  const overview = useMemo(() => {
    const timed = monitors.filter((m) => m.enabled && m.last_response_time_ms > 0)
    const avgResponse = timed.length
      ? Math.round(timed.reduce((sum, m) => sum + m.last_response_time_ms, 0) / timed.length)
      : 0
    return { avgResponse, timedCount: timed.length }
  }, [monitors])

  const byType = useMemo(() => {
    const keys = ['dns', 'http', 'ping', 'tcp'] as const
    return keys.map((key) => {
      const of = monitors.filter((m) => m.type === key)
      return {
        key,
        label: key.toUpperCase(),
        count: of.length,
        online: of.filter((m) => m.enabled && m.current_status === 'online').length,
      }
    })
  }, [monitors])

  const lastUpdated = useMemo(
    () => new Date(refreshedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    [refreshedAt]
  )

  // Emerald while everything answers, yellow the moment anything is down.
  const anyDown = counts.down > 0
  const mainCard = anyDown
    ? { bg: 'from-yellow-600/20', border: 'border-yellow-500/30', text: 'text-yellow-400' }
    : { bg: 'from-emerald-600/20', border: 'border-emerald-500/30', text: 'text-emerald-400' }
  const periodHeading =
    REPORT_PERIODS.find((pp) => pp.key === period)?.heading.toLowerCase() ?? 'last 30 days'
  const periodUptime = periodSummary?.aggregate.avg_uptime

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-4xl font-light text-white">Overview</h1>
        <p className="mt-2 text-sm text-slate-400">
          {counts.total} service{counts.total === 1 ? '' : 's'} monitored &bull; Last updated{' '}
          {lastUpdated}
        </p>
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        <div className="space-y-4 lg:col-span-2">
          <div
            className={`group relative overflow-hidden rounded-xl border bg-gradient-to-br ${mainCard.bg} via-slate-800/40 to-cyan-600/20 p-8 backdrop-blur-sm ${mainCard.border}`}
            onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'operational')}
            onMouseEnter={() => shimmer.handleCardMouseEnter('operational')}
            onMouseLeave={() => shimmer.handleCardMouseLeave('operational')}
          >
            <div className="pointer-events-none absolute inset-0 rounded-xl bg-gradient-to-r from-emerald-500/0 via-emerald-500/10 to-cyan-500/0" />
            {shimmer.isShown('operational') && (
              <div
                className="pointer-events-none absolute inset-0 rounded-xl transition-all duration-75"
                style={shimmer.getShimmerStyle('operational')}
              />
            )}
            <div className="relative z-10 flex flex-wrap items-start justify-between gap-6">
              <div>
                <div className={`mb-4 text-xs font-semibold uppercase tracking-widest ${mainCard.text}`}>
                  All Services
                </div>
                <div className="mb-2 text-5xl font-light text-white">
                  {counts.active === 0
                    ? 'Idle'
                    : anyDown
                      ? counts.up === 0
                        ? 'Major outage'
                        : 'Degraded'
                      : 'Operational'}
                </div>
                <div className="text-slate-300">
                  {counts.up} of {counts.active} service{counts.active === 1 ? '' : 's'} are up
                  {counts.paused > 0 && (
                    <span className="text-slate-400"> &middot; {counts.paused} paused</span>
                  )}
                </div>
              </div>
              <div className="text-right">
                <div className={`mb-2 text-4xl font-light ${mainCard.text}`}>
                  {periodLoading && periodUptime == null
                    ? '—'
                    : periodUptime != null
                      ? `${periodUptime.toFixed(2)}%`
                      : '—'}
                </div>
                <select
                  value={period}
                  onChange={(e) => setPeriod(e.target.value as ReportPeriod)}
                  aria-label="Uptime reporting window"
                  className="cursor-pointer rounded border border-white/10 bg-slate-900/60 px-2 py-1 text-xs text-slate-400 transition hover:text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-white/40"
                >
                  {REPORT_PERIODS.map((pp) => (
                    <option key={pp.key} value={pp.key}>
                      {pp.heading} uptime
                    </option>
                  ))}
                </select>
              </div>
            </div>
          </div>

          <div className="grid gap-4 sm:grid-cols-3">
            <ShimmerStatCard
              title="Avg Response Time"
              value={overview.avgResponse > 0 ? `${overview.avgResponse}ms` : '—'}
              subtitle={overview.timedCount > 0 ? `across ${overview.timedCount}` : 'no data yet'}
              colorType="responseTime"
              onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'responseTime')}
              onMouseEnter={() => shimmer.handleCardMouseEnter('responseTime')}
              onMouseLeave={() => shimmer.handleCardMouseLeave('responseTime')}
              showShimmer={shimmer.isShown('responseTime')}
              shimmerStyle={shimmer.getShimmerStyle('responseTime')}
            />
            <ShimmerStatCard
              title="Total Incidents"
              value={summary?.aggregate.total_incidents ?? 0}
              subtitle={periodHeading}
              colorType="incidents"
              onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'incidents')}
              onMouseEnter={() => shimmer.handleCardMouseEnter('incidents')}
              onMouseLeave={() => shimmer.handleCardMouseLeave('incidents')}
              showShimmer={shimmer.isShown('incidents')}
              shimmerStyle={shimmer.getShimmerStyle('incidents')}
            />
            <ShimmerStatCard
              title="Monitoring Agents"
              value="0 online"
              subtitle="coming soon"
              colorType="agents"
              onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'agents')}
              onMouseEnter={() => shimmer.handleCardMouseEnter('agents')}
              onMouseLeave={() => shimmer.handleCardMouseLeave('agents')}
              showShimmer={shimmer.isShown('agents')}
              shimmerStyle={shimmer.getShimmerStyle('agents')}
            />
          </div>
        </div>

        <div className="lg:col-span-1">
          <div className="grid grid-cols-2 gap-3">
            {byType
              .filter((t) => t.count > 0)
              .map((t) => (
                <ShimmerTypeCard
                  key={t.key}
                  label={t.label}
                  count={t.count}
                  online={t.online}
                  colorType={t.key}
                  onMouseMove={(e) => shimmer.handleCardMouseMove(e, t.key)}
                  onMouseEnter={() => shimmer.handleCardMouseEnter(t.key)}
                  onMouseLeave={() => shimmer.handleCardMouseLeave(t.key)}
                  showShimmer={shimmer.isShown(t.key)}
                  shimmerStyle={shimmer.getShimmerStyle(t.key)}
                />
              ))}
            {byType.every((t) => t.count === 0) && (
              <div className="col-span-2 rounded-lg border border-white/10 bg-slate-800/40 p-4 text-sm text-slate-400 backdrop-blur-sm">
                No monitors configured yet.
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
