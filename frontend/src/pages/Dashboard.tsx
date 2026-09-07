import { useAuthContext } from '@/context/AuthContext'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  RefreshCw,
  Plus,
  FolderPlus,
  Search,
  Trash2,
  SlidersHorizontal,
  X,
  ChevronDown,
  Wand2,
  Upload,
  Globe,
  Radar,
} from 'lucide-react'
import { useMonitors } from '@/hooks/useMonitors'
import {
  useMonitorGroups,
  useCreateMonitorGroup,
  useUpdateMonitorGroup,
  useDeleteMonitorGroup,
} from '@/hooks/useMonitorGroups'
import { useSummaryReport } from '@/hooks/useReports'
import { useUsers } from '@/hooks/useUsers'
import { useToasts, Toaster } from '@/components/Toast'
import ColorPicker from '@/components/ColorPicker'
import GroupSection from '@/components/GroupSection'
import MonitorCard from '@/components/MonitorCard'
import StatusSidebar, { REPORT_PERIODS, type ReportPeriod } from '@/components/StatusSidebar'
import type { Monitor, MonitorGroup } from '@/types'
import { useCardShimmer } from '@/hooks/useCardShimmer'
import ShimmerStatCard from '@/components/ShimmerStatCard'
import ShimmerTypeCard from '@/components/ShimmerTypeCard'

const REFRESH_MS = 30_000
const DEFAULT_GROUP_COLOR = '#37F98A'

// useDebounced returns a value that only updates after `ms` of no changes.
function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(value), ms)
    return () => window.clearTimeout(t)
  }, [value, ms])
  return debounced
}

/** Close a popover when the pointer goes down anywhere outside it. */
function useDismissOnOutsideClick(open: boolean, close: () => void) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) close()
    }
    const onEsc = (e: KeyboardEvent) => e.key === 'Escape' && close()
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onEsc)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onEsc)
    }
  }, [open, close])
  return ref
}

const STATUS_OPTIONS = ['all', 'online', 'offline', 'maintenance', 'paused', 'unknown'] as const
type StatusFilter = (typeof STATUS_OPTIONS)[number]

const SORTS = [
  { key: 'down-first', label: 'Down first' },
  { key: 'name', label: 'Name (A–Z)' },
  { key: 'uptime', label: 'Lowest uptime' },
  { key: 'slowest', label: 'Slowest first' },
] as const
type SortKey = (typeof SORTS)[number]['key']

// Sort weight for "Down first": the states that need attention float up, and
// paused monitors sink below healthy ones — dormant is not urgent. Checked
// before status so a monitor paused while offline still sorts as paused.
function urgency(m: Monitor): number {
  if (!m.enabled) return 4
  if (m.current_status === 'offline') return 0
  if (m.is_in_maintenance) return 1
  if (m.current_status === 'online') return 3
  return 2 // unknown / not yet checked
}

const selectCls = 'rd-select'

