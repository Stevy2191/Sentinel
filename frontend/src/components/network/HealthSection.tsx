import { useState } from 'react'
import HealthRowTile from '@/components/network/HealthRowTile'
import TrafficChart from '@/components/network/TrafficChart'
import type { DeviceHealth, DeviceProfileView, HealthMetric } from '@/hooks/useDeviceHealth'
import { timeAgo } from '@/utils/network'

interface Props {
  deviceId: string
  health: DeviceHealth | null
}

/** A profile's status line: "Cisco switch health · last poll 1 min ago", or
 *  red "… · last poll failed: <error>" when its last run didn't succeed. */
function profileLine(p: DeviceProfileView): { text: string; failed: boolean } {
  const label = `${p.profile.name} health`
  if (!p.last_run) return { text: `${label} · not polled yet`, failed: false }
  if (!p.last_run.ok) return { text: `${label} · last poll failed: ${p.last_run.error}`, failed: true }
  return { text: `${label} · last poll ${timeAgo(p.last_run.ran_at)}`, failed: false }
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
          <HealthRowTile
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
