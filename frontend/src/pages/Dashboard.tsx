import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Activity,
  CheckCircle2,
  XCircle,
  Gauge,
  RefreshCw,
  Plus,
  Play,
  ExternalLink,
  ChevronDown,
  ChevronRight,
  FolderPlus,
  Pencil,
  Trash2,
  Wrench,
} from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { useMonitors, useTestMonitor } from '@/hooks/useMonitors'
import {
  useMonitorGroups,
  useCreateMonitorGroup,
  useUpdateMonitorGroup,
  useDeleteMonitorGroup,
  useMoveMonitorToGroup,
} from '@/hooks/useMonitorGroups'
import { useToasts, Toaster } from '@/components/Toast'
import ColorPicker from '@/components/ColorPicker'
import { formatResponseTime, formatDate } from '@/utils/formatters'
import type { Monitor, MonitorGroup } from '@/types'

const REFRESH_MS = 30_000
const DEFAULT_GROUP_COLOR = '#10b981'

function responseColor(ms: number): string {
  if (ms <= 0) return 'text-neutral-400'
  if (ms < 200) return 'text-emerald-500'
  if (ms <= 500) return 'text-amber-500'
  return 'text-red-500'
}

function uptimeColor(pct: number): string {
  if (pct >= 95) return 'text-emerald-500'
  if (pct >= 80) return 'text-amber-500'
  return 'text-red-500'
}

// ---------- compact stat tile ----------
function CompactStat({
  label,
  value,
  icon: Icon,
  tone,
}: {
  label: string
  value: string | number
  icon: typeof Activity
  tone: string
}) {
  return (
    <div className="card flex items-center gap-3 p-3">
      <div className={`rounded-md p-2 ${tone}`}>
        <Icon className="h-4 w-4" />
      </div>
      <div className="min-w-0">
        <div className="text-lg font-bold leading-tight">{value}</div>
        <div className="truncate text-xs text-neutral-500 dark:text-neutral-400">{label}</div>
      </div>
    </div>
  )
}

// ---------- horizontal monitor row ----------
function MonitorRow({
  monitor,
  groups,
  onDetails,
  onTest,
  onMove,
  testing,
}: {
  monitor: Monitor
  groups: MonitorGroup[]
  onDetails: (id: string) => void
  onTest: (id: string, name: string) => void
  onMove: (id: string, groupID: string | null) => void
  testing: boolean
}) {
  const inMaintenance = monitor.is_in_maintenance ?? false
  const online = monitor.current_status === 'online'
  const offline = monitor.current_status === 'offline'

  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-md border border-neutral-200 bg-white px-4 py-3 transition hover:shadow-card-hover dark:border-neutral-800 dark:bg-neutral-900">
      {/* Status dot + name + url */}
      <button
        onClick={() => onDetails(monitor.id)}
        className="flex min-w-0 flex-1 items-center gap-3 text-left"
      >
        <span
          className={`h-2.5 w-2.5 shrink-0 rounded-full ${
            inMaintenance
              ? 'bg-amber-500'
              : online
                ? 'bg-emerald-500'
                : offline
                  ? 'bg-red-500 animate-pulse'
                  : 'bg-neutral-400'
          }`}
        />
        <span className="min-w-0">
          <span className="block truncate font-semibold">{monitor.name}</span>
          <span className="block truncate text-xs text-neutral-500 dark:text-neutral-400">
            {monitor.url}
          </span>
        </span>
      </button>

      {/* Status label */}
      <span className="w-24 shrink-0 text-sm font-medium">
        {inMaintenance ? (
          <span className="flex items-center gap-1 text-amber-600 dark:text-amber-400">
            <Wrench className="h-3.5 w-3.5" /> Maint.
          </span>
        ) : (
          <span className={online ? 'text-emerald-500' : offline ? 'text-red-500' : 'text-neutral-400'}>
            {online ? 'Online' : offline ? 'Offline' : 'Unknown'}
          </span>
        )}
      </span>

      {/* Response time */}
      <span className={`w-16 shrink-0 text-right text-sm font-medium ${responseColor(monitor.last_response_time_ms)}`}>
        {formatResponseTime(monitor.last_response_time_ms)}
      </span>

      {/* Last check */}
      <span className="hidden w-28 shrink-0 text-right text-xs text-neutral-500 dark:text-neutral-400 sm:block">
        {monitor.last_check_at ? formatDate(monitor.last_check_at) : 'Never'}
      </span>

      {/* Move-to-group + actions */}
      <div className="flex shrink-0 items-center gap-1">
        <select
          aria-label="Move to group"
          className="rounded-md border border-neutral-300 bg-white px-2 py-1 text-xs dark:border-neutral-700 dark:bg-neutral-800"
          value={monitor.group_id ?? ''}
          onChange={(e) => onMove(monitor.id, e.target.value || null)}
        >
          <option value="">Ungrouped</option>
          {groups.map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
        </select>
        <button
          className="btn-secondary !px-2 !py-1"
          onClick={() => onTest(monitor.id, monitor.name)}
          disabled={testing}
          title="Test now"
        >
          <Play className="h-4 w-4" />
        </button>
        <button className="btn-secondary !px-2 !py-1" onClick={() => onDetails(monitor.id)} title="Details">
          <ExternalLink className="h-4 w-4" />
        </button>
      </div>
    </div>
  )
}