// ---------- "New" split button ----------
function NewMenu({
  onSingle,
  onWizard,
  onBulk,
  onDiscover,
  onGroup,
  isAdmin,
}: {
  onSingle: () => void
  onWizard: () => void
  onBulk: () => void
  onDiscover: () => void
  onGroup: () => void
  isAdmin: boolean
}) {
  const [open, setOpen] = useState(false)
  const ref = useDismissOnOutsideClick(open, useCallback(() => setOpen(false), []))

  const items = [
    { key: 'single', label: 'Single monitor', icon: Globe, onClick: onSingle },
    { key: 'wizard', label: 'Monitor wizard', icon: Wand2, onClick: onWizard },
    { key: 'bulk', label: 'Bulk upload', icon: Upload, onClick: onBulk },
    ...(isAdmin ? [{ key: 'discover', label: 'Network discovery', icon: Radar, onClick: onDiscover }] : []),
    { key: 'group', label: 'Group', icon: FolderPlus, onClick: onGroup },
  ]

  return (
    <div ref={ref} className="relative shrink-0">
      <button
        className="rd-btn rd-btn-primary"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <Plus className="h-4 w-4" /> New
        <ChevronDown className={`h-3.5 w-3.5 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
      {open && (
        <div
          role="menu"
          className="absolute right-0 z-40 mt-2 w-52 overflow-hidden rounded-lg py-1 shadow-xl"
          style={{ backgroundColor: 'var(--vs-panel)', border: '1px solid var(--vs-line)' }}
        >
          {items.map((item) => (
            <button
              key={item.key}
              role="menuitem"
              onClick={() => {
                setOpen(false)
                item.onClick()
              }}
              className="flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm transition-colors hover:bg-white/5"
              style={{ color: 'var(--vs-text)' }}
            >
              <item.icon className="h-4 w-4 shrink-0" style={{ color: 'var(--vs-cyan)' }} />
              <span className="flex-1">{item.label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// ---------- filter popover ----------
interface FilterMenuProps {
  activeCount: number
  allTypes: string[]
  allTags: string[]
  groups: MonitorGroup[]
  typeFilter: string
  statusFilter: StatusFilter
  groupFilter: string
  selectedTags: string[]
  setTypeFilter: (v: string) => void
  setStatusFilter: (v: StatusFilter) => void
  setGroupFilter: (v: string) => void
  toggleTag: (t: string) => void
  clearAll: () => void
}

function FilterMenu(props: FilterMenuProps) {
  const [open, setOpen] = useState(false)
  const ref = useDismissOnOutsideClick(open, useCallback(() => setOpen(false), []))
  const { activeCount } = props

  return (
    <div ref={ref} className="relative shrink-0">
      <button
        className="rd-btn rd-btn-secondary"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        style={activeCount > 0 ? { color: 'var(--vs-cyan)', borderColor: 'var(--vs-cyan)' } : undefined}
      >
        <SlidersHorizontal className="h-4 w-4" />
        Filter{activeCount > 0 && ` (${activeCount})`}
      </button>
      {open && (
        <div
          className="absolute left-0 z-40 mt-2 w-72 space-y-3 rounded-lg p-3 shadow-xl"
          style={{ backgroundColor: 'var(--vs-panel)', border: '1px solid var(--vs-line)' }}
        >
          <label className="block">
            <span className="vs-eyebrow mb-1 block">Type</span>
            <select className={`${selectCls} w-full`} value={props.typeFilter} onChange={(e) => props.setTypeFilter(e.target.value)}>
              <option value="all">All</option>
              {props.allTypes.map((t) => (
                <option key={t} value={t}>
                  {t.toUpperCase()}
                </option>
              ))}
            </select>
          </label>
          <label className="block">
            <span className="vs-eyebrow mb-1 block">Status</span>
            <select
              className={`${selectCls} w-full`}
              value={props.statusFilter}
              onChange={(e) => props.setStatusFilter(e.target.value as StatusFilter)}
            >
              <option value="all">All</option>
              <option value="online">Online</option>
              <option value="offline">Offline</option>
              <option value="maintenance">Maintenance</option>
              <option value="paused">Paused</option>
              <option value="unknown">Unknown</option>
            </select>
          </label>
          <label className="block">
            <span className="vs-eyebrow mb-1 block">Group</span>
            <select className={`${selectCls} w-full`} value={props.groupFilter} onChange={(e) => props.setGroupFilter(e.target.value)}>
              <option value="all">All</option>
              <option value="ungrouped">Ungrouped</option>
              {props.groups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name}
                </option>
              ))}
            </select>
          </label>
          {props.allTags.length > 0 && (
            <div>
              <span className="vs-eyebrow mb-1.5 block">Tags</span>
              <div className="flex flex-wrap gap-1.5">
                {props.allTags.map((t) => {
                  const on = props.selectedTags.includes(t)
                  return (
                    <button
                      key={t}
                      onClick={() => props.toggleTag(t)}
                      className={`rounded-full px-2.5 py-1 text-xs font-medium transition ${
                        on ? 'bg-primary-600 text-white' : 'bg-neutral-800 text-neutral-300 hover:bg-neutral-700'
                      }`}
                    >
                      {t}
                    </button>
                  )
                })}
              </div>
            </div>
          )}
          {activeCount > 0 && (
            <button
              onClick={props.clearAll}
              className="flex w-full items-center justify-center gap-1.5 rounded-md py-1.5 text-xs"
              style={{ color: 'var(--vs-text-dim)', border: '1px solid var(--vs-line)' }}
            >
              <X className="h-3.5 w-3.5" /> Clear all filters
            </button>
          )}
        </div>
      )}
    </div>
  )
}

// ---------- group create/edit modal ----------
function GroupModal({
  mode,
  group,
  onClose,
  onSaved,
  push,
}: {
  mode: 'create' | 'edit'
  group?: MonitorGroup
  onClose: () => void
  onSaved: () => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}) {
  const { create, loading: creating } = useCreateMonitorGroup()
  const { update, loading: updating } = useUpdateMonitorGroup()
  const { delete: remove, loading: deleting } = useDeleteMonitorGroup()
  const [name, setName] = useState(group?.name ?? '')
  const [description, setDescription] = useState(group?.description ?? '')
  const [color, setColor] = useState(group?.color ?? DEFAULT_GROUP_COLOR)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const busy = creating || updating || deleting
  const inputCls =
    'w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-800 focus:outline-none focus:ring-2 focus:ring-primary-500'

  const save = async () => {
    if (!name.trim()) {
      push('Group name is required', 'error')
      return
    }
    const input = { name: name.trim(), description: description.trim() || null, color }
    try {
      if (mode === 'create') {
        await create(input)
        push('Group created', 'success')
      } else if (group) {
        await update(group.id, input)
        push('Group updated', 'success')
      }
      onSaved()
      onClose()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to save group', 'error')
    }
  }
  const del = async () => {
    if (!group) return
    try {
      await remove(group.id)
      push('Group deleted; its monitors were ungrouped', 'success')
      onSaved()
      onClose()
    } catch (err) {
      push((err as { message?: string }).message ?? 'Failed to delete group', 'error')
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="card w-full max-w-md space-y-4 p-6" onClick={(e) => e.stopPropagation()}>
        <h3 className="text-lg font-semibold">{mode === 'create' ? 'Create Group' : 'Edit Group'}</h3>
        <div>
          <span className="mb-1 block text-sm font-medium">
            Name <span className="text-red-500">*</span>
          </span>
          <input autoFocus className={inputCls} value={name} onChange={(e) => setName(e.target.value)} placeholder="Internal Servers" />
        </div>
        <div>
          <span className="mb-1 block text-sm font-medium">Description</span>
          <input className={inputCls} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Optional" />
        </div>
        <ColorPicker label="Color" value={color} defaultValue={DEFAULT_GROUP_COLOR} onChange={setColor} />
        <div className="flex items-center justify-between gap-2 border-t border-neutral-200 pt-4 dark:border-neutral-800">
          {mode === 'edit' ? (
            <button
              className="btn border border-error-300 text-error-600 hover:bg-error-50 dark:hover:bg-error-900/20"
              disabled={busy}
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 className="h-4 w-4" /> Delete
            </button>
          ) : (
            <span />
          )}
          <div className="flex gap-2">
            <button className="btn-secondary" disabled={busy} onClick={onClose}>
              Cancel
            </button>
            <button className="btn-primary" disabled={busy} onClick={() => void save()}>
              {busy ? 'Saving…' : 'Save'}
            </button>
          </div>
        </div>
        {confirmDelete && (
          <div className="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-800 dark:bg-amber-900/30">
            <p className="mb-2 text-amber-800 dark:text-amber-200">Delete this group? Its monitors will be ungrouped (not deleted).</p>
            <div className="flex justify-end gap-2">
              <button className="btn-secondary !py-1" onClick={() => setConfirmDelete(false)}>
                Cancel
              </button>
              <button className="btn bg-error-600 !py-1 text-white hover:bg-error-700" disabled={deleting} onClick={() => void del()}>
                {deleting ? 'Deleting…' : 'Delete'}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

// ---------- page ----------
export default function Dashboard() {
  const { currentUser } = useAuthContext()
  const isAdmin = currentUser?.is_admin ?? false
  const navigate = useNavigate()
  const { monitors, loading, error, refetch } = useMonitors()
  const { groups, refetch: refetchGroups } = useMonitorGroups()
  const { usernameFor } = useUsers()
  const { toasts, push } = useToasts()

  // 24h uptime for every monitor in one call, powering each card's fallback
  // percentage. Fixed at mount so the hook doesn't refetch on every render.
  const window24h = useMemo(() => {
    const end = new Date()
    return { start: new Date(end.getTime() - 24 * 3600e3).toISOString(), end: end.toISOString() }
  }, [])
  const { report: summary } = useSummaryReport(window24h.start, window24h.end)
  const uptimeById = useMemo(() => {
    const m = new Map<string, number>()
    for (const row of summary?.monitors ?? []) m.set(row.monitor_id, row.uptime_percent)
    return m
  }, [summary])

  // The sidebar's reporting window is independently selectable.
  const [period, setPeriod] = useState<ReportPeriod>('30d')
  const periodRange = useMemo(() => {
    const hours = REPORT_PERIODS.find((p) => p.key === period)?.hours ?? 24 * 30
    const end = new Date()
    return { start: new Date(end.getTime() - hours * 3600e3).toISOString(), end: end.toISOString() }
  }, [period])
  const { report: periodSummary, loading: periodLoading } = useSummaryReport(periodRange.start, periodRange.end)

  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [modal, setModal] = useState<{ mode: 'create' | 'edit'; group?: MonitorGroup } | null>(null)
  const [search, setSearch] = useState('')
  const debouncedSearch = useDebounced(search, 300)
  const [selectedTags, setSelectedTags] = useState<string[]>([])
  const [typeFilter, setTypeFilter] = useState<string>('all')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [groupFilter, setGroupFilter] = useState<string>('all') // 'all' | 'ungrouped' | groupId
  const [sort, setSort] = useState<SortKey>('down-first')

  useEffect(() => {
    const t = window.setInterval(() => {
      void refetch()
      void refetchGroups()
    }, REFRESH_MS)
    return () => window.clearInterval(t)
  }, [refetch, refetchGroups])

  const refetchAll = useCallback(async () => {
    await Promise.all([refetch(), refetchGroups()])
  }, [refetch, refetchGroups])

  // Live counts for the status card. "Paused" is a configuration state, so a
  // disabled monitor is counted as paused rather than by its last known status.
  const counts = useMemo(() => {
    let down = 0
    let up = 0
    let paused = 0
    for (const m of monitors) {
      if (!m.enabled) paused++
      else if (m.current_status === 'offline') down++
      else if (m.current_status === 'online') up++
    }
    return { down, up, paused, active: monitors.length - paused, total: monitors.length }
  }, [monitors])

  // Overview figures for the hero cards. Average response time is computed from
  // each monitor's last recorded value rather than a separate API call, so the
  // number always matches what the cards below are showing.
  const overview = useMemo(() => {
    const timed = monitors.filter((m) => m.enabled && m.last_response_time_ms > 0)
    const avgResponse = timed.length
      ? Math.round(timed.reduce((sum, m) => sum + m.last_response_time_ms, 0) / timed.length)
      : 0
    return { avgResponse, timedCount: timed.length }
  }, [monitors])

  // Counts per monitor type, for the four type cards.
  const byType = useMemo(() => {
    const keys = ['dns', 'http', 'ping', 'tcp'] as const
    return keys.map((key) => {
      const of = monitors.filter((m) => m.type === key)
      return {
        key,
        label: key.toUpperCase(),
        count: of.length,
        online: of.filter((m) => m.enabled && m.current_status === 'online').length,
      }
    })
  }, [monitors])

  // One shimmer position per card, so the highlight follows the cursor on the
  // hovered card only.
  const shimmer = useCardShimmer([
    'status',
    'responseTime',
    'incidents',
    'agents',
    'dns',
    'http',
    'ping',
    'tcp',
  ])

  const allTags = useMemo(() => {
    const s = new Set<string>()
    for (const m of monitors) (m.tags ?? []).forEach((t) => s.add(t))
    return Array.from(s).sort()
  }, [monitors])
  const allTypes = useMemo(() => {
    const s = new Set<string>()
    for (const m of monitors) s.add(m.type)
    return Array.from(s).sort()
  }, [monitors])

  const activeFilterCount =
    (typeFilter !== 'all' ? 1 : 0) +
    (statusFilter !== 'all' ? 1 : 0) +
    (groupFilter !== 'all' ? 1 : 0) +
    selectedTags.length
  const filterActive = debouncedSearch.trim() !== '' || activeFilterCount > 0

  const filtered = useMemo(() => {
    const q = debouncedSearch.trim().toLowerCase()
    const matches = monitors.filter((m) => {
      const matchQ = !q || m.name.toLowerCase().includes(q) || m.url.toLowerCase().includes(q)
      const matchType = typeFilter === 'all' || m.type === typeFilter
      const matchStatus =
        statusFilter === 'all' ||
        (statusFilter === 'maintenance'
          ? !!m.is_in_maintenance
          : statusFilter === 'paused'
            ? !m.enabled
            : m.current_status === statusFilter)
      const matchGroup =
        groupFilter === 'all' || (groupFilter === 'ungrouped' ? !m.group_id : m.group_id === groupFilter)
      const matchTags = selectedTags.length === 0 || (m.tags ?? []).some((t) => selectedTags.includes(t))
      return matchQ && matchType && matchStatus && matchGroup && matchTags
    })

    const byName = (a: Monitor, b: Monitor) => a.name.localeCompare(b.name)
    return [...matches].sort((a, b) => {
      switch (sort) {
        case 'name':
          return byName(a, b)
        case 'uptime': {
          const ua = uptimeById.get(a.id) ?? 100
          const ub = uptimeById.get(b.id) ?? 100
          return ua - ub || byName(a, b)
        }
        case 'slowest':
          return b.last_response_time_ms - a.last_response_time_ms || byName(a, b)
        default:
          return urgency(a) - urgency(b) || byName(a, b)
      }
    })
  }, [monitors, debouncedSearch, typeFilter, statusFilter, groupFilter, selectedTags, sort, uptimeById])

  const clearAllFilters = () => {
    setTypeFilter('all')
    setStatusFilter('all')
    setGroupFilter('all')
    setSelectedTags([])
  }

  const ungrouped = useMemo(() => filtered.filter((m) => !m.group_id), [filtered])
  const monitorsByGroup = useMemo(() => {
    const map = new Map<string, Monitor[]>()
    for (const m of filtered) {
      if (m.group_id) {
        const list = map.get(m.group_id) ?? []
        list.push(m)
        map.set(m.group_id, list)
      }
    }
    return map
  }, [filtered])

  const toggleCard = useCallback((id: string) => setExpandedId((cur) => (cur === id ? null : id)), [])
  const toggleGroup = (id: string) => setCollapsed((c) => ({ ...c, [id]: !c[id] }))
  const toggleTag = (t: string) =>
    setSelectedTags((cur) => (cur.includes(t) ? cur.filter((x) => x !== t) : [...cur, t]))

  const cardProps = { groups, onToggle: toggleCard, onChanged: () => void refetchAll(), push }

  return (
    // The inner-scroll layout is a desktop affordance: below xl the sidebar
    // stacks and the page scrolls normally, which suits a narrow screen better.
    <div className="flex flex-col gap-4 xl:h-full">
      {/* ---- Overview -------------------------------------------------- */}
      <div className="shrink-0 space-y-4">
        <div>
          <h1 className="text-3xl font-light text-white">System Overview</h1>
          <p className="mt-1 text-sm text-slate-400">
            {counts.total} monitor{counts.total === 1 ? '' : 's'} &middot; {counts.active} active
            {counts.paused > 0 && ` \u00b7 ${counts.paused} paused`}
          </p>
        </div>

        {/* Headline status. Emerald when everything answers, yellow while some
            are down, red only when nothing active is up. */}
        <div
          className={`relative overflow-hidden rounded-lg border p-6 backdrop-blur-sm transition-all group ${
            counts.down === 0
              ? 'border-emerald-500/30 bg-gradient-to-br from-emerald-600/15 to-slate-800/40'
              : counts.up === 0
                ? 'border-red-500/30 bg-gradient-to-br from-red-600/15 to-slate-800/40'
                : 'border-yellow-500/30 bg-gradient-to-br from-yellow-600/15 to-slate-800/40'
          }`}
          onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'status')}
          onMouseEnter={() => shimmer.handleCardMouseEnter('status')}
          onMouseLeave={() => shimmer.handleCardMouseLeave('status')}
        >
          <div
            className={`pointer-events-none absolute right-0 top-0 -mr-10 -mt-10 h-20 w-20 rounded-full blur-2xl transition-all ${
              counts.down === 0
                ? 'bg-emerald-500/10 group-hover:bg-emerald-500/20'
                : counts.up === 0
                  ? 'bg-red-500/10 group-hover:bg-red-500/20'
                  : 'bg-yellow-500/10 group-hover:bg-yellow-500/20'
            }`}
          />
          {shimmer.isShown('status') && (
            <div
              className="pointer-events-none absolute inset-0 rounded-lg transition-all duration-75"
              style={shimmer.getShimmerStyle('status')}
            />
          )}
          <div className="relative z-10 flex flex-wrap items-end justify-between gap-4">
            <div>
              <div
                className={`mb-2 text-xs font-semibold uppercase tracking-widest ${
                  counts.down === 0
                    ? 'text-emerald-400'
                    : counts.up === 0
                      ? 'text-red-400'
                      : 'text-yellow-400'
                }`}
              >
                System status
              </div>
              <div className="text-3xl font-light text-white">
                {counts.down === 0 ? 'Operational' : counts.up === 0 ? 'Major outage' : 'Degraded'}
              </div>
            </div>
            <div className="flex gap-6 text-sm">
              <div>
                <div className="text-xs uppercase tracking-widest text-slate-400">Up</div>
                <div className="text-2xl font-light text-emerald-400">{counts.up}</div>
              </div>
              <div>
                <div className="text-xs uppercase tracking-widest text-slate-400">Down</div>
                <div className="text-2xl font-light text-red-400">{counts.down}</div>
              </div>
              <div>
                <div className="text-xs uppercase tracking-widest text-slate-400">Paused</div>
                <div className="text-2xl font-light text-slate-300">{counts.paused}</div>
              </div>
            </div>
          </div>
        </div>

        {/* Three stat cards, each with its own cursor highlight. */}
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <ShimmerStatCard
            title="Avg Response Time"
            value={overview.avgResponse > 0 ? `${overview.avgResponse}ms` : '\u2014'}
            subtitle={
              overview.timedCount > 0 ? `across ${overview.timedCount} monitors` : 'no data yet'
            }
            colorType="responseTime"
            onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'responseTime')}
            onMouseEnter={() => shimmer.handleCardMouseEnter('responseTime')}
            onMouseLeave={() => shimmer.handleCardMouseLeave('responseTime')}
            showShimmer={shimmer.isShown('responseTime')}
            shimmerStyle={shimmer.getShimmerStyle('responseTime')}
          />
          <ShimmerStatCard
            title="Total Incidents"
            value={summary?.aggregate.total_incidents ?? 0}
            subtitle="last 24 hours"
            colorType="incidents"
            onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'incidents')}
            onMouseEnter={() => shimmer.handleCardMouseEnter('incidents')}
            onMouseLeave={() => shimmer.handleCardMouseLeave('incidents')}
            showShimmer={shimmer.isShown('incidents')}
            shimmerStyle={shimmer.getShimmerStyle('incidents')}
          />
          {/* Agents are not implemented yet - see the Server Monitoring page.
              Shown as zero with an explicit note rather than repurposing the
              monitor count, which would read as a working feature. */}
          <ShimmerStatCard
            title="Monitoring Agents"
            value={0}
            subtitle="coming soon"
            colorType="agents"
            onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'agents')}
            onMouseEnter={() => shimmer.handleCardMouseEnter('agents')}
            onMouseLeave={() => shimmer.handleCardMouseLeave('agents')}
            showShimmer={shimmer.isShown('agents')}
            shimmerStyle={shimmer.getShimmerStyle('agents')}
          />
        </div>

        {/* Type breakdown. Clicking a card filters the list below to that type. */}
        <div className="grid gap-4 sm:grid-cols-2">
          {byType.map((t) => (
            <ShimmerTypeCard
              key={t.key}
              label={t.label}
              count={t.count}
              online={t.online}
              colorType={t.key}
              onClick={() => setTypeFilter(typeFilter === t.key ? 'all' : t.key)}
              onMouseMove={(e) => shimmer.handleCardMouseMove(e, t.key)}
              onMouseEnter={() => shimmer.handleCardMouseEnter(t.key)}
              onMouseLeave={() => shimmer.handleCardMouseLeave(t.key)}
              showShimmer={shimmer.isShown(t.key)}
              shimmerStyle={shimmer.getShimmerStyle(t.key)}
            />
          ))}
        </div>
      </div>

      {/* Toolbar: everything needed to find a monitor, on one line. */}
      <div className="flex shrink-0 flex-wrap items-center gap-2.5">
        {/* normal-case overrides vs-title's uppercase: the title is set as
            plain sentence-case text rather than an instrument label. */}
        <h1 className="vs-title shrink-0 text-2xl normal-case">Monitors.</h1>

        <div className="relative min-w-[180px] flex-1">
          <Search
            className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2"
            style={{ color: 'var(--vs-text-dim)' }}
          />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search by name or URL"
            aria-label="Search by name or URL"
            className="rd-input py-2 pl-9 pr-8"
          />
          {search && (
            <button
              onClick={() => setSearch('')}
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5"
              style={{ color: 'var(--vs-text-dim)' }}
              aria-label="Clear search"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>

        <FilterMenu
          activeCount={activeFilterCount}
          allTypes={allTypes}
          allTags={allTags}
          groups={groups}
          typeFilter={typeFilter}
          statusFilter={statusFilter}
          groupFilter={groupFilter}
          selectedTags={selectedTags}
          setTypeFilter={setTypeFilter}
          setStatusFilter={setStatusFilter}
          setGroupFilter={setGroupFilter}
          toggleTag={toggleTag}
          clearAll={clearAllFilters}
        />

        <select
          className={`${selectCls} shrink-0`}
          value={sort}
          onChange={(e) => setSort(e.target.value as SortKey)}
          aria-label="Sort monitors"
        >
          {SORTS.map((s) => (
            <option key={s.key} value={s.key}>
              {s.label}
            </option>
          ))}
        </select>

        <button
          className="rd-btn rd-btn-secondary shrink-0"
          onClick={() => void refetchAll()}
          disabled={loading}
          aria-label="Refresh"
        >
          <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
        </button>

        <NewMenu
          onSingle={() => navigate('/monitors/create')}
          onWizard={() => navigate('/monitors/new/wizard')}
          onBulk={() => navigate('/monitors/bulk')}
          onDiscover={() => navigate('/monitors/discover')}
          onGroup={() => setModal({ mode: 'create' })}
	  isAdmin={isAdmin}
        />
      </div>

      {error && (
        <div className="card flex shrink-0 items-center justify-between border-error-300 p-3">
          <span className="text-error-700 dark:text-error-300">Failed to load monitors: {error.message}</span>
          <button className="btn-secondary" onClick={() => void refetch()}>
            Retry
          </button>
        </div>
      )}

      {/* Body: the list owns the scroll so the toolbar and sidebar stay put. */}
      <div className="flex flex-col gap-4 xl:min-h-0 xl:flex-1 xl:flex-row">
        <aside className="order-first shrink-0 xl:order-last xl:w-72">
          <StatusSidebar
            down={counts.down}
            up={counts.up}
            paused={counts.paused}
            active={counts.active}
            total={counts.total}
            period={period}
            onPeriodChange={setPeriod}
            summary={periodSummary}
            summaryLoading={periodLoading}
          />
        </aside>

        <div className="min-w-0 flex-1 xl:min-h-0 xl:overflow-y-auto xl:pr-1">
          {loading && monitors.length === 0 ? (
            <div className="space-y-2">
              {Array.from({ length: 6 }).map((_, i) => (
                <div key={i} className="rd-card h-[76px] animate-pulse" />
              ))}
            </div>
          ) : monitors.length === 0 ? (
            <div className="card flex flex-col items-center gap-3 p-12 text-center">
              <div className="text-lg font-semibold">No monitors yet</div>
              <p className="text-sm text-neutral-500 dark:text-neutral-400">
                Create your first monitor to start tracking uptime.
              </p>
              <button className="btn-primary" onClick={() => navigate('/monitors/create')}>
                <Plus className="h-4 w-4" /> Create Your First Monitor
              </button>
            </div>
          ) : filterActive && filtered.length === 0 ? (
            <div className="card p-8 text-center text-sm text-neutral-500 dark:text-neutral-400">
              No monitors match the current search or filters.
            </div>
          ) : (
            <div className="space-y-5">
              {groups.map((g) => {
                const members = monitorsByGroup.get(g.id) ?? []
                if (filterActive && members.length === 0) return null
                return (
                  <GroupSection
                    key={g.id}
                    title={g.name}
                    color={g.color}
                    uptime={g.group_uptime}
                    count={members.length}
                    expanded={!collapsed[g.id]}
                    onToggle={() => toggleGroup(g.id)}
                    onEdit={() => setModal({ mode: 'edit', group: g })}
                  >
                    {members.length === 0 ? (
                      <p className="px-2 py-1 text-sm text-neutral-400">
                        No monitors in this group yet — assign one from a card’s Group dropdown.
                      </p>
                    ) : (
                      members.map((m) => (
                        <MonitorCard
                          key={m.id}
                          monitor={m}
                          uptime24h={uptimeById.get(m.id) ?? null}
                          expanded={expandedId === m.id}
                          ownerUsername={usernameFor(m.owner_id)}
                          {...cardProps}
                        />
                      ))
                    )}
                  </GroupSection>
                )
              })}

              {ungrouped.length > 0 &&
                (groups.length > 0 ? (
                  <GroupSection
                    title="Ungrouped"
                    color={null}
                    uptime={null}
                    count={ungrouped.length}
                    expanded={!collapsed.__ungrouped}
                    onToggle={() => toggleGroup('__ungrouped')}
                  >
                    {ungrouped.map((m) => (
                      <MonitorCard
                        key={m.id}
                        monitor={m}
                        uptime24h={uptimeById.get(m.id) ?? null}
                        expanded={expandedId === m.id}
                        ownerUsername={usernameFor(m.owner_id)}
                        {...cardProps}
                      />
                    ))}
                  </GroupSection>
                ) : (
                  <div className="space-y-2.5">
                    {ungrouped.map((m) => (
                      <MonitorCard
                        key={m.id}
                        monitor={m}
                        uptime24h={uptimeById.get(m.id) ?? null}
                        expanded={expandedId === m.id}
                        ownerUsername={usernameFor(m.owner_id)}
                        {...cardProps}
                      />
                    ))}
                  </div>
                ))}
            </div>
          )}
        </div>
      </div>

      {modal && (
        <GroupModal mode={modal.mode} group={modal.group} onClose={() => setModal(null)} onSaved={() => void refetchAll()} push={push} />
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}
