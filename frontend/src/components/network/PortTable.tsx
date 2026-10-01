import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ChevronRight, Star } from 'lucide-react'
import { formatSpeed, type PortRole } from '@/hooks/useDevices'
import { usePortActions, type PortPatch, type PortView } from '@/hooks/usePorts'
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
  type PortState,
} from '@/utils/network'
import type { ApiError } from '@/services/api'

/** Beyond this many rows (after filtering), the table caps its height and
 *  scrolls with a sticky header, like MonitorTable. */
const SCROLL_THRESHOLD = 24

/** Stops a control's click (and so its bubbled-up row navigation) without
 *  stopping the control itself from doing its own thing. */
function stopRowClick(e: React.SyntheticEvent) {
  e.stopPropagation()
}

type SortKey = 'number' | 'alias' | 'state' | 'speed' | 'in' | 'out' | 'busy' | 'errors'
const STATE_ORDER: Record<PortState, number> = { critical: 0, warning: 1, up: 2, down: 3, disabled: 4 }

function sortValue(p: PortView, key: SortKey): number | string {
  switch (key) {
    case 'number':
      // (stack_unit, slot, number): ports group by stack member, then by
      // network module, before sorting by their own number, so the table
      // doesn't interleave "1/12" and "2/5", or "0/3" and "1/3".
      return p.stack_unit * 1_000_000 + p.slot * 10_000 + p.number
    case 'alias':
      return (p.alias || p.name).toLowerCase()
    case 'state':
      return STATE_ORDER[portState(p)]
    case 'speed':
      return p.oper_status === 'up' ? p.speed_bps : -1
    case 'in':
      return p.in_bps ?? -1
    case 'out':
      return p.out_bps ?? -1
    case 'busy':
      return busiestUtil(p) ?? -1
    case 'errors':
      return p.errors_per_min ?? -1
  }
}

interface Props {
  deviceId: string
  ports: PortView[]
  canEdit: boolean
  onChanged: () => void
}

/** Every port with its live figures; sortable, with the important star and
 *  the collect switch for editors. */
