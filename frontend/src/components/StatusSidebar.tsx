import type { SummaryReport } from '@/types'

export type ReportPeriod = '24h' | '7d' | '30d'

export const REPORT_PERIODS: { key: ReportPeriod; label: string; heading: string; hours: number }[] = [
  { key: '24h', label: '24 hours', heading: 'Last 24 hours', hours: 24 },
  { key: '7d', label: '7 days', heading: 'Last 7 days', hours: 24 * 7 },
  { key: '30d', label: '30 days', heading: 'Last 30 days', hours: 24 * 30 },
]

function uptimeTone(pct: number): string {
  if (pct >= 99) return 'var(--vs-ecg)'
  if (pct >= 95) return 'var(--vs-amber)'
  return 'var(--vs-flat)'
}

/** One of the three headline counts in the status card. */
function Count({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <span className="vs-eyebrow" style={{ color: 'var(--vs-text-dim)' }}>
        {label}
      </span>
      <span
        className={`vs-readout text-2xl font-medium leading-none ${value > 0 ? 'vs-glow' : ''}`}
        style={{ color: value > 0 ? color : 'var(--vs-text-dim)' }}
      >
        {value}
      </span>
    </div>
  )
}

/** A label/value line in the reporting card. */
function Stat({ label, value, color }: { label: string; value: string; color?: string }) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <span className="text-xs" style={{ color: 'var(--vs-text-dim)' }}>
        {label}
      </span>
      <span className="vs-readout text-sm font-medium" style={{ color: color ?? 'var(--vs-text)' }}>
        {value}
      </span>
    </div>
  )
}

/** "1h 20m" / "45m" / "0m" from a minute count. */
function formatMinutes(mins: number): string {
  const total = Math.max(0, Math.round(mins))
  const h = Math.floor(total / 60)
  const m = total % 60
  return h > 0 ? `${h}h ${m}m` : `${m}m`
}

interface Props {
  down: number
  up: number
  paused: number
  /** Monitors actively being checked, and the total configured. */
  active: number
  total: number
  period: ReportPeriod
  onPeriodChange: (period: ReportPeriod) => void
  summary: SummaryReport | null
  summaryLoading: boolean
}

/**
 * StatusSidebar — the dashboard's at-a-glance column: how many services are in
 * each state right now, and how the fleet has held up over a chosen window. It
 * stays put while the monitor list scrolls beside it.
 */
export default function StatusSidebar({
  down,
  up,
  paused,
  active,
  total,
  period,
  onPeriodChange,
  summary,
  summaryLoading,
}: Props) {
  const heading = REPORT_PERIODS.find((p) => p.key === period)?.heading ?? 'Last 30 days'
  const aggregate = summary?.aggregate

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-1">
      {/* Current status */}
      <section className="rd-card p-4" style={{ ['--rd-accent' as string]: down > 0 ? 'var(--vs-flat)' : 'var(--vs-ecg)' }}>
        <h2 className="vs-eyebrow mb-3">Current status</h2>
        <div className="space-y-2.5">
          <Count label="Down" value={down} color="var(--vs-flat)" />
          <Count label="Up" value={up} color="var(--vs-ecg)" />
          <Count label="Paused" value={paused} color="var(--vs-amber)" />
        </div>
        <p className="mt-3 border-t pt-3 text-xs" style={{ borderColor: 'var(--vs-line)', color: 'var(--vs-text-dim)' }}>
          {/* "Using" = actively checked; paused monitors are configured but idle. */}
          Using <span className="vs-readout" style={{ color: 'var(--vs-text)' }}>{active}</span> of{' '}
          <span className="vs-readout" style={{ color: 'var(--vs-text)' }}>{total}</span>{' '}
          {total === 1 ? 'monitor' : 'monitors'}
        </p>
      </section>

      {/* Reporting window */}
      <section className="rd-card p-4" style={{ ['--rd-accent' as string]: 'var(--vs-cyan)' }}>
        <h2 className="vs-eyebrow mb-3">{heading}</h2>
        <div className="mb-3 flex gap-1">
          {REPORT_PERIODS.map((p) => {
            const on = p.key === period
            return (
              <button
                key={p.key}
                onClick={() => onPeriodChange(p.key)}
                aria-pressed={on}
                className="flex-1 rounded-md px-2 py-1.5 font-display text-[11px] font-semibold transition-colors"
                style={{
                  letterSpacing: '0.04em',
                  backgroundColor: on ? 'rgba(61, 225, 255, 0.12)' : 'rgba(255,255,255,0.03)',
                  color: on ? 'var(--vs-cyan)' : 'var(--vs-text-dim)',
                  border: `1px solid ${on ? 'var(--vs-cyan)' : 'var(--vs-line)'}`,
                }}
              >
                {p.label}
              </button>
            )
          })}
        </div>
        {summaryLoading && !aggregate ? (
          <div className="space-y-2">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="h-4 animate-pulse rounded" style={{ backgroundColor: 'var(--vs-line)' }} />
            ))}
          </div>
        ) : aggregate ? (
          <div className="space-y-2">
            <Stat label="Avg uptime" value={`${aggregate.avg_uptime.toFixed(2)}%`} color={uptimeTone(aggregate.avg_uptime)} />
            <Stat label="Incidents" value={String(aggregate.total_incidents)} />
            <Stat label="Total downtime" value={formatMinutes(aggregate.total_downtime_minutes)} />
            <Stat label="Worst monitor" value={`${aggregate.worst_uptime.toFixed(2)}%`} color={uptimeTone(aggregate.worst_uptime)} />
          </div>
        ) : (
          <p className="text-xs" style={{ color: 'var(--vs-text-dim)' }}>
            No reporting data for this window.
          </p>
        )}
      </section>
    </div>
  )
}
