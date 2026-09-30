import { useMemo, useState } from 'react'
import { Plus, Router, Search } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { useSites } from '@/hooks/useSites'
import { useDevices, type DeviceStatus } from '@/hooks/useDevices'
import DeviceTable from '@/components/network/DeviceTable'
import DeviceFormModal from '@/components/network/DeviceFormModal'

export default function Devices() {
  const { currentUser } = useAuthContext()
  const { sites } = useSites()
  const [siteId, setSiteId] = useState('')
  const [status, setStatus] = useState<DeviceStatus | ''>('')
  const [search, setSearch] = useState('')
  const [adding, setAdding] = useState(false)
  const { devices, loading, error, refetch } = useDevices({ siteId: siteId || undefined, status: status || undefined })

  const shown = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return devices
    return devices.filter((d) => [d.name, d.host, d.vendor, d.model, d.site_name].some((v) => v?.toLowerCase().includes(q)))
  }, [devices, search])

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

      <div className="flex flex-wrap items-center gap-2.5">
        <div className="relative min-w-[220px] flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search name, address, vendor…"
            className="w-full rounded-lg border border-white/10 bg-slate-900/60 py-2 pl-9 pr-3 text-sm text-white placeholder-slate-500"
          />
        </div>
        <select className="rd-select" value={siteId} onChange={(e) => setSiteId(e.target.value)} aria-label="Filter by site">
          <option value="">All sites</option>
          {sites.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
        <select className="rd-select" value={status} onChange={(e) => setStatus(e.target.value as DeviceStatus | '')} aria-label="Filter by status">
          <option value="">All statuses</option>
          <option value="down">Down</option>
          <option value="up">Up</option>
          <option value="error">Error</option>
          <option value="pending">Pending</option>
          <option value="paused">Paused</option>
        </select>
      </div>

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
