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
import MonitorTable from '@/components/MonitorTable'
import { REPORT_PERIODS, type ReportPeriod } from '@/utils/reportPeriods'
import type { Monitor, MonitorGroup } from '@/types'
import { useCardShimmer } from '@/hooks/useCardShimmer'
import ShimmerStatCard from '@/components/ShimmerStatCard'
import ShimmerTypeCard from '@/components/ShimmerTypeCard'

const REFRESH_MS = 30_000
const DEFAULT_GROUP_COLOR = '#10b981'

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

type StatusFilter = 'all' | 'online' | 'offline' | 'maintenance' | 'paused' | 'unknown'

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
                        on ? 'bg-primary-600 text-white' : 'bg-slate-800 text-slate-300 hover:bg-slate-700'
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
    'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

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
        <div className="flex items-center justify-between gap-2 border-t border-white/10 pt-4">
          {mode === 'edit' ? (
            <button
              className="btn border border-red-500/30 text-red-400 hover:bg-red-500/10"
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
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm">
            <p className="mb-2 text-amber-300">Delete this group? Its monitors will be ungrouped (not deleted).</p>
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

  const [refreshedAt, setRefreshedAt] = useState(() => Date.now())
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
      setRefreshedAt(Date.now())
    }, REFRESH_MS)
    return () => window.clearInterval(t)
  }, [refetch, refetchGroups])

  const refetchAll = useCallback(async () => {
    await Promise.all([refetch(), refetchGroups()])
    setRefreshedAt(Date.now())
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
    'operational',
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

  // The reference stamps the overview with when it last refreshed.
  const lastUpdated = useMemo(
    () => new Date(refreshedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    [refreshedAt]
  )

  // Two states only, as in the reference: emerald while everything answers,
  // yellow the moment anything is down.
  const anyDown = counts.down > 0
  const mainCard = anyDown
    ? {
        bg: 'from-yellow-600/20',
        border: 'border-yellow-500/30',
        text: 'text-yellow-400',
        glow: 'bg-yellow-500/10',
      }
    : {
        bg: 'from-emerald-600/20',
        border: 'border-emerald-500/30',
        text: 'text-emerald-400',
        glow: 'bg-emerald-500/10',
      }
  const periodHeading =
    REPORT_PERIODS.find((pp) => pp.key === period)?.heading.toLowerCase() ?? 'last 30 days'
  const periodUptime = periodSummary?.aggregate.avg_uptime

  return (
    <div className="space-y-8">
      {/* ---- Overview -------------------------------------------------- */}
      <div>
        <h1 className="text-4xl font-light text-white">System Overview</h1>
        <p className="mt-2 text-sm text-slate-400">
          {counts.total} service{counts.total === 1 ? '' : 's'} monitored &bull; Last updated{' '}
          {lastUpdated}
        </p>
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        {/* Left two thirds: headline status over the three stat cards. */}
        <div className="space-y-4 lg:col-span-2">
          <div
            className={`group relative overflow-hidden rounded-xl border bg-gradient-to-br ${mainCard.bg} via-slate-800/40 to-cyan-600/20 p-8 backdrop-blur-sm ${mainCard.border}`}
            onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'operational')}
            onMouseEnter={() => shimmer.handleCardMouseEnter('operational')}
            onMouseLeave={() => shimmer.handleCardMouseLeave('operational')}
          >
            {/* Emerald-to-cyan wash across the card, under the content. */}
            <div className="pointer-events-none absolute inset-0 rounded-xl bg-gradient-to-r from-emerald-500/0 via-emerald-500/10 to-cyan-500/0" />

            {shimmer.isShown('operational') && (
              <div
                className="pointer-events-none absolute inset-0 rounded-xl transition-all duration-75"
                style={shimmer.getShimmerStyle('operational')}
              />
            )}

            <div className="relative z-10 flex flex-wrap items-start justify-between gap-6">
              <div>
                <div
                  className={`mb-4 text-xs font-semibold uppercase tracking-widest ${mainCard.text}`}
                >
                  All Services
                </div>
                <div className="mb-2 text-5xl font-light text-white">
                  {counts.active === 0
                    ? 'Idle'
                    : anyDown
                      ? counts.up === 0
                        ? 'Major outage'
                        : 'Degraded'
                      : 'Operational'}
                </div>
                <div className="text-slate-300">
                  {counts.up} of {counts.active} service{counts.active === 1 ? '' : 's'} are up
                  {counts.paused > 0 && (
                    <span className="text-slate-400"> &middot; {counts.paused} paused</span>
                  )}
                </div>
              </div>
              <div className="text-right">
                <div className={`mb-2 text-4xl font-light ${mainCard.text}`}>
                  {periodLoading && periodUptime == null
                    ? '\u2014'
                    : periodUptime != null
                      ? `${periodUptime.toFixed(2)}%`
                      : '\u2014'}
                </div>
                {/* The reference hard-codes "30-day uptime"; the window is
                    selectable here, so the caption names the one in force. */}
                <select
                  value={period}
                  onChange={(e) => setPeriod(e.target.value as ReportPeriod)}
                  aria-label="Uptime reporting window"
                  className="cursor-pointer rounded border border-white/10 bg-slate-900/60 px-2 py-1 text-xs text-slate-400 transition hover:text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-white/40"
                >
                  {REPORT_PERIODS.map((pp) => (
                    <option key={pp.key} value={pp.key}>
                      {pp.heading} uptime
                    </option>
                  ))}
                </select>
              </div>
            </div>
          </div>

          {/* Three stat cards, each with its own cursor highlight. */}
          <div className="grid gap-4 sm:grid-cols-3">
            <ShimmerStatCard
              title="Avg Response Time"
              value={overview.avgResponse > 0 ? `${overview.avgResponse}ms` : '\u2014'}
              subtitle={
                overview.timedCount > 0 ? `across ${overview.timedCount}` : 'no data yet'
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
              value={periodSummary?.aggregate.total_incidents ?? 0}
              subtitle={periodHeading}
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
              value="0 online"
              subtitle="coming soon"
              colorType="agents"
              onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'agents')}
              onMouseEnter={() => shimmer.handleCardMouseEnter('agents')}
              onMouseLeave={() => shimmer.handleCardMouseLeave('agents')}
              showShimmer={shimmer.isShown('agents')}
              shimmerStyle={shimmer.getShimmerStyle('agents')}
            />
          </div>
        </div>

        {/* Right third: one tile per monitor type in play. Clicking filters
            the table below to that type. */}
        <div className="lg:col-span-1">
          <div className="grid grid-cols-2 gap-3">
            {byType
              .filter((t) => t.count > 0)
              .map((t) => (
                <ShimmerTypeCard
                  key={t.key}
                  label={t.label}
                  count={t.count}
                  online={t.online}
                  colorType={t.key}
                  active={typeFilter === t.key}
                  onClick={() => setTypeFilter(typeFilter === t.key ? 'all' : t.key)}
                  onMouseMove={(e) => shimmer.handleCardMouseMove(e, t.key)}
                  onMouseEnter={() => shimmer.handleCardMouseEnter(t.key)}
                  onMouseLeave={() => shimmer.handleCardMouseLeave(t.key)}
                  showShimmer={shimmer.isShown(t.key)}
                  shimmerStyle={shimmer.getShimmerStyle(t.key)}
                />
              ))}
            {byType.every((t) => t.count === 0) && (
              <div className="col-span-2 rounded-lg border border-white/10 bg-slate-800/40 p-4 text-sm text-slate-400 backdrop-blur-sm">
                No monitors configured yet.
              </div>
            )}
          </div>
        </div>
      </div>

      {/* ---- Monitored services ---------------------------------------- */}
      <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2.5">
        <h2 className="shrink-0 text-sm font-medium uppercase tracking-wide text-white">
          Monitored Services
        </h2>

        <div className="relative min-w-[180px] flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
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
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-slate-500 transition hover:text-white"
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
        <div className="flex items-center justify-between rounded-lg border border-red-500/30 bg-red-500/10 p-3">
          <span className="text-sm text-red-400">Failed to load monitors: {error.message}</span>
          <button className="btn-secondary" onClick={() => void refetch()}>
            Retry
          </button>
        </div>
      )}

      {loading && monitors.length === 0 ? (
        <div className="space-y-2">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="h-14 animate-pulse rounded-lg border border-white/10 bg-slate-800/40" />
          ))}
        </div>
      ) : monitors.length === 0 ? (
        <div className="flex flex-col items-center gap-3 rounded-lg border border-white/10 bg-slate-800/40 p-12 text-center backdrop-blur-sm">
          <div className="text-lg font-light text-white">No monitors yet</div>
          <p className="text-sm text-slate-400">
            Create your first monitor to start tracking uptime.
          </p>
          <button className="btn-primary" onClick={() => navigate('/monitors/create')}>
            <Plus className="h-4 w-4" /> Create Your First Monitor
          </button>
        </div>
      ) : filterActive && filtered.length === 0 ? (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-8 text-center text-sm text-slate-400 backdrop-blur-sm">
          No monitors match the current search or filters.
        </div>
      ) : (
        <div className="space-y-5">
          {/* Groups keep their own heading, each carrying the reference's
              table beneath it, so grouping survives the switch from cards. */}
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
                  <p className="px-2 py-1 text-sm text-slate-400">
                    No monitors in this group yet &mdash; assign one from a row&rsquo;s Group dropdown.
                  </p>
                ) : (
                  <MonitorTable
                    monitors={members}
                    uptimeById={uptimeById}
                    groups={groups}
                    expandedId={expandedId}
                    onToggle={toggleCard}
                    usernameFor={usernameFor}
                    onChanged={() => void refetchAll()}
                    push={push}
                  />
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
                <MonitorTable
                  monitors={ungrouped}
                  uptimeById={uptimeById}
                  groups={groups}
                  expandedId={expandedId}
                  onToggle={toggleCard}
                  usernameFor={usernameFor}
                  onChanged={() => void refetchAll()}
                  push={push}
                />
              </GroupSection>
            ) : (
              <MonitorTable
                monitors={ungrouped}
                uptimeById={uptimeById}
                groups={groups}
                expandedId={expandedId}
                onToggle={toggleCard}
                usernameFor={usernameFor}
                onChanged={() => void refetchAll()}
                push={push}
              />
            ))}
        </div>
      )}
      </div>

      {modal && (
        <GroupModal mode={modal.mode} group={modal.group} onClose={() => setModal(null)} onSaved={() => void refetchAll()} push={push} />
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}
