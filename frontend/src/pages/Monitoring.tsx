import { useCallback, useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { RefreshCw } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { useAllMonitors } from '@/hooks/useAllMonitors'
import { useMonitorGroups } from '@/hooks/useMonitorGroups'
import { useSummaryReport } from '@/hooks/useReports'
import { useUsers } from '@/hooks/useUsers'
import { useAgentActions, useAgents, type Agent, type CreatedAgent } from '@/hooks/useAgents'
import { useDevices, type Device } from '@/hooks/useDevices'
import { useSites } from '@/hooks/useSites'
import { useToasts, Toaster } from '@/components/Toast'
import CreateMonitorModal from '@/components/CreateMonitorModal'
import AddServerAgentModal from '@/components/AddServerAgentModal'
import DeviceFormModal from '@/components/network/DeviceFormModal'
import AddMenu from '@/components/monitoring/AddMenu'
import GroupModal from '@/components/monitoring/GroupModal'
import ListFilterBar from '@/components/monitoring/ListFilterBar'
import MonitoringList from '@/components/monitoring/MonitoringList'
import SummaryStrip from '@/components/monitoring/SummaryStrip'
import UnregisterServerDialog from '@/components/monitoring/UnregisterServerDialog'
import { summaryCounts } from '@/utils/monitoringView'
import {
  NO_FILTERS,
  buildListParams,
  filterRows,
  filtersActive,
  groupRows,
  nextSort,
  parseListParams,
  siteFilterOptions,
  sortRows,
  toRows,
  type GroupBy,
  type ListFilters,
  type SortKey,
} from '@/utils/monitoringList'
import type { ApiError } from '@/services/api'
import type { MonitorGroup } from '@/types'
import { usePageLabel } from '@/utils/navHistory'

const GROUPS_REFRESH_MS = 30_000
const GROUP_BY_KEY = 'sentinel:monitoringGroupBy'

function storedGroupBy(): GroupBy {
  try {
    const v = localStorage.getItem(GROUP_BY_KEY)
    return v === 'site' || v === 'kind' || v === 'group' ? v : 'none'
  } catch {
    return 'none'
  }
}

export default function Monitoring() {
  const { currentUser } = useAuthContext()
  const isAdmin = !!currentUser?.is_admin
  const [searchParams, setSearchParams] = useSearchParams()
  const view = useMemo(() => parseListParams(searchParams), [searchParams])
  const { filters, sort } = view
  // Group by is remembered in this browser; an address that says otherwise
  // (a shared link) wins while it is open.
  const [remembered, setRemembered] = useState<GroupBy>(storedGroupBy)
  const by = view.by ?? remembered

  const setFilters = useCallback(
    (next: ListFilters, replace = false) => setSearchParams(buildListParams({ ...view, filters: next }), { replace }),
    [setSearchParams, view]
  )
  const onSort = (key: SortKey) => setSearchParams(buildListParams({ ...view, sort: nextSort(sort, key) }))
  const onBy = (next: GroupBy) => {
    try {
      localStorage.setItem(GROUP_BY_KEY, next)
    } catch {
      // Private browsing or a full quota: it applies now, just isn't kept.
    }
    setRemembered(next)
    setSearchParams(buildListParams({ ...view, by: next }), { replace: true })
  }

  const { monitors, loading: monitorsLoading, error: monitorsError, loadedAt, refetch: refetchMonitorList } = useAllMonitors()
  const { groups, refetch: refetchGroups } = useMonitorGroups()
  const { agents, loading: agentsLoading, error: agentsError, refetch: refetchAgents } = useAgents()
  const { devices, loading: devicesLoading, error: devicesError, refetch: refetchDevices } = useDevices()
  usePageLabel('Monitoring')
  const { sites } = useSites()
  const { usernameFor } = useUsers()
  const { toasts, push } = useToasts()
  const { getWithToken, remove, busy } = useAgentActions()

  useEffect(() => {
    const t = window.setInterval(() => void refetchGroups(), GROUPS_REFRESH_MS)
    return () => window.clearInterval(t)
  }, [refetchGroups])

  const [refreshing, setRefreshing] = useState(false)
  const refreshAll = async () => {
    setRefreshing(true)
    try {
      await Promise.all([refetchMonitorList(), refetchGroups(), refetchAgents(), refetchDevices()])
    } finally {
      setRefreshing(false)
    }
  }

  const refetchMonitors = useCallback(async () => {
    await Promise.all([refetchMonitorList(), refetchGroups()])
  }, [refetchMonitorList, refetchGroups])

  // 24-hour uptime per monitor: each row's figure until its own loads.
  const windows = useMemo(() => {
    const end = new Date()
    return { day: new Date(end.getTime() - 24 * 3600e3).toISOString(), end: end.toISOString() }
  }, [])
  const { report: daySummary } = useSummaryReport(windows.day, windows.end)
  const uptimeById = useMemo(
    () => new Map((daySummary?.monitors ?? []).map((r) => [r.monitor_id, r.uptime_percent] as const)),
    [daySummary]
  )

  const rows = useMemo(() => toRows(monitors, agents, devices), [monitors, agents, devices])
  const shown = useMemo(() => sortRows(filterRows(rows, filters), sort), [rows, filters, sort])
  const grouped = useMemo(
    () => (by === 'none' ? null : groupRows(shown, by, { groups, includeEmptyGroups: !filtersActive(filters) })),
    [shown, by, groups, filters]
  )
  const counts = useMemo(() => summaryCounts(monitors, agents, devices), [monitors, agents, devices])
  const siteOptions = useMemo(() => siteFilterOptions(sites, rows), [sites, rows])

  const [createOpen, setCreateOpen] = useState(false)
  const [groupModal, setGroupModal] = useState<{ mode: 'create' | 'edit'; group?: MonitorGroup } | null>(null)
  const [agentModal, setAgentModal] = useState<{ existing: CreatedAgent | null } | null>(null)
  const [deviceModal, setDeviceModal] = useState<{ initial?: Device } | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<Agent | null>(null)

  const showInstructions = async (agent: Agent) => {
    try {
      // Re-fetched rather than held in memory: the token is not in the list
      // payload, and the install command is meaningless without it.
      setAgentModal({ existing: await getWithToken(agent.agent_id) })
    } catch (err) {
      push((err as ApiError).message || 'Could not load install instructions', 'error')
    }
  }

  const doDelete = async () => {
    if (!confirmDelete) return
    try {
      await remove(confirmDelete.agent_id)
      push(`${confirmDelete.name} unregistered`, 'success')
      setConfirmDelete(null)
      void refetchAgents()
    } catch (err) {
      push((err as ApiError).message || 'Could not unregister the agent', 'error')
    }
  }

  const failed = [
    { label: 'Uptime checks', error: monitorsError, retry: () => void refetchMonitors() },
    { label: 'Servers', error: agentsError, retry: () => void refetchAgents() },
    { label: 'Devices', error: devicesError, retry: () => void refetchDevices() },
  ].filter((s) => s.error)
  const settled = !monitorsLoading && !agentsLoading && !devicesLoading
  const nothingAtAll = settled && failed.length === 0 && rows.length === 0
  const nothingMatches = settled && rows.length > 0 && shown.length === 0
  // A new device goes to the site being looked at, when that is one site.
  const realSites = filters.sites.filter((s) => s !== 'none')
  const addSite = realSites.length === 1 ? realSites[0] : undefined

  return (
    <div className="space-y-6">
      <Toaster toasts={toasts} />

      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-4xl font-light text-white">Monitoring</h1>
          <p className="mt-2 text-sm text-slate-400">Uptime checks, servers and network devices in one place</p>
        </div>
        <div className="flex items-center gap-2">
          <button className="btn-secondary px-2.5" onClick={() => void refreshAll()} aria-label="Refresh" title="Refresh">
            <RefreshCw className={`h-4 w-4 ${refreshing || monitorsLoading ? 'animate-spin' : ''}`} />
          </button>
          <AddMenu
            isAdmin={isAdmin}
            onMonitor={() => setCreateOpen(true)}
            onGroup={() => setGroupModal({ mode: 'create' })}
            onServer={() => setAgentModal({ existing: null })}
            onDevice={() => setDeviceModal({})}
          />
        </div>
      </div>

      <ListFilterBar rows={rows} filters={filters} onChange={setFilters} sites={siteOptions} groups={groups} by={by} onBy={onBy} />

      <SummaryStrip
        counts={counts}
        statuses={filters.statuses}
        onPick={(status) => setFilters({ ...filters, statuses: status ? [status] : [] })}
      />

      {failed.map((s) => (
        <div key={s.label} className="flex items-center justify-between gap-3 rounded-lg border border-red-500/30 bg-red-500/10 p-3">
          <span className="text-sm text-red-400">
            {s.label} couldn&rsquo;t be loaded <span className="text-red-400/70">— {s.error}</span>
          </span>
          <button className="btn-secondary" onClick={s.retry}>
            Retry
          </button>
        </div>
      ))}

      {!settled ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="h-12 animate-pulse rounded-lg border border-white/10 bg-slate-800/40" />
          ))}
        </div>
      ) : nothingAtAll ? (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-10 text-center text-sm text-slate-300">
          Nothing is being watched yet. Use <span className="font-medium text-white">Add</span> to start with an uptime monitor
          {isAdmin ? ', a server agent or a network device' : ''}.
        </div>
      ) : nothingMatches ? (
        <div className="flex flex-col items-center gap-3 rounded-lg border border-white/10 bg-slate-800/40 p-8 text-center text-sm text-slate-400">
          Nothing matches these filters.
          <button className="btn-secondary" onClick={() => setFilters(NO_FILTERS)}>
            Clear filters
          </button>
        </div>
      ) : (
        rows.length > 0 && (
          <MonitoringList
            rows={shown}
            groups={grouped}
            by={by}
            sort={sort}
            onSort={onSort}
            isAdmin={isAdmin}
            uptimeById={uptimeById}
            usernameFor={usernameFor}
            refreshKey={loadedAt}
            push={push}
            onMonitorsChanged={() => void refetchMonitors()}
            onServerInstructions={(a) => void showInstructions(a)}
            onServerDelete={setConfirmDelete}
            onDeviceEdit={(d) => setDeviceModal({ initial: d })}
            onEditGroup={(group) => setGroupModal({ mode: 'edit', group })}
          />
        )
      )}

      <CreateMonitorModal isOpen={createOpen} onClose={() => setCreateOpen(false)} onCreated={() => void refetchMonitors()} push={push} />
      {groupModal && (
        <GroupModal
          mode={groupModal.mode}
          group={groupModal.group}
          onClose={() => setGroupModal(null)}
          onSaved={() => void refetchMonitors()}
          push={push}
        />
      )}
      <AddServerAgentModal
        isOpen={agentModal !== null}
        existing={agentModal?.existing ?? null}
        onClose={() => setAgentModal(null)}
        onCreated={() => void refetchAgents()}
        push={push}
      />
      {confirmDelete && (
        <UnregisterServerDialog agent={confirmDelete} busy={busy} onCancel={() => setConfirmDelete(null)} onConfirm={() => void doDelete()} />
      )}
      {deviceModal && (
        <DeviceFormModal
          initial={deviceModal.initial}
          siteId={deviceModal.initial ? undefined : addSite}
          onClose={() => setDeviceModal(null)}
          onSaved={() => {
            setDeviceModal(null)
            void refetchDevices()
          }}
        />
      )}
    </div>
  )
}
