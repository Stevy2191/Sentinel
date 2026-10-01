import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Pause, Pencil, Play, RefreshCw, SlidersHorizontal, Trash2 } from 'lucide-react'
import { useSite } from '@/hooks/useSites'
import { DEVICE_TYPE_LABEL, formatSpeed, useDevice, useDeviceActions, type Device } from '@/hooks/useDevices'
import { useIncidents, formatDuration, DEFAULT_FILTERS } from '@/hooks/useIncidents'
import { useDevicePorts, usePortEvents, type PortView } from '@/hooks/usePorts'
import DeviceStatusBadge from '@/components/network/DeviceStatusBadge'
import DeviceFormModal from '@/components/network/DeviceFormModal'
import EditDetailsModal from '@/components/network/EditDetailsModal'
import Faceplate, { FaceplateLegend } from '@/components/network/Faceplate'
import TrafficChart from '@/components/network/TrafficChart'
import PortTable from '@/components/network/PortTable'
import PortEventList from '@/components/network/PortEventList'
import { busiestUtil, CONDITION_LABEL, formatBps, formatPct, PORT_STATE, portState, portTitle, WARNING_CONDITIONS } from '@/utils/network'
import type { ApiError } from '@/services/api'

/** Device types drawn as a faceplate; the rest get the port table only. */
const FACEPLATE_TYPES = new Set(['switch', 'router'])

const TRAFFIC_LINES = [
  { metric: 'if_in_bps', label: 'In', colour: '#22d3ee' },
  { metric: 'if_out_bps', label: 'Out', colour: '#a78bfa' },
]

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

function PortQuickPanel({ deviceId, port }: { deviceId: string; port: PortView }) {
  const st = portState(port)
  const warns = (port.conditions ?? []).filter((c) => WARNING_CONDITIONS.includes(c))
  const facts: [string, string][] = [
    ['Link', port.oper_status === 'up' ? formatSpeed(port.speed_bps) : '—'],
    ['In', formatBps(port.in_bps)],
    ['Out', formatBps(port.out_bps)],
    ['Busy', formatPct(busiestUtil(port))],
    ['Errors/min', port.errors_per_min == null ? '—' : String(+port.errors_per_min.toFixed(1))],
  ]
  return (
    <div className="card flex flex-wrap items-start gap-x-8 gap-y-3 p-4">
      <div className="min-w-[12rem]">
        <p className="font-medium text-white">{portTitle(port)}</p>
        <div className="mt-1 flex flex-wrap items-center gap-2">
          <span className={`rounded-full border px-2 py-0.5 text-xs ${PORT_STATE[st].badge}`}>{PORT_STATE[st].label}</span>
          {port.important && <span className="rounded-full border border-sky-500/30 bg-sky-500/10 px-2 py-0.5 text-xs text-sky-300">Important</span>}
          {warns.map((c) => (
            <span key={c} className="text-xs text-yellow-400">
              {CONDITION_LABEL[c]}
            </span>
          ))}
        </div>
      </div>
      {facts.map(([k, v]) => (
        <div key={k}>
          <p className="text-[11px] uppercase tracking-widest text-slate-500">{k}</p>
          <p className="text-sm tabular-nums text-slate-200">{v}</p>
        </div>
      ))}
      <Link to={`/network/devices/${deviceId}/ports/${port.if_index}`} className="ml-auto self-center text-sm text-primary-400 hover:underline">
        Open port page →
      </Link>
    </div>
  )
}

export default function DeviceDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { device, loading, notFound, refetch } = useDevice(id)
  const { site } = useSite(device?.site_id)
  const { data: portsView, refetch: refetchPorts } = useDevicePorts(id)
  const { events } = usePortEvents({ deviceId: id }, 15)
  const { incidents } = useIncidents({ ...DEFAULT_FILTERS, limit: 10, deviceId: id })
  const { update, remove, refresh, busy } = useDeviceActions()
  const [editing, setEditing] = useState(false)
  const [editingDetails, setEditingDetails] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [selected, setSelected] = useState<number | null>(null)

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
  const type = device.effective_type ?? device.device_type_detected ?? 'other'
  const ports = portsView?.ports ?? []
  const showFaceplate = FACEPLATE_TYPES.has(type) && (portsView?.faceplate.blocks.length ?? 0) > 0
  const selectedPort = ports.find((p) => p.if_index === selected) ?? null

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
            <button className="btn-secondary flex items-center gap-2" onClick={() => setEditingDetails(true)}>
              <SlidersHorizontal className="h-4 w-4" /> Edit details
            </button>
            <button className="btn-secondary flex items-center gap-2" onClick={() => setEditing(true)}>
              <Pencil className="h-4 w-4" /> Edit
            </button>
            <button className="btn-secondary flex items-center gap-2" disabled={busy} onClick={() => void act(() => update(device.id, toInput(device, !device.enabled)))}>
              {device.enabled ? <><Pause className="h-4 w-4" /> Pause</> : <><Play className="h-4 w-4" /> Resume</>}
            </button>
            {confirmDelete ? (
              <>
                <span className="self-center text-xs text-slate-400">Its incident history and graphs are deleted too.</span>
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
          ['Vendor', device.effective_vendor || device.vendor || '—'],
          ['Model', device.effective_model || device.model || '—'],
          ['Type', DEVICE_TYPE_LABEL[type] ?? 'Other'],
          ['Serial', device.serial || '—'],
          ['Location', device.effective_location || device.sys_location || '—'],
          ['Contact', device.sys_contact || '—'],
          ['Up since', upSince(device)],
          ['Last seen', device.last_seen_at ? new Date(device.last_seen_at).toLocaleString() : 'never'],
          ['Credential profile', device.credential_name || '—'],
          ['30-day availability', device.availability_30d == null ? '—' : `${device.availability_30d.toFixed(2)}%`],
          ['Stats poll', device.last_stats_duration_ms == null ? 'not yet' : `${device.last_stats_duration_ms} ms`],
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

      {showFaceplate && portsView && (
        <section className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h2 className="text-lg font-light text-white">Front panel</h2>
            <FaceplateLegend />
          </div>
          <Faceplate
            layout={portsView.faceplate}
            ports={ports}
            title={device.name}
            subtitle={device.effective_model || device.host}
            selected={selected}
            onSelect={(i) => setSelected((s) => (s === i ? null : i))}
          />
          {selectedPort && <PortQuickPanel deviceId={device.id} port={selectedPort} />}
        </section>
      )}

      <TrafficChart title="Traffic" query={{ deviceIds: [device.id], sum: true, physicalOnly: true }} lines={TRAFFIC_LINES} unit="bps" />

      <PortTable deviceId={device.id} ports={ports} canEdit={canEdit} onChanged={() => void refetchPorts()} />

      <section className="space-y-3">
        <h2 className="text-lg font-light text-white">Recent port events</h2>
        <PortEventList events={events} />
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
                    {inc.status === 'ongoing' ? 'Ongoing' : 'Resolved'} · {new Date(inc.start_time).toLocaleString()} ·{' '}
                    {inc.port_if_index != null ? `${inc.subject_name}: ${CONDITION_LABEL[inc.condition ?? ''] ?? 'problem'}` : 'Device unreachable'}
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
      {editingDetails && (
        <EditDetailsModal device={device} onClose={() => setEditingDetails(false)} onSaved={() => { setEditingDetails(false); void refetch(); void refetchPorts() }} />
      )}
    </div>
  )
}
