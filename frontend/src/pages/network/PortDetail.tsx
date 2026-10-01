import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { Star } from 'lucide-react'
import { formatSpeed, useDevice, type PortRole } from '@/hooks/useDevices'
import { useSite } from '@/hooks/useSites'
import { usePort, usePortActions, usePortEvents, type PortDetail as Port, type PortPatch } from '@/hooks/usePorts'
import TrafficChart from '@/components/network/TrafficChart'
import PortEventList from '@/components/network/PortEventList'
import {
  busiestUtil,
  CONDITION_LABEL,
  formatBps,
  formatPct,
  PORT_ROLE_LABEL,
  PORT_ROLES,
  PORT_STATE,
  portState,
  portTitle,
  WARNING_CONDITIONS,
} from '@/utils/network'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-32 rounded-md border border-white/10 bg-slate-900/60 px-3 py-1.5 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

function ThresholdsForm({ port, deviceId, onSaved }: { port: Port; deviceId: string; onSaved: () => void }) {
  const { updatePort, busy } = usePortActions()
  const [util, setUtil] = useState(port.util_threshold_pct?.toString() ?? '')
  const [errs, setErrs] = useState(port.error_threshold_per_min?.toString() ?? '')
  const [grace, setGrace] = useState(port.down_grace_seconds?.toString() ?? '')
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const num = (s: string) => (s.trim() === '' ? null : Number(s))
  const d = port.defaults

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaved(false)
    setError(null)
    try {
      await updatePort(deviceId, port.if_index, {
        util_threshold_pct: num(util),
        error_threshold_per_min: num(errs),
        down_grace_seconds: num(grace),
      })
      setSaved(true)
      onSaved()
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the thresholds')
    }
  }

  return (
    <form onSubmit={(e) => void save(e)} className="flex flex-wrap items-end gap-4">
      <label className="space-y-1">
        <span className="block text-xs text-slate-400">Nearly full at (%)</span>
        <input className={inputCls} type="number" min={10} max={100} value={util} onChange={(e) => setUtil(e.target.value)} placeholder={`Default ${d.util_threshold_pct}`} />
      </label>
      <label className="space-y-1">
        <span className="block text-xs text-slate-400">Errors per minute</span>
        <input className={inputCls} type="number" min={1} value={errs} onChange={(e) => setErrs(e.target.value)} placeholder={`Default ${d.error_threshold_per_min}`} />
      </label>
      <label className="space-y-1">
        <span className="block text-xs text-slate-400">Alert when down for (s)</span>
        <input className={inputCls} type="number" min={0} max={86400} value={grace} onChange={(e) => setGrace(e.target.value)} placeholder={`Default ${d.down_grace_seconds}`} />
      </label>
      <button className="btn-secondary" disabled={busy}>
        Save thresholds
      </button>
      {saved && <span className="text-sm text-emerald-400">Saved</span>}
      {error && <span className="text-sm text-red-400">{error}</span>}
      <p className="basis-full text-xs text-slate-500">Leave a field empty to use the default from Network settings.</p>
    </form>
  )
}

