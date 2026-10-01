import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { MapPin, Pencil, Plus, Radar, Trash2 } from 'lucide-react'
import { useSite, useSiteActions } from '@/hooks/useSites'
import { useDevices } from '@/hooks/useDevices'
import SiteFormModal from '@/components/SiteFormModal'
import SiteSharingPanel from '@/components/SiteSharingPanel'
import DeviceTable from '@/components/network/DeviceTable'
import DeviceFormModal from '@/components/network/DeviceFormModal'
import ScanModal from '@/components/network/ScanModal'
import { useSitePortSummary, usePortEvents, type PortRef } from '@/hooks/usePorts'
import SiteTrafficCharts from '@/components/network/SiteTrafficCharts'
import PortEventList from '@/components/network/PortEventList'
import { CONDITION_LABEL, formatPct, portTitle } from '@/utils/network'
import type { ApiError } from '@/services/api'

function PortRefList({ title, refs, empty, detail }: { title: string; refs: PortRef[]; empty: string; detail: (r: PortRef) => string }) {
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-light text-white">{title}</h2>
      {refs.length === 0 ? (
        <p className="text-sm text-slate-500">{empty}</p>
      ) : (
        <ul className="card divide-y divide-white/10">
          {refs.map((r) => (
            <li key={`${r.device_id}-${r.if_index}`}>
              <Link to={`/network/devices/${r.device_id}/ports/${r.if_index}`} className="flex items-center justify-between gap-3 p-3 text-sm hover:bg-white/5">
                <span className="min-w-0 truncate text-slate-200">
                  {r.device_name} · {portTitle(r)}
                </span>
                <span className="shrink-0 tabular-nums text-slate-400">{detail(r)}</span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

export default function SiteDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { site, loading, notFound, refetch } = useSite(id)
  const { devices, refetch: refetchDevices } = useDevices({ siteId: id })
  const { remove, busy } = useSiteActions()
  const { data: summary } = useSitePortSummary(id)
  const { events } = usePortEvents({ siteId: id }, 15)
  const [editing, setEditing] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [addingDevice, setAddingDevice] = useState(false)
  const [scanning, setScanning] = useState(false)

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
  const canEdit = isAdmin || site.access === 'editable'

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

      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-lg font-light text-white">Devices ({devices.length})</h2>
          {canEdit && (
            <div className="flex gap-2">
              <button className="btn-secondary flex items-center gap-2" onClick={() => setScanning(true)}>
                <Radar className="h-4 w-4" /> Scan subnet
              </button>
              <button className="btn-primary flex items-center gap-2" onClick={() => setAddingDevice(true)}>
                <Plus className="h-4 w-4" /> Add device
              </button>
            </div>
          )}
        </div>
        {devices.length === 0 ? (
          <div className="card p-8 text-center">
            <p className="text-slate-300">No devices yet.</p>
            {canEdit && <p className="mt-1 text-sm text-slate-500">Add a device by address, or scan a subnet to find them.</p>}
          </div>
        ) : (
          <DeviceTable devices={devices} showSite={false} />
        )}
      </section>

      {devices.length > 0 && (
        <>
          <SiteTrafficCharts siteId={site.id} />
          <div className="grid gap-6 lg:grid-cols-2">
            <PortRefList title="Busiest ports" refs={summary?.busiest ?? []} empty="No traffic figures yet." detail={(r) => formatPct(r.util_pct)} />
            <PortRefList
              title="Ports with problems"
              refs={summary?.problems ?? []}
              empty="Nothing wrong right now."
              detail={(r) => (r.conditions.length ? r.conditions.map((c) => CONDITION_LABEL[c]).join(', ') : 'Link down')}
            />
          </div>
          <section className="space-y-3">
            <h2 className="text-lg font-light text-white">Recent port events</h2>
            <PortEventList events={events} showDevice />
          </section>
        </>
      )}

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
      {addingDevice && (
        <DeviceFormModal siteId={site.id} onClose={() => setAddingDevice(false)} onSaved={() => { setAddingDevice(false); void refetchDevices() }} />
      )}
      {scanning && <ScanModal siteId={site.id} onClose={() => setScanning(false)} onAdded={() => void refetchDevices()} />}
    </div>
  )
}
