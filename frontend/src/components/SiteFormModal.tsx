import { useState } from 'react'
import { X } from 'lucide-react'
import { useSiteActions, type Site } from '@/hooks/useSites'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  /** The site being edited; omitted when creating. */
  initial?: Site
  onClose: () => void
  onSaved: (site: Site) => void
}

export default function SiteFormModal({ initial, onClose, onSaved }: Props) {
  const { create, update, busy } = useSiteActions()
  const [name, setName] = useState(initial?.name ?? '')
  const [description, setDescription] = useState(initial?.description ?? '')
  const [street, setStreet] = useState(initial?.street ?? '')
  const [city, setCity] = useState(initial?.city ?? '')
  const [state, setState] = useState(initial?.state ?? '')
  const [zip, setZip] = useState(initial?.zip ?? '')
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    // Blank optional fields are sent as null; the server trims and does the same.
    const input = {
      name,
      description: description.trim() || null,
      street: street.trim() || null,
      city: city.trim() || null,
      state: state.trim() || null,
      zip: zip.trim() || null,
    }
    try {
      const saved = initial ? await update(initial.id, input) : await create(input)
      onSaved(saved)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to save site')
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form
        className="card w-full max-w-md space-y-4 p-6"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => void submit(e)}
      >
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">{initial ? 'Edit site' : 'Add site'}</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Name</span>
          <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} maxLength={255} required autoFocus />
        </label>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Description <span className="text-slate-500">(optional)</span></span>
          <textarea className={inputCls} rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
        </label>
        <fieldset className="space-y-2">
          <legend className="mb-1 text-sm text-slate-300">
            Address <span className="text-slate-500">(optional)</span>
          </legend>
          <label className="block space-y-1">
            <span className="text-xs text-slate-400">Street</span>
            <input className={inputCls} value={street} onChange={(e) => setStreet(e.target.value)} autoComplete="street-address" />
          </label>
          <div className="grid grid-cols-6 gap-2">
            <label className="col-span-6 space-y-1 sm:col-span-3">
              <span className="text-xs text-slate-400">City</span>
              <input className={inputCls} value={city} onChange={(e) => setCity(e.target.value)} autoComplete="address-level2" />
            </label>
            <label className="col-span-2 space-y-1 sm:col-span-1">
              <span className="text-xs text-slate-400">State</span>
              <input className={inputCls} value={state} onChange={(e) => setState(e.target.value)} autoComplete="address-level1" />
            </label>
            <label className="col-span-4 space-y-1 sm:col-span-2">
              <span className="text-xs text-slate-400">ZIP</span>
              <input className={inputCls} value={zip} onChange={(e) => setZip(e.target.value)} autoComplete="postal-code" inputMode="numeric" />
            </label>
          </div>
        </fieldset>

        {error && (
          <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>
        )}

        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={busy || !name.trim()}>
            {busy ? 'Saving…' : initial ? 'Save' : 'Add site'}
          </button>
        </div>
      </form>
    </div>
  )
}
