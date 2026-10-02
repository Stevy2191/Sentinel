import { useState } from 'react'
import { X } from 'lucide-react'
import { useSites } from '@/hooks/useSites'
import { useDashboardActions } from '@/hooks/useDashboards'
import type { ApiError } from '@/services/api'
import type { DashboardDetail } from '@/types/dashboards'
import { inputCls } from '@/utils/dashboards'

interface Props {
  /** Preselects a site dashboard for this site (from the site's page). */
  siteId?: string | null
  onClose: () => void
  onCreated: (d: DashboardDetail) => void
}

export default function NewDashboardModal({ siteId = null, onClose, onCreated }: Props) {
  const { sites } = useSites()
  const actions = useDashboardActions()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [kind, setKind] = useState<'personal' | 'site'>(siteId ? 'site' : 'personal')
  const [site, setSite] = useState(siteId ?? '')
  const [starter, setStarter] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const forSite = kind === 'site' && site !== ''
      const d = await actions.create({ name, description, site_id: forSite ? site : null, starter: forSite && starter })
      onCreated(d)
    } catch (err) {
      setError((err as ApiError).message || 'Could not create the dashboard')
      setBusy(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form className="card w-full max-w-md space-y-4 p-6" onClick={(e) => e.stopPropagation()} onSubmit={(e) => void submit(e)}>
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">New dashboard</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Name</span>
          <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} maxLength={100} required autoFocus />
        </label>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Description</span>
          <textarea className={inputCls} rows={2} value={description} onChange={(e) => setDescription(e.target.value)} maxLength={500} />
        </label>
        <fieldset className="space-y-2">
          <legend className="text-sm text-slate-300">Belongs to</legend>
          <label className="flex items-center gap-2 text-sm">
            <input type="radio" checked={kind === 'personal'} onChange={() => setKind('personal')} /> Me — share it with people later
          </label>
          <label className="flex items-center gap-2 text-sm">
            <input type="radio" checked={kind === 'site'} onChange={() => setKind('site')} /> A site — everyone with access to the site sees it
          </label>
          {kind === 'site' && (
            <>
              <select className={inputCls} value={site} onChange={(e) => setSite(e.target.value)} required>
                <option value="">Choose a site…</option>
                {sites.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </select>
              <label className="flex items-center gap-2 text-sm text-slate-300">
                <input type="checkbox" checked={starter} onChange={(e) => setStarter(e.target.checked)} />
                Start with the standard widgets (traffic, power, incidents, busiest ports, devices)
              </label>
            </>
          )}
        </fieldset>
        {error && <p className="text-sm text-red-400">{error}</p>}
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={busy || name.trim() === ''}>
            {busy ? 'Creating…' : 'Create'}
          </button>
        </div>
      </form>
    </div>
  )
}