export default function PortDetail() {
  const { id, ifIndex } = useParams<{ id: string; ifIndex: string }>()
  const { device } = useDevice(id)
  const { site } = useSite(device?.site_id)
  const { data: port, loading, notFound, refetch } = usePort(id, ifIndex)
  const { events } = usePortEvents({ deviceId: id, ifIndex: ifIndex ? Number(ifIndex) : undefined }, 30)
  const { updatePort, busy } = usePortActions()
  const [error, setError] = useState<string | null>(null)

  if (loading && !port) return <p className="text-sm text-slate-400">Loading…</p>
  if (notFound || !port || !id) {
    return (
      <div className="card p-8 text-center">
        <p className="text-slate-300">Port not found.</p>
        <Link to={id ? `/network/devices/${id}` : '/network/devices'} className="mt-2 inline-block text-sm text-primary-400 hover:underline">
          Back to the device
        </Link>
      </div>
    )
  }

  const canEdit = site?.access === 'admin' || site?.access === 'editable'
  const st = portState(port)
  const warns = (port.conditions ?? []).filter((c) => WARNING_CONDITIONS.includes(c))
  const scope = { deviceIds: [id], instances: [String(port.if_index)] }
  const change = async (patch: PortPatch) => {
    setError(null)
    try {
      await updatePort(id, port.if_index, patch)
      await refetch()
    } catch (err) {
      setError((err as ApiError).message || 'Could not update the port')
    }
  }

  return (
    <div className="space-y-8">
      <div>
        <Link to={`/network/devices/${id}`} className="text-sm text-slate-400 hover:text-slate-300">
          ← {device?.name ?? 'Device'}
        </Link>
        <div className="mt-2 flex flex-wrap items-center gap-3">
          <h1 className="break-words text-4xl font-light text-white">{portTitle(port)}</h1>
          <span className={`rounded-full border px-2.5 py-0.5 text-xs ${PORT_STATE[st].badge}`}>{PORT_STATE[st].label}</span>
          {port.important && <span className="rounded-full border border-sky-500/30 bg-sky-500/10 px-2.5 py-0.5 text-xs text-sky-300">Important</span>}
        </div>
        {warns.length > 0 && <p className="mt-2 text-sm text-yellow-400">{warns.map((c) => CONDITION_LABEL[c]).join(' · ')}</p>}
      </div>

      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

      <dl className="card grid gap-x-6 gap-y-3 p-5 sm:grid-cols-2 lg:grid-cols-4">
        {[
          ['Interface', port.name || '—'],
          ['Description', port.descr || '—'],
          ['Link', port.oper_status === 'up' ? formatSpeed(port.speed_bps) : 'Down'],
          ['Usual speed', port.usual_speed_bps ? formatSpeed(port.usual_speed_bps) : 'Learning (needs a day of history)'],
          ['In', formatBps(port.in_bps)],
          ['Out', formatBps(port.out_bps)],
          ['Busy', formatPct(busiestUtil(port))],
          ['Errors/min', port.errors_per_min == null ? '—' : String(+port.errors_per_min.toFixed(1))],
          ['Link last changed', port.oper_changed_at ? new Date(port.oper_changed_at).toLocaleString() : '—'],
          ['MAC', port.mac || '—'],
          ['Statistics', port.collected ? 'Collected every poll' : 'Not collected'],
          ['Connects to', PORT_ROLE_LABEL[port.role]],
        ].map(([k, v]) => (
          <div key={k}>
            <dt className="text-xs uppercase tracking-widest text-slate-500">{k}</dt>
            <dd className="mt-0.5 break-words text-sm text-slate-200">{v}</dd>
          </div>
        ))}
      </dl>

      {canEdit && (
        <section className="card space-y-4 p-5">
          <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
            <label className="flex items-center gap-2 text-sm text-slate-200">
              <input type="checkbox" checked={port.important} disabled={busy} onChange={() => void change({ important: !port.important })} />
              <Star className="h-4 w-4 text-amber-400" aria-hidden /> Important: alert on link down, errors, flapping, a slower link or nearly full
            </label>
            <label className="flex items-center gap-2 text-sm text-slate-300">
              <input type="checkbox" checked={port.collected} disabled={busy} onChange={() => void change({ collect: !port.collected })} /> Collect statistics
            </label>
            {port.collect !== null && (
              <button type="button" className="text-xs text-primary-400 hover:underline" onClick={() => void change({ collect: null })}>
                Back to automatic
              </button>
            )}
            <label className="flex items-center gap-2 text-sm text-slate-300">
              Connects to
              <select
                value={port.role}
                disabled={busy}
                onChange={(e) => void change({ role: e.target.value as PortRole })}
                className="cursor-pointer rounded-md border border-white/10 bg-slate-900/60 px-2 py-1 text-sm text-white focus:outline-none focus:ring-2 focus:ring-primary-500"
              >
                {PORT_ROLES.map((r) => (
                  <option key={r} value={r}>
                    {PORT_ROLE_LABEL[r]}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <ThresholdsForm
            key={`${port.util_threshold_pct}-${port.error_threshold_per_min}-${port.down_grace_seconds}`}
            port={port}
            deviceId={id}
            onSaved={() => void refetch()}
          />
        </section>
      )}

      <TrafficChart
        title="Traffic"
        query={scope}
        lines={[
          { metric: 'if_in_bps', label: 'In', colour: '#22d3ee' },
          { metric: 'if_out_bps', label: 'Out', colour: '#a78bfa' },
        ]}
        unit="bps"
      />
      <TrafficChart
        title="How busy"
        query={scope}
        lines={[
          { metric: 'if_in_util_pct', label: 'In', colour: '#22d3ee' },
          { metric: 'if_out_util_pct', label: 'Out', colour: '#a78bfa' },
        ]}
        unit="pct"
        threshold={port.util_threshold_pct ?? port.defaults.util_threshold_pct}
      />
      <TrafficChart
        title="Errors and discards"
        query={scope}
        lines={[
          { metric: 'if_in_errors_pm', label: 'Errors in', colour: '#f87171' },
          { metric: 'if_out_errors_pm', label: 'Errors out', colour: '#fb923c' },
          { metric: 'if_in_discards_pm', label: 'Discards in', colour: '#facc15' },
          { metric: 'if_out_discards_pm', label: 'Discards out', colour: '#94a3b8' },
        ]}
        unit="per_min"
      />

      {port.open_incidents.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-lg font-light text-white">Open incidents</h2>
          <ul className="card divide-y divide-white/10">
            {port.open_incidents.map((i) => (
              <li key={i.id}>
                <Link to={`/incidents/${i.id}`} className="block p-3 text-sm text-red-400 hover:bg-white/5">
                  {CONDITION_LABEL[i.condition ?? ''] ?? 'Incident'} since {new Date(i.start_time).toLocaleString()}
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="space-y-3">
        <h2 className="text-lg font-light text-white">Events</h2>
        <PortEventList events={events} emptyText="Nothing has happened on this port yet." />
      </section>
    </div>
  )
}
