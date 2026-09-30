import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Pause, Pencil, Play, RefreshCw, Trash2 } from 'lucide-react'
import { useSite } from '@/hooks/useSites'
import { formatSpeed, useDevice, useDeviceActions, useDeviceInterfaces, type Device } from '@/hooks/useDevices'
import { useIncidents, formatDuration, DEFAULT_FILTERS } from '@/hooks/useIncidents'
import DeviceStatusBadge from '@/components/network/DeviceStatusBadge'
import DeviceFormModal from '@/components/network/DeviceFormModal'
import type { ApiError } from '@/services/api'

const PORT_TONE: Record<string, string> = {
  up: 'text-emerald-400',
  down: 'text-red-400',
  lowerLayerDown: 'text-red-400',
  dormant: 'text-slate-400',
  notPresent: 'text-slate-500',
}

function upSince(d: Device): string {
  if (d.sys_uptime_seconds == null || !d.last_seen_at) return '—'
  const since = new Date(new Date(d.last_seen_at).getTime() - d.sys_uptime_seconds * 1000)
  return `${since.toLocaleString()} (${formatDuration(d.sys_uptime_seconds)})`
}

function toInput(d: Device, enabled: boolean) {
  return {
    site_id: d.site_id, credential_id: d.credential_id, name: d.name, host: d.host, port: d.port, enabled,
    poll_interval: d.poll_interval, timeout_ms: d.timeout_ms, retries: d.retries, notify_channels: d.notify_channels,
  }
}

