import { useNavigate } from 'react-router-dom'
import { useMonitorUptime } from '@/hooks/useMonitorUptime'
import { Sparkline, hourlySparklinePoints, uptimeColor } from '@/components/UptimeSparkline'
import MonitorRowActions from '@/components/MonitorRowActions'
import ListRowLayout, { ago } from '@/components/monitoring/ListRowLayout'
import StatusPill from '@/components/monitoring/StatusPill'
import { formatLastResponseTime } from '@/utils/formatters'
import { monitorAccess, badgeToneClass } from '@/utils/monitorAccess'
import { kindLabel, type ListRow } from '@/utils/monitoringList'

/** Colour for the response time: fast, acceptable, slow. */
function responseColor(ms: number): string {
  if (ms <= 0) return 'text-slate-500'
  if (ms < 200) return 'text-emerald-400'
  if (ms <= 500) return 'text-yellow-400'
  return 'text-red-400'
}

interface Props {
  row: Extract<ListRow, { kind: 'uptime' }>
  uptime24h: number | null
  ownerUsername?: string
  /** Changes on each refresh, re-fetching this row's checks. */
  refreshKey: unknown
  onChanged: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

/** An uptime check on the Monitoring list: response time, and the 24-hour
 *  strip with its uptime. */
export default function MonitorListRow({ row, uptime24h, ownerUsername, refreshKey, onChanged, push }: Props) {
  const navigate = useNavigate()
  const m = row.monitor
  // One fetch per row powers both the bar strip and the uptime percentage.
  const { data: uptime, loading } = useMonitorUptime(m.id, '24h', true, refreshKey)
  const access = monitorAccess(m)
  // Uptime over the same 24 hourly buckets the strip draws, so the cell is
  // internally consistent. Falls back to the summary endpoint's 24h figure
  // only until this row's own request lands.
  const pct = uptime ? uptime.uptime_24h : uptime24h
  const response = formatLastResponseTime(m.last_response_time_ms, m.current_status)

  return (
    <ListRowLayout
      kind="uptime"
      name={
        <>
          <span className="truncate">{m.name}</span>
          {access.badge && (
            <span className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium ${badgeToneClass[access.badge.tone]}`}>
              {access.badge.label}
            </span>
          )}
        </>
      }
      line={m.url + (!access.isOwner && ownerUsername ? ` · ${ownerUsername}` : '')}
      kindText={kindLabel(row)}
      site={row.siteName}
      status={<StatusPill status={row.status} />}
      health={<span className={`tabular-nums ${responseColor(m.last_response_time_ms)}`}>{response}</span>}
      healthText={response}
      uptime={
        <div title="Last 24 hours">
          <div className="flex items-center gap-2">
            {loading && !uptime ? (
              <div className="flex h-6 w-24 items-end gap-px">
                {Array.from({ length: 24 }).map((_, i) => (
                  <div key={i} className="w-full flex-1 animate-pulse rounded-sm bg-slate-700" style={{ height: '40%' }} />
                ))}
              </div>
            ) : (
              <Sparkline data={hourlySparklinePoints(uptime?.hourly_data ?? [])} className="h-6 w-24" />
            )}
            <span className={`whitespace-nowrap text-xs font-medium tabular-nums ${pct != null ? uptimeColor(pct) : 'text-slate-500'}`}>
              {pct != null ? `${pct.toFixed(1)}%` : '—'}
            </span>
          </div>
          <div className="text-xs text-slate-500">Last 24 hours</div>
        </div>
      }
      seen={ago(m.last_check_at)}
      actions={<MonitorRowActions monitor={m} access={access} onChanged={onChanged} push={push} />}
      onOpen={() => navigate(`/monitors/${m.id}`)}
    />
  )
}
