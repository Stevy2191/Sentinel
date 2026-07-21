import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { RefreshCw, Plus, FolderPlus, Search, Trash2 } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { useMonitors } from '@/hooks/useMonitors'
import {
  useMonitorGroups,
  useCreateMonitorGroup,
  useUpdateMonitorGroup,
  useDeleteMonitorGroup,
} from '@/hooks/useMonitorGroups'
import { useSummaryReport } from '@/hooks/useReports'
import { useToasts, Toaster } from '@/components/Toast'
import ColorPicker from '@/components/ColorPicker'
import DashboardStats from '@/components/DashboardStats'
import GroupSection from '@/components/GroupSection'
import MonitorCard from '@/components/MonitorCard'
import type { Monitor, MonitorGroup } from '@/types'

const REFRESH_MS = 30_000
const DEFAULT_GROUP_COLOR = '#10b981'

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
  const navigate = useNavigate()
  const { monitors, loading, error, refetch } = useMonitors()
  const { groups, refetch: refetchGroups } = useMonitorGroups()
  const { toasts, push } = useToasts()

  // 24h uptime for every monitor in one call (summary endpoint). Window is fixed
  // at mount so the summary hook doesn't refetch on every render.
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

  const [updatedAt, setUpdatedAt] = useState<Date>(new Date())
  const [, setTick] = useState(0)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [modal, setModal] = useState<{ mode: 'create' | 'edit'; group?: MonitorGroup } | null>(null)
  const [search, setSearch] = useState('')
  const [selectedTags, setSelectedTags] = useState<string[]>([])

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

  const refetchAll = useCallback(async () => {
    await Promise.all([refetch(), refetchGroups()])
  }, [refetch, refetchGroups])

  const stats = useMemo(() => {
    const total = monitors.length
    const online = monitors.filter((m) => m.current_status === 'online').length
    const offline = monitors.filter((m) => m.current_status === 'offline').length
    const responders = monitors.filter((m) => m.last_response_time_ms > 0)
    const avg = responders.length > 0 ? Math.round(responders.reduce((s, m) => s + m.last_response_time_ms, 0) / responders.length) : 0
    return { total, online, offline, avg }
  }, [monitors])

  const allTags = useMemo(() => {
    const s = new Set<string>()
    for (const m of monitors) (m.tags ?? []).forEach((t) => s.add(t))
    return Array.from(s).sort()
  }, [monitors])

  const filterActive = search.trim() !== '' || selectedTags.length > 0
  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    return monitors.filter((m) => {
      const matchQ = !q || m.name.toLowerCase().includes(q) || m.url.toLowerCase().includes(q)
      const matchTags = selectedTags.length === 0 || (m.tags ?? []).some((t) => selectedTags.includes(t))
      return matchQ && matchTags
    })
  }, [monitors, search, selectedTags])

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
          <button className="btn-primary" onClick={() => navigate('/monitors/create')}>
            <Plus className="h-4 w-4" /> New Monitor
          </button>
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

      {/* Always-visible stats */}
      <DashboardStats total={stats.total} online={stats.online} offline={stats.offline} avgResponseMs={stats.avg} />

      {/* Search + tag filter */}
      {monitors.length > 0 && (
        <div className="space-y-2">
          <div className="relative max-w-sm">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-neutral-400" />
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search monitors…"
              className="w-full rounded-md border border-neutral-300 bg-white py-2 pl-9 pr-3 text-sm dark:border-neutral-700 dark:bg-neutral-800 focus:outline-none focus:ring-2 focus:ring-primary-500"
            />
          </div>
          {allTags.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-xs text-neutral-500 dark:text-neutral-400">Tags:</span>
              {allTags.map((t) => {
                const on = selectedTags.includes(t)
                return (
                  <button
                    key={t}
                    onClick={() => toggleTag(t)}
                    className={`rounded-full px-2.5 py-0.5 text-xs font-medium transition ${
                      on
                        ? 'bg-primary-600 text-white'
                        : 'bg-neutral-100 text-neutral-600 hover:bg-neutral-200 dark:bg-neutral-800 dark:text-neutral-300 dark:hover:bg-neutral-700'
                    }`}
                  >
                    {t}
                  </button>
                )
              })}
              {selectedTags.length > 0 && (
                <button onClick={() => setSelectedTags([])} className="text-xs text-primary-600 hover:underline">
                  clear
                </button>
              )}
            </div>
          )}
        </div>
      )}

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
          <p className="text-sm text-neutral-500 dark:text-neutral-400">Create your first monitor to start tracking uptime.</p>
          <button className="btn-primary" onClick={() => navigate('/monitors/create')}>
            <Plus className="h-4 w-4" /> Create Your First Monitor
          </button>
        </div>
      ) : filterActive && filtered.length === 0 ? (
        <div className="card p-8 text-center text-sm text-neutral-500 dark:text-neutral-400">
          No monitors match the current search/tag filter.
        </div>
      ) : (
        <div className="space-y-6">
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
                    <MonitorCard key={m.id} monitor={m} uptime24h={uptimeById.get(m.id) ?? null} expanded={expandedId === m.id} {...cardProps} />
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
                  <MonitorCard key={m.id} monitor={m} uptime24h={uptimeById.get(m.id) ?? null} expanded={expandedId === m.id} {...cardProps} />
                ))}
              </GroupSection>
            ) : (
              <div className="space-y-2">
                {ungrouped.map((m) => (
                  <MonitorCard key={m.id} monitor={m} uptime24h={uptimeById.get(m.id) ?? null} expanded={expandedId === m.id} {...cardProps} />
                ))}
              </div>
            ))}
        </div>
      )}

      {modal && (
        <GroupModal mode={modal.mode} group={modal.group} onClose={() => setModal(null)} onSaved={() => void refetchAll()} push={push} />
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}
