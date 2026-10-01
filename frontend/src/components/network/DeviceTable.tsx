import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { DEVICE_TYPE_LABEL, type Device, type DeviceStatus } from '@/hooks/useDevices'
import { deviceType, vendorModel } from '@/utils/devices'
import DeviceStatusBadge from './DeviceStatusBadge'

function ago(iso: string | null): string {
  if (!iso) return 'never'
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 90) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)} min ago`
  if (s < 86400) return `${Math.round(s / 3600)} h ago`
  return new Date(iso).toLocaleDateString()
}

type SortKey = 'status' | 'name' | 'site' | 'type' | 'host' | 'model' | 'seen' | 'availability'

// Problems first, so sorting by status puts what needs attention on top.
const STATUS_ORDER: Record<DeviceStatus, number> = { down: 0, error: 1, pending: 2, up: 3, paused: 4 }

/** Sorts IPv4 addresses numerically (10.0.0.9 before 10.0.0.10); names after. */
function hostKey(host: string): string {
  const m = host.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/)
  return m ? m.slice(1).map((n) => n.padStart(3, '0')).join('.') : `~${host.toLowerCase()}`
}

function sortValue(d: Device, key: SortKey): number | string {
  switch (key) {
    case 'status':
      return STATUS_ORDER[d.status] ?? 9
    case 'name':
      return d.name.toLowerCase()
    case 'site':
      return (d.site_name ?? '').toLowerCase()
    case 'type':
      return DEVICE_TYPE_LABEL[deviceType(d)]
    case 'host':
      return hostKey(d.host)
    case 'model':
      return vendorModel(d).toLowerCase() || '~'
    case 'seen':
      return d.last_seen_at ? new Date(d.last_seen_at).getTime() : 0
    case 'availability':
      return d.availability_30d ?? -1
  }
}

/** The device list. Click a column heading to sort by it; click again to
 *  reverse. Rows open the device page. */
export default function DeviceTable({ devices, showSite }: { devices: Device[]; showSite: boolean }) {
  const navigate = useNavigate()
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: 'name', desc: false })

  const sorted = useMemo(() => {
    const dir = sort.desc ? -1 : 1
    return [...devices].sort((a, b) => {
      const x = sortValue(a, sort.key)
      const y = sortValue(b, sort.key)
      if (x < y) return -dir
      if (x > y) return dir
      return a.name.localeCompare(b.name)
    })
  }, [devices, sort])

  // Last seen and availability start newest/highest first; the rest A to Z.
  const startsDesc = (key: SortKey) => key === 'seen' || key === 'availability'
  const head = (key: SortKey, label: string, right = false) => (
    <th
      className={`px-4 py-3 font-medium ${right ? 'text-right' : ''}`}
      aria-sort={sort.key === key ? (sort.desc ? 'descending' : 'ascending') : 'none'}
    >
      <button
        type="button"
        className="hover:text-slate-200"
        onClick={() => setSort((s) => ({ key, desc: s.key === key ? !s.desc : startsDesc(key) }))}
      >
        {label}
        {sort.key === key ? (sort.desc ? ' ↓' : ' ↑') : ''}
      </button>
    </th>
  )

  return (
    <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-white/10 text-left text-xs text-slate-400">
              {head('status', 'Status')}
              {head('name', 'Name')}
              {showSite && head('site', 'Site')}
              {head('type', 'Type')}
              {head('host', 'Address')}
              {head('model', 'Vendor / model')}
              {head('seen', 'Last seen')}
              {head('availability', '30-day availability', true)}
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5">
            {sorted.map((d) => (
              <tr key={d.id} className="cursor-pointer transition hover:bg-white/5" onClick={() => navigate(`/network/devices/${d.id}`)}>
                <td className="px-4 py-3">
                  <DeviceStatusBadge status={d.status} detail={d.status_detail} />
                </td>
                <td className="px-4 py-3 font-medium text-slate-200">{d.name}</td>
                {showSite && <td className="px-4 py-3 text-slate-400">{d.site_name}</td>}
                <td className="px-4 py-3 text-slate-400">{DEVICE_TYPE_LABEL[deviceType(d)]}</td>
                <td className="px-4 py-3 font-mono text-xs text-slate-400">
                  {d.host}
                  {d.port !== 161 && `:${d.port}`}
                </td>
                <td className="px-4 py-3 text-slate-400">{vendorModel(d) || '—'}</td>
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
