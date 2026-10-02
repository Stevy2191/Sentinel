import { Link } from 'react-router-dom'
import UPSReadingTiles from '@/components/network/UPSReadingTiles'
import UPSSourceBadge from '@/components/network/UPSSourceBadge'
import type { SitePowerData } from '@/types/dashboards'
import { CONDITION_LABEL } from '@/utils/network'

const KEY_READINGS = ['ups_charge_pct', 'ups_runtime_min', 'ups_load_pct']

export default function SitePowerWidget({ data, linkable }: { data: SitePowerData; linkable: boolean }) {
  return (
    <ul className="space-y-3">
      {(data.upses ?? []).map((u, i) => (
        <li key={u.device_id ?? `${u.name}-${i}`} className="space-y-2 rounded-lg border border-white/10 p-2">
          <div className="flex flex-wrap items-center gap-2">
            {linkable && u.device_id ? (
              <Link to={`/network/devices/${u.device_id}`} className="font-medium text-slate-200 hover:text-primary-400">
                {u.name}
              </Link>
            ) : (
              <span className="font-medium text-slate-200">{u.name}</span>
            )}
            {u.status === 'down' ? (
              <span className="rounded-full border border-red-500/30 bg-red-500/15 px-2 py-0.5 text-xs text-red-300">Not answering</span>
            ) : (
              <UPSSourceBadge onBattery={u.readings?.ups_on_battery} />
            )}
            {(u.conditions ?? [])
              .filter((c) => c !== 'ups_on_battery')
              .map((c) => (
                <span key={c} className="rounded-full border border-red-500/30 bg-red-500/15 px-2 py-0.5 text-xs text-red-300">
                  {CONDITION_LABEL[c] ?? c}
                </span>
              ))}
          </div>
          <UPSReadingTiles readings={u.readings ?? {}} compact only={KEY_READINGS} />
        </li>
      ))}
    </ul>
  )
}
