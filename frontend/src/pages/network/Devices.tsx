import { useMemo, useState } from 'react'
import { Plus, Router } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { useSites } from '@/hooks/useSites'
import { useDevices } from '@/hooks/useDevices'
import DeviceTable from '@/components/network/DeviceTable'
import DeviceFormModal from '@/components/network/DeviceFormModal'
import DeviceFilterBar from '@/components/network/DeviceFilterBar'
import { filterDevices, NO_DEVICE_FILTERS } from '@/utils/devices'

export default function Devices() {
  const { currentUser } = useAuthContext()
  const { sites } = useSites()
  const [siteId, setSiteId] = useState('')
  const [filters, setFilters] = useState(NO_DEVICE_FILTERS)
  const [adding, setAdding] = useState(false)
  const { devices, loading, error, refetch } = useDevices({ siteId: siteId || undefined })
  const shown = useMemo(() => filterDevices(devices, filters), [devices, filters])

  // Admins can add devices to any site from here. Everyone else adds them
  // from a site's page, which knows their access to that site.
  const canAdd = !!currentUser?.is_admin

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-4xl font-light text-white">Devices</h1>
          <p className="mt-2 text-sm text-slate-400">Every SNMP device in the sites you can see.</p>
        </div>
        {canAdd && (
          <button className="btn-primary flex items-center gap-2" onClick={() => setAdding(true)}>
            <Plus className="h-4 w-4" /> Add device
          </button>
        )}
      </div>

      <DeviceFilterBar filters={filters} onChange={setFilters}>
        <select className="rd-select" value={siteId} onChange={(e) => setSiteId(e.target.value)} aria-label="Filter by site">
          <option value="">All sites</option>
          {sites.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      </DeviceFilterBar>

      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

      {loading ? (
        <p className="text-sm text-slate-400">Loading devices…</p>
      ) : shown.length === 0 ? (
        <div className="card p-8 text-center">
          <Router className="mx-auto h-8 w-8 text-slate-500" />
          <p className="mt-2 text-slate-300">{devices.length === 0 ? 'No devices yet.' : 'No devices match these filters.'}</p>
          {devices.length === 0 && (
            <p className="mt-1 text-sm text-slate-500">
              {canAdd ? "Add one, or scan a subnet from a site's page." : "Add devices from a site's page."}
            </p>
          )}
        </div>
      ) : (
        <DeviceTable devices={shown} showSite />
      )}

      {adding && (
        <DeviceFormModal
          siteId={siteId || undefined}
          onClose={() => setAdding(false)}
          onSaved={() => {
            setAdding(false)
            void refetch()
          }}
        />
      )}
    </div>
  )
}
