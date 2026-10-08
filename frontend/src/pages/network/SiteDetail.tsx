import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { LayoutDashboard, MapPin, Pencil, Plus, Radar, Trash2 } from 'lucide-react'
import { siteAddressLines, useSite, useSiteActions } from '@/hooks/useSites'
import { useDevices } from '@/hooks/useDevices'
import { useDashboards } from '@/hooks/useDashboards'
import { useSiteProfile, useSiteProfileActions } from '@/hooks/useSiteProfile'
import { useAllMonitors } from '@/hooks/useAllMonitors'
import { useAgents } from '@/hooks/useAgents'
import SiteServersList from '@/components/sites/SiteServersList'
import SiteChecksList from '@/components/sites/SiteChecksList'
import { monitorState, serverState, summaryCounts } from '@/utils/monitoringView'
import { siteTab, type SiteTab } from '@/utils/siteProfile'
import SiteFormModal from '@/components/SiteFormModal'
import SiteSharingPanel from '@/components/SiteSharingPanel'
import NewDashboardModal from '@/components/dashboards/NewDashboardModal'
import DeviceTable from '@/components/network/DeviceTable'
import DeviceFilterBar from '@/components/network/DeviceFilterBar'
import { filterDevices, NO_DEVICE_FILTERS } from '@/utils/devices'
import DeviceFormModal from '@/components/network/DeviceFormModal'
import ScanModal from '@/components/network/ScanModal'
import CircuitsCard from '@/components/sites/CircuitsCard'
import NetworksCard from '@/components/sites/NetworksCard'
import NotesCard from '@/components/sites/NotesCard'
import { useSitePortSummary, usePortEvents, type PortRef, type SitePortSummary } from '@/hooks/usePorts'
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

// The Traffic & ports tab. Port events load only while it is open.
function TrafficPanel({ siteId, hasDevices, summary }: { siteId: string; hasDevices: boolean; summary: SitePortSummary | null }) {
  const { events } = usePortEvents({ siteId }, 15)
  if (!hasDevices) return <p className="text-sm text-slate-500">No devices at this site yet, so there is no traffic to show.</p>
  return (
    <div className="space-y-8">
      <SiteTrafficCharts siteId={siteId} />
      <div className="grid gap-6 xl:grid-cols-2">
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
    </div>
  )
}

