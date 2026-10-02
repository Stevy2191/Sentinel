import { useState } from 'react'
import TrafficChart from '@/components/network/TrafficChart'
import type { DeviceHealth, DeviceProfileView, HealthMetric, HealthRow } from '@/hooks/useDeviceHealth'
import { timeAgo } from '@/utils/network'

interface Props {
  deviceId: string
  health: DeviceHealth | null
}

const CHIP_TONE = {
  ok: 'border-emerald-500/30 bg-emerald-500/15 text-emerald-300',
  problem: 'border-red-500/30 bg-red-500/15 text-red-300',
  warn: 'border-amber-500/30 bg-amber-500/15 text-amber-300',
} as const

/** green when the row's state is OK, red while an incident is open for it,
 *  else amber (a state outside the OK list, but with no rule raising it). */
function chipTone(row: HealthRow): keyof typeof CHIP_TONE {
  if (row.problem) return 'problem'
  if (row.ok) return 'ok'
  return 'warn'
}

function rowValueText(row: HealthRow, units: string): string {
  return `${+row.value.toFixed(1)} ${units}`.trim()
}

/** A profile's status line: "Cisco switch health · last poll 1 min ago", or
 *  red "… · last poll failed: <error>" when its last run didn't succeed. */
function profileLine(p: DeviceProfileView): { text: string; failed: boolean } {
  const label = `${p.profile.name} health`
  if (!p.last_run) return { text: `${label} · not polled yet`, failed: false }
  if (!p.last_run.ok) return { text: `${label} · last poll failed: ${p.last_run.error}`, failed: true }
  return { text: `${label} · last poll ${timeAgo(p.last_run.ran_at)}`, failed: false }
}

/** One row's tile: a status row shows a coloured chip and is not clickable; a
 *  numeric row shows its value and opens its history chart when clicked. A
 *  red ring marks a row an incident is currently open for; a row with an open
 *  incident but no live sample says "No reading". */
function RowTile({ row, units, open, onToggle }: { row: HealthRow; units: string; open: boolean; onToggle: () => void }) {
  const ring = row.problem ? 'ring-2 ring-red-500/60' : ''
  if (row.no_reading) {
    return (
      <div className={`rounded-lg border border-white/10 bg-slate-800/40 p-2 ${ring}`}>
        <p className="truncate text-xs text-slate-400">{row.label}</p>
        <p className="mt-1 text-sm text-red-300">No reading</p>
      </div>
    )
  }
  if (row.state !== undefined) {
    return (
      <div className={`rounded-lg border border-white/10 bg-slate-800/40 p-2 ${ring}`}>
        <p className="truncate text-xs text-slate-400">{row.label}</p>
        <span className={`mt-1 inline-flex rounded-full border px-2 py-0.5 text-xs font-medium ${CHIP_TONE[chipTone(row)]}`}>{row.state}</span>
      </div>
    )
  }
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={open}
      className={`rounded-lg border border-white/10 bg-slate-800/40 p-2 text-left transition hover:bg-slate-800/60 ${ring}`}
    >
      <p className="truncate text-xs text-slate-400">{row.label}</p>
      <p className="mt-1 text-sm tabular-nums text-white">{rowValueText(row, units)}</p>
    </button>
  )
}

const CHART_COLOUR = '#38bdf8'

/** One metric's card: its rule in words, a compact grid of its rows, and —
 *  while a numeric row is selected — that row's history chart underneath. */
function MetricCard({ deviceId, metric }: { deviceId: string; metric: HealthMetric }) {
  const [openInstance, setOpenInstance] = useState<string | null>(null)
  const openRow = metric.rows.find((r) => r.instance === openInstance) ?? null

  return (
    <div className="card space-y-3 p-4">
      <div>
        <p className="font-medium text-white">{metric.name}</p>
        {metric.rule && <p className="text-xs text-slate-500">{metric.rule}</p>}
      </div>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
        {metric.rows.map((row) => (
          <RowTile
            key={row.instance}
            row={row}
            units={metric.units}
            open={row.instance === openInstance}
            onToggle={() => setOpenInstance((s) => (s === row.instance ? null : row.instance))}
          />
        ))}
      </div>
      {openRow && (
        <TrafficChart
          title={openRow.label}
          query={{ deviceIds: [deviceId], instances: [openRow.instance] }}
          lines={[{ metric: metric.key, label: openRow.label, colour: CHART_COLOUR }]}
          unit="custom"
          unitLabel={metric.units}
        />
      )}
    </div>
  )
}

/**
 * A device's Health section: each applicable profile's last-poll standing,
 * then a card per live metric. Hidden entirely when there is no metric data
 * yet (a brand-new device, or none of its profiles have polled).
 */
export default function HealthSection({ deviceId, health }: Props) {
  if (!health || health.metrics.length === 0) return null
  const applicable = health.profiles.filter((p) => p.applies)

  return (
    <section className="space-y-4">
      <h2 className="text-lg font-light text-white">Health</h2>
      {applicable.length > 0 && (
        <div className="space-y-1">
          {applicable.map((p) => {
            const line = profileLine(p)
            return (
              <p key={p.profile.id} className={`text-sm ${line.failed ? 'text-red-400' : 'text-slate-400'}`}>
                {line.text}
              </p>
            )
          })}
        </div>
      )}
      <div className="space-y-4">
        {health.metrics.map((m) => (
          <MetricCard key={m.key} deviceId={deviceId} metric={m} />
        ))}
      </div>
    </section>
  )
}
