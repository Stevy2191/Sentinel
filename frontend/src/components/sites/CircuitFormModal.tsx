import { useState } from 'react'
import { X } from 'lucide-react'
import { useDevices } from '@/hooks/useDevices'
import { usePortChoices } from '@/hooks/usePorts'
import { useSiteProfileActions, type CircuitKind, type SiteCircuit } from '@/hooks/useSiteProfile'
import { CIRCUIT_KINDS } from '@/utils/siteProfile'
import { portTitle } from '@/utils/network'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  siteId: string
  /** The circuit being edited; omitted when adding. */
  initial?: SiteCircuit
  onClose: () => void
  onSaved: () => void
}

// A speed box: blank is null, anything else must be a number.
function speedValue(s: string): number | null | 'bad' {
  const t = s.trim()
  if (t === '') return null
  const n = Number(t)
  return Number.isFinite(n) ? n : 'bad'
}

export default function CircuitFormModal({ siteId, initial, onClose, onSaved }: Props) {
  const { createCircuit, updateCircuit, busy } = useSiteProfileActions(siteId)
  const { devices } = useDevices({ siteId })
  const [provider, setProvider] = useState(initial?.provider ?? '')
  const [kind, setKind] = useState<CircuitKind>(initial?.kind ?? 'fiber')
  const [circuitRef, setCircuitRef] = useState(initial?.circuit_ref ?? '')
  const [down, setDown] = useState(initial?.download_mbps != null ? String(initial.download_mbps) : '')
  const [up, setUp] = useState(initial?.upload_mbps != null ? String(initial.upload_mbps) : '')
  const [phone, setPhone] = useState(initial?.support_phone ?? '')
  const [account, setAccount] = useState(initial?.account_number ?? '')
  const [notes, setNotes] = useState(initial?.notes ?? '')
  // Seeded from the live port, not the stored id: a port whose device has left
  // the site comes back as no port, so saving drops the stale link.
  const [deviceId, setDeviceId] = useState(initial?.port?.device_id ?? '')
  const [interfaceId, setInterfaceId] = useState(initial?.port?.interface_id ?? '')
  const { ports } = usePortChoices(deviceId || undefined)
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    const d = speedValue(down)
    const u = speedValue(up)
    if (d === 'bad' || u === 'bad') {
      setError('Speeds must be numbers in Mbps')
      return
    }
    const input = {
      provider,
      kind,
      circuit_ref: circuitRef.trim() || null,
      download_mbps: d,
      upload_mbps: u,
      support_phone: phone.trim() || null,
      account_number: account.trim() || null,
      notes: notes.trim() || null,
      interface_id: deviceId && interfaceId ? interfaceId : null,
    }
    try {
      if (initial) await updateCircuit(initial.id, input)
      else await createCircuit(input)
      onSaved()
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the circuit')
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form
        className="card max-h-[90vh] w-full max-w-lg space-y-4 overflow-y-auto p-6"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => void submit(e)}
      >
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">{initial ? 'Edit circuit' : 'Add circuit'}</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <div className="grid grid-cols-3 gap-3">
          <label className="col-span-2 block space-y-1">
            <span className="text-sm text-slate-300">Provider</span>
            <input className={inputCls} value={provider} onChange={(e) => setProvider(e.target.value)} maxLength={100} placeholder="Spectrum" required autoFocus />
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Type</span>
            <select className={`${inputCls} cursor-pointer`} value={kind} onChange={(e) => setKind(e.target.value as CircuitKind)}>
              {CIRCUIT_KINDS.map((k) => (
                <option key={k.value} value={k.value}>
                  {k.label}
                </option>
              ))}
            </select>
          </label>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Download (Mbps)</span>
            <input className={inputCls} value={down} onChange={(e) => setDown(e.target.value)} inputMode="decimal" placeholder="500" />
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Upload (Mbps)</span>
            <input className={inputCls} value={up} onChange={(e) => setUp(e.target.value)} inputMode="decimal" placeholder="500" />
          </label>
        </div>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Circuit ID</span>
          <input className={inputCls} value={circuitRef} onChange={(e) => setCircuitRef(e.target.value)} maxLength={100} />
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Support phone</span>
            <input className={inputCls} value={phone} onChange={(e) => setPhone(e.target.value)} maxLength={50} />
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Account number</span>
            <input className={inputCls} value={account} onChange={(e) => setAccount(e.target.value)} maxLength={100} />
          </label>
        </div>
        <fieldset className="space-y-2">
          <legend className="mb-1 text-sm text-slate-300">Port it plugs into</legend>
          <select
            className={`${inputCls} cursor-pointer`}
            value={deviceId}
            onChange={(e) => {
              setDeviceId(e.target.value)
              setInterfaceId('')
            }}
            aria-label="Device"
          >
            <option value="">Not tied to a port</option>
            {devices.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
          {deviceId &&
            (ports === null ? (
              <p className="text-xs text-slate-500">Loading ports…</p>
            ) : (
              <select className={`${inputCls} cursor-pointer`} value={interfaceId} onChange={(e) => setInterfaceId(e.target.value)} aria-label="Port">
                <option value="">Choose a port…</option>
                {ports.map((p) => (
                  <option key={p.id} value={p.id}>
                    {portTitle(p)}
                  </option>
                ))}
              </select>
            ))}
        </fieldset>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Notes</span>
          <textarea className={inputCls} rows={2} value={notes} onChange={(e) => setNotes(e.target.value)} maxLength={1000} />
        </label>
        {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={busy || !provider.trim()}>
            {busy ? 'Saving…' : initial ? 'Save' : 'Add circuit'}
          </button>
        </div>
      </form>
    </div>
  )
}
