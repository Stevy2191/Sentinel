import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMonitors } from '@/hooks/useMonitors'
import { useAgentSummary, useFleetMetrics } from '@/hooks/useAgents'
import { useSSLSummary } from '@/hooks/useSSLCertificates'
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
/**
 * One resource reading inside the status card.
 *
 * Colour comes from the number rather than being fixed, so the row stays quiet
 * until something has actually filled up, and the bar carries the reading at a
 * glance without the figure having to be read.
 */
function ResourceMeter({
  label,
  percent,
  detail,
}: {
  label: string
  percent: number
  detail: string
}) {
  const v = Math.max(0, Math.min(100, percent))
  const bar = v >= 90 ? 'bg-red-500' : v >= 75 ? 'bg-amber-500' : 'bg-emerald-500'
  const text = v >= 90 ? 'text-red-400' : v >= 75 ? 'text-amber-400' : 'text-white'

  return (
    <div>
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-xs uppercase tracking-wider text-slate-400">{label}</span>
        <span className={`text-sm font-medium tabular-nums ${text}`}>{v.toFixed(1)}%</span>
      </div>
      <div className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-white/10">
        <div className={`h-full rounded-full ${bar}`} style={{ width: `${v}%` }} />
      </div>
      <div className="mt-1 text-xs text-slate-500">{detail}</div>
    </div>
  )
}

export default function Overview() {
  const navigate = useNavigate()
  const { monitors, refetch } = useMonitors()
  const agentSummary = useAgentSummary()
  const sslSummary = useSSLSummary()
  const { metrics: fleet } = useFleetMetrics()
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
    'ssl',
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

      {/* One column rather than a two-thirds split. The type cards were in a
          narrow right-hand column, so an install watching only one or two
          kinds of monitor left a lone card beside a tall one. Stacked, every
          row spans the width and the breakdown reads as a row of its own. */}
      <div className="space-y-6">
        <div className="space-y-4">
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

            {/* Server resources, from the agents rather than from the
                monitors. Inside this card because it answers the same
                question it does — whether everything is healthy right now —
                and a row of its own would imply a separate subject. Hidden
                entirely when no agent is reporting: three bars at zero would
                read as a fleet at rest rather than as no fleet. */}
            {fleet && fleet.agents_reporting > 0 && (
              <div className="relative z-10 mt-8 border-t border-white/10 pt-6">
                <div className="mb-4 flex items-baseline justify-between gap-2">
                  <span className="text-xs font-semibold uppercase tracking-widest text-slate-400">
                    Server resources
                  </span>
                  <button
                    onClick={() => navigate('/servers')}
                    className="text-xs text-slate-500 transition hover:text-slate-300"
                  >
                    across {fleet.agents_reporting} server
                    {fleet.agents_reporting === 1 ? '' : 's'} &rarr;
                  </button>
                </div>
                <div className="grid gap-5 sm:grid-cols-3">
                  <ResourceMeter
                    label="CPU"
                    percent={fleet.cpu_percent}
                    detail={
                      fleet.agents_reporting === 1 ? 'current load' : 'average across servers'
                    }
                  />
                  <ResourceMeter
                    label="Memory"
                    percent={fleet.memory_percent}
                    detail={`${(fleet.memory_used_mb / 1024).toFixed(1)} of ${(
                      fleet.memory_total_mb / 1024
                    ).toFixed(1)} GB`}
                  />
                  <ResourceMeter
                    label="Disk"
                    percent={fleet.disk_percent}
                    detail={`${fleet.disk_used_gb.toFixed(0)} of ${fleet.disk_total_gb.toFixed(
                      0,
                    )} GB`}
                  />
                </div>
              </div>
            )}
          </div>

          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
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
              title="SSL & Domains"
              value={sslSummary.value}
              subtitle={sslSummary.subtitle}
              colorType="ssl"
              onClick={() => navigate('/ssl')}
              onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'ssl')}
              onMouseEnter={() => shimmer.handleCardMouseEnter('ssl')}
              onMouseLeave={() => shimmer.handleCardMouseLeave('ssl')}
              showShimmer={shimmer.isShown('ssl')}
              shimmerStyle={shimmer.getShimmerStyle('ssl')}
            />
            <ShimmerStatCard
              title="Monitoring Agents"
              value={agentSummary.value}
              subtitle={agentSummary.subtitle}
              colorType="agents"
              onClick={() => navigate('/servers')}
              onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'agents')}
              onMouseEnter={() => shimmer.handleCardMouseEnter('agents')}
              onMouseLeave={() => shimmer.handleCardMouseLeave('agents')}
              showShimmer={shimmer.isShown('agents')}
              shimmerStyle={shimmer.getShimmerStyle('agents')}
            />
          </div>
        </div>

        <div>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4">
            {byType
              .filter((t) => t.count > 0)
              .map((t) => (
                <ShimmerTypeCard
                  key={t.key}
                  label={t.label}
                  count={t.count}
                  online={t.online}
                  colorType={t.key}
                  onClick={() => navigate(`/uptime?type=${t.key}`)}
                  onMouseMove={(e) => shimmer.handleCardMouseMove(e, t.key)}
                  onMouseEnter={() => shimmer.handleCardMouseEnter(t.key)}
                  onMouseLeave={() => shimmer.handleCardMouseLeave(t.key)}
                  showShimmer={shimmer.isShown(t.key)}
                  shimmerStyle={shimmer.getShimmerStyle(t.key)}
                />
              ))}
            {byType.every((t) => t.count === 0) && (
              <div className="col-span-full rounded-lg border border-white/10 bg-slate-800/40 p-4 text-sm text-slate-400 backdrop-blur-sm">
                No monitors configured yet.
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
