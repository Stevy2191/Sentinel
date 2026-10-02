import { BatteryWarning, Plug } from 'lucide-react'

/** On mains, on battery, or unknown, from the ups_on_battery reading. */
export default function UPSSourceBadge({ onBattery }: { onBattery: number | undefined }) {
  if (onBattery === 1) {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full border border-amber-500/30 bg-amber-500/15 px-2.5 py-0.5 text-xs font-medium text-amber-300">
        <BatteryWarning className="h-3.5 w-3.5" /> On battery
      </span>
    )
  }
  if (onBattery === 0) {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-500/30 bg-emerald-500/15 px-2.5 py-0.5 text-xs font-medium text-emerald-300">
        <Plug className="h-3.5 w-3.5" /> On mains
      </span>
    )
  }
  return <span className="rounded-full border border-slate-500/30 bg-slate-500/15 px-2.5 py-0.5 text-xs text-slate-300">Source unknown</span>
}
