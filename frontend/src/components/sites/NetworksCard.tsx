import { useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import NetworkFormModal from '@/components/sites/NetworkFormModal'
import type { SiteNetwork } from '@/hooks/useSiteProfile'
import type { ApiError } from '@/services/api'

interface Props {
  siteId: string
  networks: SiteNetwork[]
  canEdit: boolean
  onDelete: (id: string) => Promise<unknown>
  onChanged: () => void
}

/** A site's subnets: name, then subnet · VLAN · gateway, then its note. */
export default function NetworksCard({ siteId, networks, canEdit, onDelete, onChanged }: Props) {
  const [editing, setEditing] = useState<SiteNetwork | 'new' | null>(null)
  const [confirming, setConfirming] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [deleting, setDeleting] = useState<string | null>(null)

  const remove = async (id: string) => {
    if (deleting) return
    setError(null)
    setDeleting(id)
    try {
      await onDelete(id)
      setConfirming(null)
      onChanged()
    } catch (err) {
      setError((err as ApiError).message || 'Could not delete the network')
    } finally {
      setDeleting(null)
    }
  }

  return (
    <section className="card space-y-3 p-4">
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-lg font-light text-white">Networks</h2>
        {canEdit && (
          <button className="btn-secondary flex items-center gap-1.5 !py-1 text-sm" onClick={() => setEditing('new')}>
            <Plus className="h-3.5 w-3.5" /> Add
          </button>
        )}
      </div>
      {error && <p className="text-sm text-red-400">{error}</p>}
      {networks.length === 0 ? (
        <p className="text-sm text-slate-500">{canEdit ? 'No networks yet.' : 'No networks recorded.'}</p>
      ) : (
        <ul className="divide-y divide-white/10">
          {networks.map((n) => (
            <li key={n.id} className="flex items-start justify-between gap-2 py-2 text-sm">
              <div className="min-w-0">
                <div className="font-medium text-slate-200">{n.name}</div>
                <div className="font-mono text-xs text-slate-400">
                  {n.cidr}
                  {n.vlan != null && ` · VLAN ${n.vlan}`}
                  {n.gateway && ` · gw ${n.gateway}`}
                </div>
                {n.note && <div className="mt-0.5 break-words text-xs text-slate-500">{n.note}</div>}
              </div>
              {canEdit &&
                (confirming === n.id ? (
                  <div className="flex shrink-0 items-center gap-1.5 text-xs">
                    <span className="text-amber-300">Delete?</span>
                    <button className="btn-secondary !px-2 !py-0.5 text-xs" disabled={deleting === n.id} onClick={() => { setConfirming(null); setError(null) }}>
                      Cancel
                    </button>
                    <button className="btn bg-red-600 !px-2 !py-0.5 text-xs text-white hover:bg-red-700" disabled={deleting === n.id} onClick={() => void remove(n.id)}>
                      {deleting === n.id ? 'Deleting…' : 'Delete'}
                    </button>
                  </div>
                ) : (
                  <div className="flex shrink-0 gap-1">
                    <button className="rounded p-1 text-slate-400 hover:bg-white/10 hover:text-white" aria-label={`Edit ${n.name}`} onClick={() => setEditing(n)}>
                      <Pencil className="h-3.5 w-3.5" />
                    </button>
                    <button className="rounded p-1 text-red-400 hover:bg-red-500/10" aria-label={`Delete ${n.name}`} onClick={() => { setError(null); setConfirming(n.id) }}>
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                ))}
            </li>
          ))}
        </ul>
      )}
      {editing && (
        <NetworkFormModal
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
