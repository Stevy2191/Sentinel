import { Link } from 'react-router-dom'
import HealthRowTile from '@/components/network/HealthRowTile'
import type { DeviceHealthData } from '@/types/dashboards'

export default function DeviceHealthWidget({ data, linkable }: { data: DeviceHealthData; linkable: boolean }) {
  return (
    <div className="space-y-3">
      {(data.metrics ?? []).map((m) => (
        <div key={m.key} className="space-y-1">
          <p className="text-xs text-slate-400">
            {m.name}
            {m.rule && ` · alert when ${m.rule}`}
          </p>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(7rem,1fr))] gap-2">
            {(m.rows ?? []).map((r) => (
              <HealthRowTile key={r.instance} row={r} units={m.units} />
            ))}
          </div>
        </div>
      ))}
      {linkable && data.device_id && (
        <Link to={`/network/devices/${data.device_id}`} className="text-xs text-primary-400 hover:underline">
          Open {data.device_name}
        </Link>
      )}
    </div>
  )
}