export default function PortTable({ deviceId, ports, canEdit, onChanged }: Props) {
  const navigate = useNavigate()
  const { updatePort, busy } = usePortActions()
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: 'number', desc: false })
  const [showAll, setShowAll] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const rows = useMemo(() => {
    const list = ports.filter((p) => showAll || p.physical)
    return [...list].sort((a, b) => {
      const x = sortValue(a, sort.key)
      const y = sortValue(b, sort.key)
      const c = x < y ? -1 : x > y ? 1 : a.if_index - b.if_index
      return sort.desc ? -c : c
    })
  }, [ports, showAll, sort])

  const change = async (p: PortView, patch: PortPatch) => {
    setError(null)
    try {
      await updatePort(deviceId, p.if_index, patch)
      onChanged()
    } catch (err) {
      setError((err as ApiError).message || 'Could not update the port')
    }
  }

  const head = (key: SortKey, label: string, right = false) => (
    <th className={`px-3 py-2 font-medium ${right ? 'text-right' : ''}`}>
      <button
        type="button"
        className="hover:text-slate-200"
        onClick={() => setSort((s) => ({ key, desc: s.key === key ? !s.desc : key !== 'number' && key !== 'alias' }))}
      >
        {label}
        {sort.key === key ? (sort.desc ? ' ↓' : ' ↑') : ''}
      </button>
    </th>
  )

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-light text-white">Ports ({rows.length})</h2>
        <label className="flex items-center gap-2 text-sm text-slate-400">
          <input type="checkbox" checked={showAll} onChange={(e) => setShowAll(e.target.checked)} /> Show aggregates and virtual interfaces
        </label>
      </div>
      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}
      {rows.length === 0 ? (
        <p className="text-sm text-slate-500">No ports reported yet. The list arrives with the first inventory.</p>
      ) : (
        <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40">
          {/* max-h + overflow-auto on this same element, not a separate
              horizontal-only wrapper — see MonitorTable.tsx's comment for why
              splitting the two scroll axes across nested divs breaks the
              sticky header. Below the threshold, this is just the ordinary
              horizontal-scroll wrapper with no height cap. */}
          <div className={rows.length > SCROLL_THRESHOLD ? 'max-h-[65vh] overflow-auto' : 'overflow-x-auto'}>
            <table className="w-full text-sm">
              <thead className={rows.length > SCROLL_THRESHOLD ? 'sticky top-0 z-10' : undefined}>
                <tr className={`border-b border-white/10 text-left text-xs text-slate-400 ${rows.length > SCROLL_THRESHOLD ? 'bg-slate-900' : ''}`}>
                  <th className="w-8 px-3 py-2">
                    <span className="sr-only">Important</span>
                  </th>
                  {head('number', 'Port')}
                  {head('alias', 'Name')}
                  {head('state', 'Status')}
                  {head('speed', 'Speed')}
                  {head('in', 'In', true)}
                  {head('out', 'Out', true)}
                  {head('busy', 'Busy', true)}
                  {head('errors', 'Errors/min', true)}
                  <th className="px-3 py-2 font-medium">Role</th>
                  <th className="px-3 py-2 font-medium">Collect</th>
                  <th className="w-8 px-3 py-2">
                    <span className="sr-only">Open</span>
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {rows.map((p) => {
                  const st = portState(p)
                  const warns = (p.conditions ?? []).filter((c) => WARNING_CONDITIONS.includes(c))
                  return (
                    <tr
                      key={p.id}
                      onClick={() => navigate(`/network/devices/${deviceId}/ports/${p.if_index}`)}
                      className={`cursor-pointer hover:bg-white/5 ${p.collected ? '' : 'opacity-60'}`}
                    >
                      <td className="px-3 py-2">
                        {canEdit ? (
                          <button
                            type="button"
                            disabled={busy}
                            onClick={(e) => {
                              stopRowClick(e)
                              void change(p, { important: !p.important })
                            }}
                            aria-label={p.important ? 'Stop treating as important' : 'Mark as important'}
                            title={p.important ? 'Important: alerts are on' : 'Mark as important to get alerts'}
                          >
                            <Star className={`h-4 w-4 ${p.important ? 'fill-amber-400 text-amber-400' : 'text-slate-600 hover:text-slate-400'}`} />
                          </button>
                        ) : (
                          p.important && <Star className="h-4 w-4 fill-amber-400 text-amber-400" aria-label="Important" />
                        )}
                      </td>
                      <td className="px-3 py-2 font-medium tabular-nums">
                        <Link
                          to={`/network/devices/${deviceId}/ports/${p.if_index}`}
                          onClick={stopRowClick}
                          className="text-slate-200 hover:text-primary-400"
                          title={p.name}
                        >
                          {p.stack_unit > 0 ? `${p.stack_unit}/${p.label}` : p.label}
                        </Link>
                      </td>
                      <td className="max-w-[16rem] truncate px-3 py-2 text-slate-300" title={p.alias || p.name}>
                        {p.alias || <span className="text-slate-500">{p.name}</span>}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2">
                        <span className={`inline-flex rounded-full border px-2 py-0.5 text-xs ${PORT_STATE[st].badge}`}>{PORT_STATE[st].label}</span>
                        {warns.length > 0 && <span className="ml-2 text-xs text-yellow-400">{warns.map((c) => CONDITION_LABEL[c]).join(', ')}</span>}
                      </td>
                      <td className="px-3 py-2 tabular-nums text-slate-400">{p.oper_status === 'up' ? formatSpeed(p.speed_bps) : '—'}</td>
                      <td className="px-3 py-2 text-right tabular-nums text-slate-300">{formatBps(p.in_bps)}</td>
                      <td className="px-3 py-2 text-right tabular-nums text-slate-300">{formatBps(p.out_bps)}</td>
                      <td className="px-3 py-2 text-right tabular-nums text-slate-300">{formatPct(busiestUtil(p))}</td>
                      <td className="px-3 py-2 text-right tabular-nums text-slate-300">
                        {p.errors_per_min == null ? '—' : +p.errors_per_min.toFixed(1)}
                      </td>
                      <td className="max-w-[12rem] px-3 py-2">
                        {canEdit ? (
                          <select
                            value={p.role}
                            disabled={busy}
                            onClick={stopRowClick}
                            onChange={(e) => void change(p, { role: e.target.value as PortRole })}
                            aria-label={`Connects to, for port ${p.label}`}
                            className="w-full cursor-pointer truncate rounded-md border border-white/10 bg-slate-900/60 px-2 py-1 text-xs text-slate-200 focus:outline-none focus:ring-2 focus:ring-primary-500"
                          >
                            {PORT_ROLES.map((r) => (
                              <option key={r} value={r}>
                                {PORT_ROLE_LABEL[r]}
                              </option>
                            ))}
                          </select>
                        ) : (
                          <span className="text-slate-400">{PORT_ROLE_LABEL[p.role]}</span>
                        )}
                        {p.role === 'uplink' && p.neighbor_device_name && (
                          <p className="mt-0.5 truncate text-xs text-slate-500">
                            → {p.neighbor_device_name}
                            {p.neighbor_port_number != null &&
                              ` · ${portTitle({ number: p.neighbor_port_number, label: p.neighbor_port_label, alias: p.neighbor_port_alias, stack_unit: p.neighbor_port_unit })}`}
                          </p>
                        )}
                      </td>
                      <td className="px-3 py-2">
                        {canEdit ? (
                          <input
                            type="checkbox"
                            checked={p.collected}
                            disabled={busy}
                            onClick={stopRowClick}
                            onChange={() => void change(p, { collect: !p.collected })}
                            aria-label={`Collect statistics for port ${p.label}`}
                          />
                        ) : (
                          <span className="text-slate-400">{p.collected ? 'Yes' : 'No'}</span>
                        )}
                      </td>
                      <td className="px-3 py-2 text-slate-600">
                        <ChevronRight className="h-4 w-4" aria-hidden />
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </section>
  )
}
