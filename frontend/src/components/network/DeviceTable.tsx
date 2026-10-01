import { useNavigate } from 'react-router-dom'
import type { Device } from '@/hooks/useDevices'
import DeviceStatusBadge from './DeviceStatusBadge'

function ago(iso: string | null): string {
  if (!iso) return 'never'
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 90) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)} min ago`
  if (s < 86400) return `${Math.round(s / 3600)} h ago`
  return new Date(iso).toLocaleDateString()
}

export default function DeviceTable({ devices, showSite }: { devices: Device[]; showSite: boolean }) {
  const navigate = useNavigate()
  return (
    <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-white/10 text-left text-xs text-slate-400">
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium">Name</th>
              {showSite && <th className="px-4 py-3 font-medium">Site</th>}
              <th className="px-4 py-3 font-medium">Address</th>
              <th className="px-4 py-3 font-medium">Vendor / model</th>
              <th className="px-4 py-3 font-medium">Last seen</th>
              <th className="px-4 py-3 text-right font-medium">30-day availability</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5">
            {devices.map((d) => (
              <tr key={d.id} className="cursor-pointer transition hover:bg-white/5" onClick={() => navigate(`/network/devices/${d.id}`)}>
                <td className="px-4 py-3">
                  <DeviceStatusBadge status={d.status} detail={d.status_detail} />
                </td>
                <td className="px-4 py-3 font-medium text-slate-200">{d.name}</td>
                {showSite && <td className="px-4 py-3 text-slate-400">{d.site_name}</td>}
                <td className="px-4 py-3 font-mono text-xs text-slate-400">
                  {d.host}
                  {d.port !== 161 && `:${d.port}`}
                </td>
                <td className="px-4 py-3 text-slate-400">{[d.effective_vendor || d.vendor, d.effective_model || d.model].filter(Boolean).join(' · ') || '—'}</td>
                <td className="px-4 py-3 text-slate-400">{ago(d.last_seen_at)}</td>
                <td className="px-4 py-3 text-right tabular-nums text-slate-300">
                  {d.availability_30d == null ? '—' : `${d.availability_30d.toFixed(2)}%`}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