export default function SiteDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { site, loading, notFound, refetch } = useSite(id)
  const { devices, refetch: refetchDevices } = useDevices({ siteId: id })
  const { remove, busy } = useSiteActions()
  const { data: summary } = useSitePortSummary(id)
  const { profile, error: profileError, refetch: refetchProfile } = useSiteProfile(id)
  const profileActions = useSiteProfileActions(id ?? '')
  const { monitors, error: monitorsError } = useAllMonitors()
  const { agents, error: agentsError } = useAgents()
  const siteMonitors = useMemo(() => monitors.filter((m) => m.site_id === id), [monitors, id])
  const siteAgents = useMemo(() => agents.filter((a) => a.site_id === id), [agents, id])
  const counts = useMemo(() => summaryCounts(siteMonitors, siteAgents, devices), [siteMonitors, siteAgents, devices])
  // The right column's tab, kept in the address so a reload or a link opens
  // it; switching replaces the entry so Back leaves the page, not the tab.
  const [searchParams, setSearchParams] = useSearchParams()
  const tab = siteTab(searchParams.get('tab'))
  const chooseTab = (t: SiteTab) =>
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (t === 'devices') next.delete('tab')
        else next.set('tab', t)
        return next
      },
      { replace: true }
    )
  const [editing, setEditing] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [addingDevice, setAddingDevice] = useState(false)
  const [deviceFilters, setDeviceFilters] = useState(NO_DEVICE_FILTERS)
  const shownDevices = useMemo(() => filterDevices(devices, deviceFilters), [devices, deviceFilters])
  const [scanning, setScanning] = useState(false)
  const { dashboards } = useDashboards(id)
  const [newDashboard, setNewDashboard] = useState(false)

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
  const address = siteAddressLines(site)
  const changed = () => refetchProfile()
  const problems = summary?.problems.length ?? 0
  const tabs: { key: SiteTab; label: string; count: number | null; down: number; problems: number }[] = [
    { key: 'devices', label: 'Devices', count: devices.length, down: devices.filter((d) => d.status === 'down').length, problems: 0 },
    { key: 'servers', label: 'Servers', count: siteAgents.length, down: siteAgents.filter((a) => serverState(a) === 'down').length, problems: 0 },
    { key: 'checks', label: 'Uptime checks', count: siteMonitors.length, down: siteMonitors.filter((m) => monitorState(m) === 'down').length, problems: 0 },
    { key: 'traffic', label: 'Traffic & ports', count: null, down: 0, problems },
  ]

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
          {address.length > 0 && (
            <address className="mt-2 flex items-start gap-1.5 text-sm not-italic text-slate-500">
              <MapPin className="mt-0.5 h-4 w-4 shrink-0" />
              <span>
                {address.map((line) => (
                  <span key={line} className="block">
                    {line}
                  </span>
                ))}
              </span>
            </address>
          )}
          <p className="mt-3 text-sm text-slate-400">
            <span className="font-semibold tabular-nums text-white">{counts.watching}</span> watched ·{' '}
            <span className={`font-semibold tabular-nums ${counts.down > 0 ? 'text-red-400' : 'text-white'}`}>{counts.down}</span> down ·{' '}
            <Link to={`/monitoring?site=${site.id}`} className="text-primary-400 hover:underline">
              Open in Monitoring
            </Link>
            {(monitorsError || agentsError) && <span className="text-amber-300"> · some lists could not load</span>}
          </p>
          <div className="mt-2 flex flex-wrap items-center gap-2 text-sm">
            <span className="text-slate-500">Dashboards:</span>
            {dashboards.length === 0 && <span className="text-slate-500">none yet</span>}
            {dashboards.map((d) => (
              <Link
                key={d.id}
                to={`/dashboards/${d.id}`}
                className="inline-flex items-center gap-1.5 rounded-md border border-white/10 px-2 py-0.5 text-slate-200 hover:bg-white/5"
              >
                <LayoutDashboard className="h-3.5 w-3.5 text-teal-400" /> {d.name}
                {d.published && <span className="text-xs text-teal-300">public</span>}
              </Link>
            ))}
            {canEdit && (
              <button className="inline-flex items-center gap-1 text-primary-400 hover:underline" onClick={() => setNewDashboard(true)}>
                <Plus className="h-3.5 w-3.5" /> New site dashboard
              </button>
            )}
          </div>
        </div>
        {isAdmin && (
          <div className="flex gap-2">
            <button className="btn-secondary flex items-center gap-2" onClick={() => setEditing(true)}>
              <Pencil className="h-4 w-4" /> Edit
            </button>
            {confirmDelete ? (
              <>
                <span className="self-center text-sm text-amber-300">
                  Its networks, circuits and notes
                  {dashboards.length > 0 &&
                    `, and its ${dashboards.length} dashboard${dashboards.length === 1 ? '' : 's'}${
                      dashboards.some((d) => d.published) ? ` (${dashboards.filter((d) => d.published).length} with a public link)` : ''
                    }`}{' '}
                  will be deleted too.
                </span>
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

      <div className="grid gap-8 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
        {/* The site's facts. */}
        <div className="min-w-0 space-y-6">
          {profileError && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{profileError}</div>}
          {profile ? (
            <>
              <CircuitsCard siteId={site.id} circuits={profile.circuits} canEdit={canEdit} onDelete={profileActions.deleteCircuit} onChanged={changed} />
              <NetworksCard siteId={site.id} networks={profile.networks} canEdit={canEdit} onDelete={profileActions.deleteNetwork} onChanged={changed} />
              <NotesCard
                notes={profile.notes}
                canEdit={canEdit}
                busy={profileActions.busy}
                onSave={async (notes) => {
                  await profileActions.saveNotes(notes)
                  await changed()
                }}
              />
            </>
          ) : (
            !profileError && <p className="text-sm text-slate-400">Loading the site profile…</p>
          )}
          {isAdmin && <SiteSharingPanel siteId={site.id} />}
        </div>

        {/* What is there, one tab at a time. */}
        <div className="min-w-0 space-y-5">
          <div className="-mx-1 flex gap-1 overflow-x-auto border-b border-white/10 px-1" role="tablist" aria-label="What is at this site">
            {tabs.map((t) => (
              <button
                key={t.key}
                role="tab"
                aria-selected={tab === t.key}
                onClick={() => chooseTab(t.key)}
                className={`-mb-px flex shrink-0 items-center gap-2 whitespace-nowrap border-b-2 px-3 py-2 text-sm transition ${
                  tab === t.key ? 'border-primary-500 text-white' : 'border-transparent text-slate-400 hover:text-slate-200'
                }`}
              >
                {t.label}
                {t.count != null && <span className="tabular-nums text-slate-500">{t.count}</span>}
                {t.down > 0 && <span className="rounded-full bg-red-500/10 px-1.5 text-xs font-medium text-red-400">{t.down} down</span>}
                {t.problems > 0 && (
                  <span className="rounded-full bg-amber-500/10 px-1.5 text-xs font-medium text-amber-300">
                    {t.problems} problem{t.problems === 1 ? '' : 's'}
                  </span>
                )}
              </button>
            ))}
          </div>

          {tab === 'devices' && (
            <section className="space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <h2 className="text-lg font-light text-white">
                  Devices ({shownDevices.length === devices.length ? devices.length : `${shownDevices.length} of ${devices.length}`})
                </h2>
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
                <>
                  <DeviceFilterBar filters={deviceFilters} onChange={setDeviceFilters} />
                  {shownDevices.length === 0 ? (
                    <div className="card p-6 text-center text-sm text-slate-400">
                      No devices match these filters.{' '}
                      <button type="button" className="text-primary-400 hover:underline" onClick={() => setDeviceFilters(NO_DEVICE_FILTERS)}>
                        Clear filters
                      </button>
                    </div>
                  ) : (
                    <DeviceTable devices={shownDevices} showSite={false} />
                  )}
                </>
              )}
            </section>
          )}
          {tab === 'servers' && <SiteServersList agents={siteAgents} canHint={isAdmin} error={agentsError} />}
          {tab === 'checks' && <SiteChecksList monitors={siteMonitors} error={monitorsError} />}
          {tab === 'traffic' && <TrafficPanel siteId={site.id} hasDevices={devices.length > 0} summary={summary ?? null} />}
        </div>
      </div>

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
      {scanning && (
        <ScanModal siteId={site.id} networks={profile?.networks ?? []} onClose={() => setScanning(false)} onAdded={() => void refetchDevices()} />
      )}
      {newDashboard && (
        <NewDashboardModal siteId={site.id} onClose={() => setNewDashboard(false)} onCreated={(d) => navigate(`/dashboards/${d.id}/edit`)} />
      )}
    </div>
  )
}
