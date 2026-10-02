import { Link } from 'react-router-dom'
import DeviceStatusBadge from '@/components/network/DeviceStatusBadge'
import { DEVICE_TYPE_LABEL } from '@/hooks/useDevices'
import type { DeviceTableData } from '@/types/dashboards'
import { timeAgo } from '@/utils/network'

export default function DeviceTableWidget({ data, linkable }: { data: DeviceTableData; linkable: boolean }) {
  const devices = data.devices ?? []
  if (devices.length === 0) return <p className="text-slate-500">No devices.</p>
  const showHost = devices.some((d) => d.host)
  return (
    <table className="w-full text-left">
      <thead className="text-xs uppercase tracking-widest text-slate-500">
        <tr>
          <th className="py-1 pr-3 font-medium">Status</th>
          <th className="py-1 pr-3 font-medium">Name</th>
          <th className="py-1 pr-3 font-medium">Type</th>
          {showHost && <th className="py-1 pr-3 font-medium">Address</th>}
          <th className="py-1 pr-3 font-medium">Vendor / model</th>
          <th className="py-1 pr-3 font-medium">Last seen</th>
          <th className="py-1 pr-3 text-right font-medium">30 days</th>
          <th className="py-1 text-right font-medium">Open</th>
        </tr>
      </thead>
      <tbody className="divide-y divide-white/5">
        {devices.map((d, i) => (
          <tr key={d.device_id ?? `${d.name}-${i}`}>
            <td className="py-1.5 pr-3">
              <DeviceStatusBadge status={d.status} />
            </td>
            <td className="py-1.5 pr-3 font-medium text-slate-200">
              {linkable && d.device_id ? (
                <Link to={`/network/devices/${d.device_id}`} className="hover:text-primary-400">
                  {d.name}
                </Link>
              ) : (
                d.name
              )}
            </td>
            <td className="py-1.5 pr-3 text-slate-400">{DEVICE_TYPE_LABEL[d.type] ?? d.type}</td>
            {showHost && <td className="py-1.5 pr-3 font-mono text-xs text-slate-400">{d.host}</td>}
            <td className="py-1.5 pr-3 text-slate-400">{d.vendor_model || '—'}</td>
            <td className="py-1.5 pr-3 text-slate-400">{d.last_seen_at ? timeAgo(d.last_seen_at) : '—'}</td>
            <td className="py-1.5 pr-3 text-right tabular-nums text-slate-300">
              {d.availability_30d == null ? '—' : `${d.availability_30d.toFixed(2)}%`}
            </td>
            <td className={`py-1.5 text-right tabular-nums ${d.open_incidents > 0 ? 'text-red-300' : 'text-slate-500'}`}>{d.open_incidents}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
