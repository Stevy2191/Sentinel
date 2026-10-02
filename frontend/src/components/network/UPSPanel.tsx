import type { UPSStatus } from '@/hooks/usePorts'
import TrafficChart from '@/components/network/TrafficChart'
import UPSReadingTiles from '@/components/network/UPSReadingTiles'
import UPSSourceBadge from '@/components/network/UPSSourceBadge'
import { CONDITION_LABEL } from '@/utils/network'
import { BATTERY_STATUS } from '@/utils/ups'

interface Props {
  deviceId: string
  status: UPSStatus | null
}

/** A UPS's Power section: source badge, live tiles, and history charts. */
export default function UPSPanel({ deviceId, status }: Props) {
  const r = status?.readings ?? {}
  const hasData = Object.keys(r).length > 0
  const onBattery = r.ups_on_battery
  const batteryNote = r.ups_battery_status !== undefined ? BATTERY_STATUS[r.ups_battery_status] : undefined

  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-lg font-light text-white">Power</h2>
        {hasData && <UPSSourceBadge onBattery={onBattery} />}
        {batteryNote && <span className="text-xs text-amber-300">{batteryNote}</span>}
        {status?.conditions
          .filter((c) => c !== 'ups_on_battery')
          .map((c) => (
            <span key={c} className="rounded-full border border-red-500/30 bg-red-500/15 px-2.5 py-0.5 text-xs text-red-300">
              {CONDITION_LABEL[c] ?? c}
            </span>
          ))}
      </div>
      {!hasData ? (
        <p className="text-sm text-slate-500">
          No UPS readings in the last few minutes. If this lasts, the UPS may not report standard UPS readings (UPS-MIB).
        </p>
      ) : (
        <>
          <UPSReadingTiles readings={r} />
          <TrafficChart
            title="Battery and load"
            query={{ deviceIds: [deviceId] }}
            lines={[
              { metric: 'ups_charge_pct', label: 'Charge', colour: '#34d399' },
              { metric: 'ups_load_pct', label: 'Load', colour: '#60a5fa' },
            ]}
            unit="pct"
            threshold={status?.high_load_pct}
          />
          <TrafficChart title="Runtime left" query={{ deviceIds: [deviceId] }} lines={[{ metric: 'ups_runtime_min', label: 'Runtime', colour: '#a78bfa' }]} unit="min" />
        </>
      )}
    </section>
  )
}
