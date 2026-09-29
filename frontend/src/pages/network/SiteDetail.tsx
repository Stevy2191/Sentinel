import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { MapPin, Pencil, Trash2 } from 'lucide-react'
import { useSite, useSiteActions } from '@/hooks/useSites'
import SiteFormModal from '@/components/SiteFormModal'
import SiteSharingPanel from '@/components/SiteSharingPanel'
import type { ApiError } from '@/services/api'

export default function SiteDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { site, loading, notFound, refetch } = useSite(id)
  const { remove, busy } = useSiteActions()
  const [editing, setEditing] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (loading) return <p className="text-sm text-slate-400">Loading…</p>

  // Missing and not-shared look the same on purpose; the API does not say which.
  if (notFound || !site) {
    return (
      <div className="card p-8 text-center">
        <p className="text-slate-300">Site not found.</p>
        <Link to="/network/sites" className="mt-2 inline-block text-sm text-primary-400 hover:underline">
          Back to sites
        </Link>
      </div>
    )
  }

  const isAdmin = site.access === 'admin'

  const handleDelete = async () => {
    setError(null)
    try {
      await remove(site.id)
      navigate('/network/sites')
    } catch (err) {
      setError((err as ApiError).message || 'Failed to delete site')
      setConfirmDelete(false)
    }
  }

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <Link to="/network/sites" className="text-sm text-slate-400 hover:text-slate-300">
            ← Sites
          </Link>
          <h1 className="mt-2 break-words text-4xl font-light text-white">{site.name}</h1>
          {site.description && <p className="mt-2 text-slate-400">{site.description}</p>}
          {site.address && (
            <p className="mt-2 flex items-center gap-1.5 text-sm text-slate-500">
              <MapPin className="h-4 w-4 shrink-0" /> {site.address}
            </p>
          )}
        </div>
        {isAdmin && (
          <div className="flex gap-2">
            <button className="btn-secondary flex items-center gap-2" onClick={() => setEditing(true)}>
              <Pencil className="h-4 w-4" /> Edit
            </button>
            {confirmDelete ? (
              <>
                <button className="btn-secondary" onClick={() => setConfirmDelete(false)}>
                  Cancel
                </button>
                <button className="btn bg-red-600 text-white hover:bg-red-700" disabled={busy} onClick={() => void handleDelete()}>
                  Delete site
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

      <div className="card p-8 text-center">
        <p className="text-slate-300">No devices yet.</p>
        <p className="mt-1 text-sm text-slate-500">SNMP devices arrive in a later update.</p>
      </div>

      {isAdmin && <SiteSharingPanel siteId={site.id} />}

      {editing && (
        <SiteFormModal
          initial={site}
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false)
            void refetch()
          }}
        />
      )}
    </div>
  )
}