export default function DeviceDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { device, loading, notFound, refetch } = useDevice(id)
  const { site } = useSite(device?.site_id)
  const [showAbsent, setShowAbsent] = useState(false)
  const [hideAdminDown, setHideAdminDown] = useState(false)
  const { interfaces } = useDeviceInterfaces(id, showAbsent)
  const { incidents } = useIncidents({ ...DEFAULT_FILTERS, limit: 10, deviceId: id })
  const { update, remove, refresh, busy } = useDeviceActions()
  const [editing, setEditing] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (loading) return <p className="text-sm text-slate-400">Loading…</p>
  if (notFound || !device) {
    return (
      <div className="card p-8 text-center">
        <p className="text-slate-300">Device not found.</p>
        <Link to="/network/devices" className="mt-2 inline-block text-sm text-primary-400 hover:underline">
          Back to devices
        </Link>
      </div>
    )
  }

  const canEdit = site?.access === 'admin' || site?.access === 'editable'
  const act = async (fn: () => Promise<unknown>) => {
    setError(null)
    try {
      await fn()
      await refetch()
    } catch (err) {
      setError((err as ApiError).message || 'Something went wrong')
    }
  }
  const ports = interfaces.filter((i) => !hideAdminDown || i.admin_status !== 'down')

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <Link to={`/network/sites/${device.site_id}`} className="text-sm text-slate-400 hover:text-slate-300">
            ← {device.site_name}
          </Link>
          <div className="mt-2 flex flex-wrap items-center gap-3">
            <h1 className="break-words text-4xl font-light text-white">{device.name}</h1>
            <DeviceStatusBadge status={device.status} />
          </div>
          <p className="mt-1 font-mono text-sm text-slate-400">
            {device.host}
            {device.port !== 161 && `:${device.port}`}
          </p>
          {device.status_detail && <p className="mt-1 text-sm text-amber-400">{device.status_detail}</p>}
        </div>
        {canEdit && (
          <div className="flex flex-wrap gap-2">
            <button className="btn-secondary flex items-center gap-2" disabled={busy || !device.enabled} onClick={() => void act(() => refresh(device.id))}>
              <RefreshCw className="h-4 w-4" /> Refresh now
            </button>
            <button className="btn-secondary flex items-center gap-2" onClick={() => setEditing(true)}>
              <Pencil className="h-4 w-4" /> Edit
            </button>
            <button className="btn-secondary flex items-center gap-2" disabled={busy}
              onClick={() => void act(() => update(device.id, toInput(device, !device.enabled)))}>
              {device.enabled ? <><Pause className="h-4 w-4" /> Pause</> : <><Play className="h-4 w-4" /> Resume</>}
            </button>
            {confirmDelete ? (
              <>
                <span className="self-center text-xs text-slate-400">Its incident history is deleted too.</span>
                <button className="btn-secondary" onClick={() => setConfirmDelete(false)}>Cancel</button>
                <button className="btn bg-red-600 text-white hover:bg-red-700" disabled={busy}
                  onClick={() => void act(async () => { await remove(device.id); navigate(`/network/sites/${device.site_id}`) })}>
                  Delete device
                </button>
              </>
            ) : (
              <button className="btn-secondary flex items-center gap-2 text-red-400" onClick={() => setConfirmDelete(true)}>
                <Trash2 className="h-4 w-4" /> Delete
              </button>
            )}
          </div>
        )}
      </div>

      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

      <dl className="card grid gap-x-6 gap-y-3 p-5 sm:grid-cols-2 lg:grid-cols-3">
        {[
          ['Vendor', device.vendor || '—'],
          ['Model', device.model || '—'],
          ['Serial', device.serial || '—'],
          ['Location', device.sys_location || '—'],
          ['Contact', device.sys_contact || '—'],
          ['Up since', upSince(device)],
          ['Last seen', device.last_seen_at ? new Date(device.last_seen_at).toLocaleString() : 'never'],
          ['Credential profile', device.credential_name || '—'],
          ['30-day availability', device.availability_30d == null ? '—' : `${device.availability_30d.toFixed(2)}%`],
        ].map(([k, v]) => (
          <div key={k}>
            <dt className="text-xs uppercase tracking-widest text-slate-500">{k}</dt>
            <dd className="mt-0.5 break-words text-sm text-slate-200">{v}</dd>
          </div>
        ))}
        {device.sys_descr && (
          <div className="sm:col-span-2 lg:col-span-3">
            <dt className="text-xs uppercase tracking-widest text-slate-500">Description</dt>
            <dd className="mt-0.5 break-words text-sm text-slate-400">{device.sys_descr}</dd>
          </div>
        )}
      </dl>

      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-lg font-light text-white">Interfaces ({ports.length})</h2>
          <div className="flex gap-4 text-sm text-slate-400">
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={hideAdminDown} onChange={(e) => setHideAdminDown(e.target.checked)} /> Hide disabled ports
            </label>
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={showAbsent} onChange={(e) => setShowAbsent(e.target.checked)} /> Show removed ports
            </label>
          </div>
        </div>
        {ports.length === 0 ? (
          <p className="text-sm text-slate-500">{device.last_inventory_at ? 'No interfaces reported.' : 'The interface list arrives with the first inventory.'}</p>
        ) : (
          <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40">
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                    <th className="px-4 py-2 font-medium">Port</th>
                    <th className="px-4 py-2 font-medium">Description</th>
                    <th className="px-4 py-2 font-medium">Admin</th>
                    <th className="px-4 py-2 font-medium">Link</th>
                    <th className="px-4 py-2 font-medium">Speed</th>
                    <th className="px-4 py-2 font-medium">MAC</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {ports.map((p) => (
                    <tr key={p.id} className={p.present ? '' : 'opacity-50'}>
                      <td className="px-4 py-2 font-medium text-slate-200" title={p.descr}>{p.name}</td>
                      <td className="px-4 py-2 text-slate-400">{p.alias || '—'}</td>
                      <td className={`px-4 py-2 ${p.admin_status === 'down' ? 'text-slate-500' : 'text-slate-300'}`}>{p.admin_status || '—'}</td>
                      <td className={`px-4 py-2 ${PORT_TONE[p.oper_status] ?? 'text-slate-400'}`}>{p.present ? p.oper_status || '—' : 'removed'}</td>
                      <td className="px-4 py-2 tabular-nums text-slate-400">{formatSpeed(p.speed_bps)}</td>
                      <td className="px-4 py-2 font-mono text-xs text-slate-500">{p.mac || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </section>

      <section className="space-y-3">
        <h2 className="text-lg font-light text-white">Incidents</h2>
        {incidents.length === 0 ? (
          <p className="text-sm text-slate-500">No incidents recorded for this device.</p>
        ) : (
          <ul className="card divide-y divide-white/10">
            {incidents.map((inc) => (
              <li key={inc.id}>
                <Link to={`/incidents/${inc.id}`} className="flex flex-wrap items-center justify-between gap-2 p-3 text-sm hover:bg-white/5">
                  <span className={inc.status === 'ongoing' ? 'text-red-400' : 'text-slate-300'}>
                    {inc.status === 'ongoing' ? 'Ongoing' : 'Resolved'} · {new Date(inc.start_time).toLocaleString()}
                  </span>
                  <span className="text-slate-400">{formatDuration(inc.duration_seconds)}</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </section>

      {editing && (
        <DeviceFormModal initial={device} onClose={() => setEditing(false)} onSaved={() => { setEditing(false); void refetch() }} />
      )}
    </div>
  )
}
