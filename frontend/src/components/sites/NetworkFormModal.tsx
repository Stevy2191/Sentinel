import { useState } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { useSiteProfileActions, type SiteNetwork } from '@/hooks/useSiteProfile'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  siteId: string
  /** The network being edited; omitted when adding. */
  initial?: SiteNetwork
  onClose: () => void
  onSaved: () => void
}

export default function NetworkFormModal({ siteId, initial, onClose, onSaved }: Props) {
  const { createNetwork, updateNetwork, busy } = useSiteProfileActions(siteId)
  const [name, setName] = useState(initial?.name ?? '')
  const [cidr, setCidr] = useState(initial?.cidr ?? '')
  const [vlan, setVlan] = useState(initial?.vlan != null ? String(initial.vlan) : '')
  const [gateway, setGateway] = useState(initial?.gateway ?? '')
  const [note, setNote] = useState(initial?.note ?? '')
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    const vlanNum = vlan.trim() === '' ? null : Number(vlan.trim())
    if (vlanNum !== null && !Number.isInteger(vlanNum)) {
      setError('VLAN must be a whole number')
      return
    }
    // Blank optional fields are sent as null; the server trims and checks the rest.
    const input = { name, cidr, vlan: vlanNum, gateway: gateway.trim() || null, note: note.trim() || null }
    try {
      if (initial) await updateNetwork(initial.id, input)
      else await createNetwork(input)
      onSaved()
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the network')
    }
  }

  // Rendered at the end of <body>: the cards it opens from use backdrop-filter,
  // which traps a fixed-position overlay inside the card instead of the page.
  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form className="card w-full max-w-md space-y-4 p-6" onClick={(e) => e.stopPropagation()} onSubmit={(e) => void submit(e)}>
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">{initial ? 'Edit network' : 'Add network'}</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Name</span>
          <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} maxLength={100} placeholder="Staff" required autoFocus />
        </label>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Subnet</span>
          <input className={inputCls} value={cidr} onChange={(e) => setCidr(e.target.value)} placeholder="10.20.0.0/24" required />
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">VLAN <span className="text-slate-500">(optional)</span></span>
            <input className={inputCls} value={vlan} onChange={(e) => setVlan(e.target.value)} inputMode="numeric" placeholder="1–4094" />
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Gateway <span className="text-slate-500">(optional)</span></span>
            <input className={inputCls} value={gateway} onChange={(e) => setGateway(e.target.value)} placeholder="10.20.0.1" />
          </label>
        </div>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Note <span className="text-slate-500">(optional)</span></span>
          <input className={inputCls} value={note} onChange={(e) => setNote(e.target.value)} maxLength={500} />
        </label>
        {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={busy || !name.trim() || !cidr.trim()}>
            {busy ? 'Saving…' : initial ? 'Save' : 'Add network'}
          </button>
        </div>
      </form>
    </div>,
    document.body
  )
}
