import { BatteryCharging, BatteryWarning, Plug } from 'lucide-react'
import type { UPSStatus } from '@/hooks/usePorts'
import TrafficChart from '@/components/network/TrafficChart'
import { CONDITION_LABEL } from '@/utils/network'

const BATTERY_STATUS: Record<number, string> = { 1: 'Battery status unknown', 3: 'Battery low', 4: 'Battery depleted' }

interface Props {
  deviceId: string
  status: UPSStatus | null
}

/** A UPS's Power section: source badge, live tiles, and history charts. */
export default function UPSPanel({ deviceId, status }: Props) {
  const r = status?.readings ?? {}
  const hasData = Object.keys(r).length > 0
  const onBattery = r.ups_on_battery
  const tiles: [string, string][] = []
  const add = (label: string, v: number | undefined, fmt: (n: number) => string) => {
    if (v !== undefined) tiles.push([label, fmt(v)])
  }
  add('Charge', r.ups_charge_pct, (n) => `${Math.round(n)}%`)
  add('Runtime left', r.ups_runtime_min, (n) => `${Math.round(n)} min`)
  add('Load', r.ups_load_pct, (n) => `${Math.round(n)}%`)
  add('Input', r.ups_input_v, (n) => `${Math.round(n)} V`)
  add('Output', r.ups_output_v, (n) => `${Math.round(n)} V`)
  add('Battery temperature', r.ups_battery_temp_c, (n) => `${Math.round(n)} °C`)
  const batteryNote = r.ups_battery_status !== undefined ? BATTERY_STATUS[r.ups_battery_status] : undefined

  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-lg font-light text-white">Power</h2>
        {hasData &&
          (onBattery === 1 ? (
            <span className="inline-flex items-center gap-1.5 rounded-full border border-amber-500/30 bg-amber-500/15 px-2.5 py-0.5 text-xs font-medium text-amber-300">
              <BatteryWarning className="h-3.5 w-3.5" /> On battery
            </span>
          ) : onBattery === 0 ? (
            <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-500/30 bg-emerald-500/15 px-2.5 py-0.5 text-xs font-medium text-emerald-300">
              <Plug className="h-3.5 w-3.5" /> On mains
            </span>
          ) : (
            <span className="rounded-full border border-slate-500/30 bg-slate-500/15 px-2.5 py-0.5 text-xs text-slate-300">Source unknown</span>
          ))}
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
          <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
            {tiles.map(([k, v]) => (
              <div key={k} className="rounded-lg border border-white/10 bg-slate-800/40 p-3">
                <dt className="flex items-center gap-1 text-xs uppercase tracking-widest text-slate-500">
                  {k === 'Charge' && <BatteryCharging className="h-3.5 w-3.5" />}
                  {k}
                </dt>
                <dd className="mt-1 text-xl font-light tabular-nums text-white">{v}</dd>
              </div>
            ))}
          </dl>
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