// ---------- collapsible group section ----------
function GroupSection({
  title,
  color,
  uptime,
  count,
  expanded,
  onToggle,
  onEdit,
  children,
}: {
  title: string
  color: string | null
  uptime: number | null
  count: number
  expanded: boolean
  onToggle: () => void
  onEdit?: () => void
  children: React.ReactNode
}) {
  return (
    <div className="space-y-2">
      <div
        className="flex items-center gap-3 rounded-md border-l-4 bg-neutral-50 px-3 py-2 dark:bg-neutral-800/50"
        style={{ borderLeftColor: color ?? '#94a3b8' }}
      >
        <button onClick={onToggle} className="flex min-w-0 flex-1 items-center gap-2 text-left">
          {expanded ? (
            <ChevronDown className="h-4 w-4 shrink-0 text-neutral-400" />
          ) : (
            <ChevronRight className="h-4 w-4 shrink-0 text-neutral-400" />
          )}
          <span
            className="h-3 w-3 shrink-0 rounded-full"
            style={{ backgroundColor: color ?? '#94a3b8' }}
          />
          <span className="truncate font-semibold">{title}</span>
          <span className="shrink-0 rounded-full bg-neutral-200 px-2 py-0.5 text-xs text-neutral-600 dark:bg-neutral-700 dark:text-neutral-300">
            {count}
          </span>
        </button>
        {uptime != null && (
          <span className={`shrink-0 text-lg font-bold ${uptimeColor(uptime)}`}>{uptime.toFixed(1)}%</span>
        )}
        {onEdit && (
          <button
            onClick={onEdit}
            className="shrink-0 rounded p-1 text-neutral-400 hover:bg-neutral-200 hover:text-neutral-600 dark:hover:bg-neutral-700"
            title="Edit group"
          >
            <Pencil className="h-4 w-4" />
          </button>
        )}
      </div>
      {expanded && <div className="space-y-2 pl-1">{children}</div>}
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
          <input
            autoFocus
            className={inputCls}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Internal Servers"
          />
        </div>
        <div>
          <span className="mb-1 block text-sm font-medium">Description</span>
          <input
            className={inputCls}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Optional"
          />
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
            <p className="mb-2 text-amber-800 dark:text-amber-200">
              Delete this group? Its monitors will be ungrouped (not deleted).
            </p>
            <div className="flex justify-end gap-2">
              <button className="btn-secondary !py-1" onClick={() => setConfirmDelete(false)}>
                Cancel
              </button>
              <button
                className="btn bg-error-600 !py-1 text-white hover:bg-error-700"
                disabled={deleting}
                onClick={() => void del()}
              >
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
  const navigate = useNavigate()
  const { monitors, loading, error, refetch } = useMonitors()
  const { groups, refetch: refetchGroups } = useMonitorGroups()
  const { test } = useTestMonitor()
  const { move } = useMoveMonitorToGroup()
  const { toasts, push } = useToasts()

  const [updatedAt, setUpdatedAt] = useState<Date>(new Date())
  const [, setTick] = useState(0)
  const [testingId, setTestingId] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [modal, setModal] = useState<{ mode: 'create' | 'edit'; group?: MonitorGroup } | null>(null)

  useEffect(() => setUpdatedAt(new Date()), [monitors])
  useEffect(() => {
    const t = window.setInterval(() => setTick((x) => x + 1), 1000)
    return () => window.clearInterval(t)
  }, [])
  useEffect(() => {
    const t = window.setInterval(() => {
      void refetch()
      void refetchGroups()
    }, REFRESH_MS)
    return () => window.clearInterval(t)
  }, [refetch, refetchGroups])

  const stats = useMemo(() => {
    const total = monitors.length
    const online = monitors.filter((m) => m.current_status === 'online').length
    const offline = monitors.filter((m) => m.current_status === 'offline').length
    const responders = monitors.filter((m) => m.last_response_time_ms > 0)
    const avg =
      responders.length > 0
        ? Math.round(responders.reduce((s, m) => s + m.last_response_time_ms, 0) / responders.length)
        : 0
    return { total, online, offline, avg }
  }, [monitors])

  const ungrouped = useMemo(() => monitors.filter((m) => !m.group_id), [monitors])
  const monitorsByGroup = useMemo(() => {
    const map = new Map<string, Monitor[]>()
    for (const m of monitors) {
      if (m.group_id) {
        const list = map.get(m.group_id) ?? []
        list.push(m)
        map.set(m.group_id, list)
      }
    }
    return map
  }, [monitors])

  const refetchAll = useCallback(async () => {
    await Promise.all([refetch(), refetchGroups()])
  }, [refetch, refetchGroups])

  const handleTest = useCallback(
    async (id: string, name: string) => {
      setTestingId(id)
      try {
        const check = await test(id)
        push(`${name}: ${check.status} (${check.response_time_ms}ms)`, check.status === 'success' ? 'success' : 'error')
        await refetch()
      } catch {
        push(`${name}: test failed`, 'error')
      } finally {
        setTestingId(null)
      }
    },
    [test, push, refetch]
  )

  const handleMove = useCallback(
    async (monitorID: string, groupID: string | null) => {
      try {
        await move(monitorID, groupID)
        push(groupID ? 'Monitor moved to group' : 'Monitor ungrouped', 'success')
        await refetchAll()
      } catch (err) {
        push((err as { message?: string }).message ?? 'Failed to move monitor', 'error')
      }
    },
    [move, push, refetchAll]
  )

  const goToDetails = useCallback((id: string) => navigate(`/monitors/${id}`), [navigate])
  const toggle = (id: string) => setCollapsed((c) => ({ ...c, [id]: !c[id] }))

  const rowProps = {
    groups,
    onDetails: goToDetails,
    onTest: handleTest,
    onMove: handleMove,
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold">Dashboard</h1>
          <p className="text-sm text-neutral-500 dark:text-neutral-400">
            Last updated: {formatDistanceToNow(updatedAt, { addSuffix: true })}
          </p>
        </div>
        <div className="flex gap-2">
          <button className="btn-secondary" onClick={() => setModal({ mode: 'create' })}>
            <FolderPlus className="h-4 w-4" /> New Group
          </button>
          <button className="btn-secondary" onClick={() => void refetchAll()} disabled={loading}>
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /> Refresh
          </button>
        </div>
      </div>

      {error && (
        <div className="card flex items-center justify-between border-error-300 p-4">
          <span className="text-error-700 dark:text-error-300">Failed to load monitors: {error.message}</span>
          <button className="btn-secondary" onClick={() => void refetch()}>
            Retry
          </button>
        </div>
      )}

      {/* Compact stats */}
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <CompactStat label="Total Monitors" value={stats.total} icon={Activity} tone="bg-info-100 text-info-600 dark:bg-info-900/40 dark:text-info-400" />
        <CompactStat label="Online" value={stats.online} icon={CheckCircle2} tone="bg-primary-100 text-primary-600 dark:bg-primary-900/40 dark:text-primary-400" />
        <CompactStat label="Offline" value={stats.offline} icon={XCircle} tone="bg-error-100 text-error-600 dark:bg-error-900/40 dark:text-error-400" />
        <CompactStat label="Avg Response" value={formatResponseTime(stats.avg)} icon={Gauge} tone="bg-warning-100 text-warning-600 dark:bg-warning-900/40 dark:text-warning-400" />
      </div>

      {/* Content */}
      {loading && monitors.length === 0 ? (
        <div className="card animate-pulse p-6">
          <div className="h-4 w-1/3 rounded bg-neutral-200 dark:bg-neutral-800" />
          <div className="mt-4 space-y-2">
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="h-12 rounded bg-neutral-200 dark:bg-neutral-800" />
            ))}
          </div>
        </div>
      ) : monitors.length === 0 ? (
        <div className="card flex flex-col items-center gap-3 p-12 text-center">
          <div className="text-lg font-semibold">No monitors yet</div>
          <p className="text-sm text-neutral-500 dark:text-neutral-400">
            Create your first monitor to start tracking uptime.
          </p>
          <button className="btn-primary" onClick={() => navigate('/monitors')}>
            <Plus className="h-4 w-4" /> Create Your First Monitor
          </button>
        </div>
      ) : (
        <div className="space-y-6">
          {/* Groups */}
          {groups.map((g) => {
            const members = monitorsByGroup.get(g.id) ?? []
            const expanded = !collapsed[g.id]
            return (
              <GroupSection
                key={g.id}
                title={g.name}
                color={g.color}
                uptime={g.group_uptime}
                count={members.length}
                expanded={expanded}
                onToggle={() => toggle(g.id)}
                onEdit={() => setModal({ mode: 'edit', group: g })}
              >
                {members.length === 0 ? (
                  <p className="px-2 py-1 text-sm text-neutral-400">
                    No monitors in this group yet — assign one from its dropdown below.
                  </p>
                ) : (
                  members.map((m) => (
                    <MonitorRow key={m.id} monitor={m} testing={testingId === m.id} {...rowProps} />
                  ))
                )}
              </GroupSection>
            )
          })}

          {/* Ungrouped (labeled only when groups exist) */}
          {ungrouped.length > 0 &&
            (groups.length > 0 ? (
              <GroupSection
                title="Ungrouped"
                color={null}
                uptime={null}
                count={ungrouped.length}
                expanded={!collapsed.__ungrouped}
                onToggle={() => toggle('__ungrouped')}
              >
                {ungrouped.map((m) => (
                  <MonitorRow key={m.id} monitor={m} testing={testingId === m.id} {...rowProps} />
                ))}
              </GroupSection>
            ) : (
              <div className="space-y-2">
                {ungrouped.map((m) => (
                  <MonitorRow key={m.id} monitor={m} testing={testingId === m.id} {...rowProps} />
                ))}
              </div>
            ))}
        </div>
      )}

      {modal && (
        <GroupModal
          mode={modal.mode}
          group={modal.group}
          onClose={() => setModal(null)}
          onSaved={() => void refetchAll()}
          push={push}
        />
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}
