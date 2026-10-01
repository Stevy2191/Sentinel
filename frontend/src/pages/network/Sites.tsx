import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { MapPin, Plus } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { siteAddressLines, useSites } from '@/hooks/useSites'
import SiteFormModal from '@/components/SiteFormModal'

export default function Sites() {
  const navigate = useNavigate()
  const { currentUser } = useAuthContext()
  const isAdmin = !!currentUser?.is_admin
  const { sites, loading, error, refetch } = useSites()
  const [adding, setAdding] = useState(false)

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-4xl font-light text-white">Sites</h1>
          <p className="mt-2 text-sm text-slate-400">
            Each site groups the network devices, maps and dashboards for one place.
          </p>
        </div>
        {isAdmin && (
          <button className="btn-primary flex items-center gap-2" onClick={() => setAdding(true)}>
            <Plus className="h-4 w-4" /> Add site
          </button>
        )}
      </div>

      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

      {loading ? (
        <p className="text-sm text-slate-400">Loading sites…</p>
      ) : sites.length === 0 ? (
        <div className="card p-8 text-center">
          <p className="text-slate-300">No sites yet.</p>
          <p className="mt-1 text-sm text-slate-500">
            {isAdmin ? 'Add a site to start organising network monitoring.' : 'No sites have been shared with you.'}
          </p>
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {sites.map((s) => (
            <button
              key={s.id}
              className="card p-6 text-left transition hover:border-white/20"
              onClick={() => navigate(`/network/sites/${s.id}`)}
            >
              <div className="truncate text-lg font-medium text-white" title={s.name}>
                {s.name}
              </div>
              {s.description && <p className="mt-1 line-clamp-2 text-sm text-slate-400">{s.description}</p>}
              {siteAddressLines(s).length > 0 && (
                <p className="mt-3 flex items-center gap-1.5 truncate text-xs text-slate-500">
                  <MapPin className="h-3.5 w-3.5 shrink-0" /> {siteAddressLines(s).join(', ')}
                </p>
              )}
            </button>
          ))}
        </div>
      )}

      {adding && (
        <SiteFormModal
          onClose={() => setAdding(false)}
          onSaved={(site) => {
            setAdding(false)
            void refetch()
            navigate(`/network/sites/${site.id}`)
          }}
        />
      )}
    </div>
  )
}
