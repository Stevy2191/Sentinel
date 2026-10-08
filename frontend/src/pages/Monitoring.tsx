import { useCallback, useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { RefreshCw } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { useAllMonitors } from '@/hooks/useAllMonitors'
import { useMonitorGroups } from '@/hooks/useMonitorGroups'
import { useSummaryReport } from '@/hooks/useReports'
import { useUsers } from '@/hooks/useUsers'
import { useAgents, type CreatedAgent } from '@/hooks/useAgents'
import { useDevices } from '@/hooks/useDevices'
import { useSites } from '@/hooks/useSites'
import { useRememberedToggles } from '@/hooks/useRememberedToggles'
import { useToasts, Toaster } from '@/components/Toast'
import CreateMonitorModal from '@/components/CreateMonitorModal'
import AddServerAgentModal from '@/components/AddServerAgentModal'
import DeviceFormModal from '@/components/network/DeviceFormModal'
import AddMenu from '@/components/monitoring/AddMenu'
import GroupModal from '@/components/monitoring/GroupModal'
import MonitoringToolbar from '@/components/monitoring/MonitoringToolbar'
import SummaryStrip from '@/components/monitoring/SummaryStrip'
import UptimeSection from '@/components/monitoring/UptimeSection'
import ServersSection from '@/components/monitoring/ServersSection'
import DevicesSection from '@/components/monitoring/DevicesSection'
import {
  NO_FILTERS,
  SECTION_LABEL,
  buildMonitoringParams,
  filterDevicesView,
  filterMonitors,
  filterServers,
  filtersNarrow,
  ownFiltersSet,
  parseMonitoringParams,
  sectionVisible,
  siteFilterOptions,
  statusOptions,
  summaryCounts,
  visibleSections,
  type MonitoringFilters,
  type Section,
} from '@/utils/monitoringView'
import type { MonitorGroup } from '@/types'

const GROUPS_REFRESH_MS = 30_000

export default function Monitoring() {
  const { currentUser } = useAuthContext()
  const isAdmin = !!currentUser?.is_admin
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = useMemo(() => parseMonitoringParams(searchParams), [searchParams])
  const setFilters = useCallback(
    (next: MonitoringFilters, replace = false) => setSearchParams(buildMonitoringParams(next), { replace }),
    [setSearchParams]
  )

  const { monitors, loading: monitorsLoading, error: monitorsError, loadedAt, refetch: refetchMonitorList } = useAllMonitors()
  const { groups, refetch: refetchGroups } = useMonitorGroups()
  const { agents, loading: agentsLoading, error: agentsError, refetch: refetchAgents } = useAgents()
  const { devices, loading: devicesLoading, error: devicesError, refetch: refetchDevices } = useDevices()
  const { sites } = useSites()
  const { usernameFor } = useUsers()
  const { toasts, push } = useToasts()
  const sectionToggles = useRememberedToggles('sentinel:monitoringCollapsed')

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

  // 24-hour uptime per monitor (each row's figure until its own loads, and
  // the "lowest uptime" sort) and 30-day incidents, fixed at mount.
  const windows = useMemo(() => {
    const end = new Date()
    return {
      day: new Date(end.getTime() - 24 * 3600e3).toISOString(),
      month: new Date(end.getTime() - 30 * 24 * 3600e3).toISOString(),
      end: end.toISOString(),
    }
  }, [])
  const { report: daySummary } = useSummaryReport(windows.day, windows.end)
  const { report: monthSummary } = useSummaryReport(windows.month, windows.end)
  const uptimeById = useMemo(
    () => new Map((daySummary?.monitors ?? []).map((r) => [r.monitor_id, r.uptime_percent] as const)),
    [daySummary]
  )

  const shownMonitors = useMemo(() => filterMonitors(monitors, filters), [monitors, filters])
  const shownAgents = useMemo(() => filterServers(agents, filters), [agents, filters])
  const shownDevices = useMemo(() => filterDevicesView(devices, filters), [devices, filters])
  const counts = useMemo(() => summaryCounts(monitors, agents, devices), [monitors, agents, devices])
  const siteOptions = useMemo(() => siteFilterOptions(sites, [...monitors, ...agents]), [sites, monitors, agents])
  const narrowed = filtersNarrow(filters)
  // Keeps the chosen section, so the user stays where they were.
  const clearFilters = () => setFilters({ ...NO_FILTERS, show: filters.show })

  const [createOpen, setCreateOpen] = useState(false)
  const [groupModal, setGroupModal] = useState<{ mode: 'create' | 'edit'; group?: MonitorGroup } | null>(null)
  const [agentModal, setAgentModal] = useState<{ existing: CreatedAgent | null } | null>(null)
  const [addingDevice, setAddingDevice] = useState(false)

  const state: Record<Section, { loading: boolean; error: string | null; total: number; shown: number; canAdd: boolean }> = {
    uptime: { loading: monitorsLoading, error: monitorsError, total: monitors.length, shown: shownMonitors.length, canAdd: true },
    servers: { loading: agentsLoading, error: agentsError, total: agents.length, shown: shownAgents.length, canAdd: isAdmin },
    devices: { loading: devicesLoading, error: devicesError, total: devices.length, shown: shownDevices.length, canAdd: isAdmin },
  }
  const order = visibleSections(filters).filter((s) => sectionVisible({ ...state[s], narrowed, ownFilter: ownFiltersSet(s, filters) }))
  const missing = (Object.keys(state) as Section[]).filter((s) => state[s].error).map((s) => SECTION_LABEL[s])
  const allLoaded = !monitorsLoading && !agentsLoading && !devicesLoading
  const nothingAtAll = allLoaded && missing.length === 0 && monitors.length + agents.length + devices.length === 0
  const nothingMatches = allLoaded && !nothingAtAll && order.length === 0

  return (
    <div className="space-y-6">
      <Toaster toasts={toasts} />

      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-4xl font-light text-white">Monitoring</h1>
          <p className="mt-2 text-sm text-slate-400">Uptime checks, servers and network devices in one place</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            className="btn-secondary px-2.5"
            onClick={() => void refreshAll()}
            aria-label="Refresh"
            title="Refresh"
          >
            <RefreshCw className={`h-4 w-4 ${refreshing || monitorsLoading ? 'animate-spin' : ''}`} />
          </button>
          <AddMenu
            isAdmin={isAdmin}
            onMonitor={() => setCreateOpen(true)}
            onGroup={() => setGroupModal({ mode: 'create' })}
            onServer={() => setAgentModal({ existing: null })}
            onDevice={() => setAddingDevice(true)}
          />
        </div>
      </div>

      <MonitoringToolbar
        filters={filters}
        onChange={setFilters}
        counts={{ uptime: monitors.length, servers: agents.length, devices: devices.length }}
        statuses={statusOptions(monitors, devices, filters.status)}
        sites={siteOptions}
        groups={groups}
      />

      <SummaryStrip counts={counts} missing={missing} onPick={(status) => setFilters({ ...NO_FILTERS, status })} />

      {nothingAtAll ? (
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
        <div className="space-y-8">
          {order.map((s) =>
            s === 'uptime' ? (
              <UptimeSection
                key="uptime"
                monitors={shownMonitors}
                all={monitors}
                groups={groups}
                filters={filters}
                onFilters={setFilters}
                onClearFilters={clearFilters}
                narrowed={narrowed}
                uptimeById={uptimeById}
                incidents30d={monthSummary?.aggregate.total_incidents ?? null}
                usernameFor={usernameFor}
                refreshKey={loadedAt}
                collapsed={!!sectionToggles.on.uptime}
                onToggle={() => sectionToggles.toggle('uptime')}
                loading={monitorsLoading}
                error={monitorsError}
                onRetry={() => void refetchMonitors()}
                onChanged={() => void refetchMonitors()}
                onAdd={() => setCreateOpen(true)}
                onEditGroup={(group) => setGroupModal({ mode: 'edit', group })}
                push={push}
              />
            ) : s === 'servers' ? (
              <ServersSection
                key="servers"
                agents={shownAgents}
                total={agents.length}
                isAdmin={isAdmin}
                collapsed={!!sectionToggles.on.servers}
                onToggle={() => sectionToggles.toggle('servers')}
                loading={agentsLoading}
                error={agentsError}
                onRetry={() => void refetchAgents()}
                onChanged={() => void refetchAgents()}
                onAdd={() => setAgentModal({ existing: null })}
                onInstructions={(existing) => setAgentModal({ existing })}
                push={push}
              />
            ) : (
              <DevicesSection
                key="devices"
                devices={shownDevices}
                total={devices.length}
                isAdmin={isAdmin}
                filters={filters}
                onFilters={setFilters}
                onClearFilters={clearFilters}
                collapsed={!!sectionToggles.on.devices}
                onToggle={() => sectionToggles.toggle('devices')}
                loading={devicesLoading}
                error={devicesError}
                onRetry={() => void refetchDevices()}
                onAdd={() => setAddingDevice(true)}
              />
            )
          )}
        </div>
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
      {addingDevice && (
        <DeviceFormModal
          siteId={filters.site && filters.site !== 'none' ? filters.site : undefined}
          onClose={() => setAddingDevice(false)}
          onSaved={() => {
            setAddingDevice(false)
            void refetchDevices()
          }}
        />
      )}
    </div>
  )
}
