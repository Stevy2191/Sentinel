import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import CircuitFormModal from '@/components/sites/CircuitFormModal'
import type { SiteCircuit } from '@/hooks/useSiteProfile'
import type { ApiError } from '@/services/api'
import { circuitKindLabel, circuitSpeed, usage } from '@/utils/siteProfile'
import { formatBps, portTitle } from '@/utils/network'

interface Props {
  siteId: string
  circuits: SiteCircuit[]
  canEdit: boolean
  onDelete: (id: string) => Promise<unknown>
  onChanged: () => Promise<unknown> | void
}

function UsageRow({ label, bps, mbps }: { label: string; bps: number | null; mbps: number | null }) {
  const u = usage(bps, mbps)
  return (
    <div>
      <div className="flex justify-between gap-2 text-xs">
        <span className="text-slate-400">{label}</span>
        <span className="tabular-nums text-slate-300">
          {formatBps(bps)}
          {u?.pct != null && ` · ${Math.round(u.pct)}%`}
        </span>
      </div>
      {u?.pct != null && (
        <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-white/10">
          <div
            className={`h-full rounded-full ${u.bar >= 90 ? 'bg-red-500' : u.bar >= 75 ? 'bg-amber-500' : 'bg-emerald-500'}`}
            style={{ width: `${u.bar}%` }}
          />
        </div>
      )}
    </div>
  )
}

function Detail({ label, value }: { label: string; value: string | null }) {
  if (!value) return null
  return (
    <div className="flex justify-between gap-3 text-xs">
      <span className="shrink-0 text-slate-500">{label}</span>
      <span className="min-w-0 break-words text-right text-slate-300">{value}</span>
    </div>
  )
}

/** A site's ISP circuits. A circuit tied to a port shows that port's current
 *  in (download) and out (upload) against the circuit's speed. */
export default function CircuitsCard({ siteId, circuits, canEdit, onDelete, onChanged }: Props) {
  const [editing, setEditing] = useState<SiteCircuit | 'new' | null>(null)
  const [confirming, setConfirming] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [deleting, setDeleting] = useState<string | null>(null)

  const remove = async (id: string) => {
    if (deleting) return
    setError(null)
    setDeleting(id)
    try {
      await onDelete(id)
      await onChanged()
      setConfirming(null)
    } catch (err) {
      setError((err as ApiError).message || 'Could not delete the circuit')
    } finally {
      setDeleting(null)
    }
  }

  return (
    <section className="card space-y-3 p-4">
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-lg font-light text-white">Circuits</h2>
        {canEdit && (
          <button className="btn-secondary flex items-center gap-1.5 !py-1 text-sm" onClick={() => setEditing('new')}>
            <Plus className="h-3.5 w-3.5" /> Add
          </button>
        )}
      </div>
      {error && <p className="text-sm text-red-400">{error}</p>}
      {circuits.length === 0 ? (
        <p className="text-sm text-slate-500">{canEdit ? 'No circuits yet.' : 'No circuits recorded.'}</p>
      ) : (
        <ul className="space-y-3">
          {circuits.map((c) => {
            const speed = circuitSpeed(c)
            const port = c.port
            return (
              <li key={c.id} className="space-y-2 rounded-lg border border-white/10 p-3">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="font-medium text-slate-200">{c.provider}</div>
                    <div className="text-xs text-slate-400">
                      {circuitKindLabel(c.kind)}
                      {speed && ` · ${speed}`}
                    </div>
                  </div>
                  {canEdit &&
                    (confirming === c.id ? (
                      <div className="flex shrink-0 items-center gap-1.5 text-xs">
                        <span className="text-amber-300">Delete?</span>
                        <button className="btn-secondary !px-2 !py-0.5 text-xs" disabled={deleting === c.id} onClick={() => { setConfirming(null); setError(null) }}>
                          Cancel
                        </button>
                        <button className="btn bg-red-600 !px-2 !py-0.5 text-xs text-white hover:bg-red-700" disabled={deleting === c.id} onClick={() => void remove(c.id)}>
                          {deleting === c.id ? 'Deleting…' : 'Delete'}
                        </button>
                      </div>
                    ) : (
                      <div className="flex shrink-0 gap-1">
                        <button className="rounded p-1 text-slate-400 hover:bg-white/10 hover:text-white" aria-label={`Edit ${c.provider}`} onClick={() => setEditing(c)}>
                          <Pencil className="h-3.5 w-3.5" />
                        </button>
                        <button className="rounded p-1 text-red-400 hover:bg-red-500/10" aria-label={`Delete ${c.provider}`} onClick={() => { setError(null); setConfirming(c.id) }}>
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    ))}
                </div>
                <div className="space-y-1">
                  <Detail label="Circuit ID" value={c.circuit_ref} />
                  <Detail label="Support" value={c.support_phone} />
                  <Detail label="Account" value={c.account_number} />
                </div>
                {c.notes && <p className="whitespace-pre-wrap break-words text-xs text-slate-500">{c.notes}</p>}
                {port && (
                  <div className="space-y-1.5 border-t border-white/10 pt-2">
                    <Link to={`/network/devices/${port.device_id}/ports/${port.if_index}`} className="block truncate text-xs text-primary-400 hover:underline">
                      {port.device_name} · {portTitle(port)}
                    </Link>
                    {port.oper_status && port.oper_status !== 'up' ? (
                      <p className="text-xs font-medium text-red-400">Port down</p>
                    ) : port.in_bps == null && port.out_bps == null ? (
                      <p className="text-xs text-slate-500">No recent data</p>
                    ) : (
                      <>
                        <UsageRow label="↓ In (download)" bps={port.in_bps} mbps={c.download_mbps} />
                        <UsageRow label="↑ Out (upload)" bps={port.out_bps} mbps={c.upload_mbps} />
                      </>
                    )}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      )}
      {editing && (
        <CircuitFormModal
          siteId={siteId}
          initial={editing === 'new' ? undefined : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            onChanged()
          }}
        />
      )}
    </section>
  )
}
