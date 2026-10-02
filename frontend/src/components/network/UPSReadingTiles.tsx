import { BatteryCharging } from 'lucide-react'
import { upsTiles } from '@/utils/ups'

interface Props {
  readings: Record<string, number>
  /** Smaller tiles in three columns, for dashboard widgets. */
  compact?: boolean
  /** Only these reading keys (e.g. charge, runtime, load). */
  only?: string[]
}

export default function UPSReadingTiles({ readings, compact = false, only }: Props) {
  const tiles = upsTiles(readings).filter((t) => !only || only.includes(t.key))
  return (
    <dl className={compact ? 'grid grid-cols-3 gap-2' : 'grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6'}>
      {tiles.map((t) => (
        <div key={t.key} className={`rounded-lg border border-white/10 bg-slate-800/40 ${compact ? 'p-2' : 'p-3'}`}>
          <dt className="flex items-center gap-1 text-xs uppercase tracking-widest text-slate-500">
            {t.key === 'ups_charge_pct' && <BatteryCharging className="h-3.5 w-3.5" />}
            {t.label}
          </dt>
          <dd className={`mt-1 font-light tabular-nums text-white ${compact ? 'text-base' : 'text-xl'}`}>{t.value}</dd>
        </div>
      ))}
    </dl>
  )
}
