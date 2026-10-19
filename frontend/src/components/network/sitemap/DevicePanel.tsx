import { Link } from 'react-router-dom'
import { X } from 'lucide-react'
import { downFor } from '@/components/network/sitemap/MapNode'
import { endText } from '@/utils/siteMap'
import type { MapDevice, MapLink } from '@/types/siteMap'

interface Props {
  device: MapDevice
  links: MapLink[]
  devices: Map<string, MapDevice>
  canEdit: boolean
  isPublic: boolean
  busy: boolean
  onClose: () => void
  onHide: () => void
}

function upFor(s: number | null): string {
  if (s == null) return '—'
  const d = Math.floor(s / 86400)
  if (d > 0) return `${d} d`
  const h = Math.floor(s / 3600)
  return h > 0 ? `${h} h` : `${Math.max(1, Math.floor(s / 60))} min`
}

const STATUS: Record<string, string> = { up: 'Up', paused: 'Paused', pending: 'Waiting for the first poll', error: 'Error' }

/** A device on the map: its state, its links, and the way to its page. */
export default function DevicePanel({ device: d, links, devices, canEdit, isPublic, busy, onClose, onHide }: Props) {
  const now = Date.now()
  const status = d.status === 'down' ? downFor(d.down_since, now) : (STATUS[d.status] ?? d.status)
  return (
    <div className="space-y-3" aria-label="Device">
      <div className="flex items-start justify-between gap-2">
        <div>
          <h3 className="text-[15px] font-medium text-white">{d.name}</h3>
          <p className="text-xs text-slate-400">
            {d.type === 'router' ? 'Router' : d.type === 'switch' ? 'Switch' : 'Device'}
            {d.model ? ` · ${d.model}` : ''}
          </p>
        </div>
        <button type="button" className="rounded p-1 text-slate-400 hover:text-white" onClick={onClose} aria-label="Close">
          <X className="h-4 w-4" />
        </button>
      </div>
      <dl className="divide-y divide-white/10 text-sm">
        <div className="flex justify-between py-1.5">
          <dt className="text-slate-400">Status</dt>
          <dd className={d.status === 'down' ? 'text-red-300' : d.status === 'up' ? 'text-emerald-300' : 'text-slate-300'} title={d.status_detail || undefined}>
            ● {status}
          </dd>
        </div>
        {d.host && (
          <div className="flex justify-between py-1.5">
            <dt className="text-slate-400">Address</dt>
            <dd>{d.host}</dd>
          </div>
        )}
        <div className="flex justify-between py-1.5">
          <dt className="text-slate-400">Up for</dt>
          <dd>{d.status === 'down' ? '—' : upFor(d.uptime_seconds)}</dd>
        </div>
      </dl>
      <div>
        <p className="mb-1 text-xs text-slate-400">Links</p>
        {links.length === 0 && <p className="text-sm text-slate-500">None found yet.</p>}
        <ul className="space-y-1 text-sm">
          {links.map((l) => {
            const [mine, other] = l.a.device_id === d.id ? [l.a, l.b] : [l.b, l.a]
            const o = devices.get(other.device_id)
            return (
              <li key={l.key} className="text-slate-300">
                {endText(mine, d.type)} → {o?.name ?? '?'} {other.if_index != null ? endText(other, o?.type) : ''}
                {l.guess && <span className="text-amber-300"> (guess)</span>}
              </li>
            )
          })}
        </ul>
      </div>
      {d.connected != null && d.connected > 0 && (
        <p className="text-xs text-slate-400">
          {d.connected} end devices are plugged into it; they’re on{' '}
          <Link className="text-sky-400 hover:underline" to={`/network/devices/${d.id}#connected`}>
            its own page
          </Link>
          .
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        {!isPublic && (
          <Link className="btn-primary" to={`/network/devices/${d.id}`}>
            Open device →
          </Link>
        )}
        {canEdit && (
          <button type="button" className="btn-secondary" disabled={busy} onClick={onHide}>
            Hide from the map
          </button>
        )}
      </div>
    </div>
  )
}
