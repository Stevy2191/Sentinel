import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Plus,
  Pencil,
  Trash2,
  Play,
  Pause,
  ExternalLink,
  ArrowUpDown,
  Search,
} from 'lucide-react'
import {
  useMonitors,
  useDeleteMonitor,
  usePauseMonitor,
  useResumeMonitor,
} from '@/hooks/useMonitors'
import { useToasts, Toaster } from '@/components/Toast'
import { useUsers } from '@/hooks/useUsers'
import { formatResponseTime, formatDate } from '@/utils/formatters'
import { monitorAccess, badgeToneClass } from '@/utils/monitorAccess'
import type { Monitor, MonitorStatus, MonitorType } from '@/types'
import MonitorTypeBadge from '@/components/MonitorTypeBadge'

const PAGE_SIZE = 50
type SortKey = 'name' | 'status' | 'response'

function responseColor(ms: number): string {
  if (ms <= 0) return 'text-slate-400'
  if (ms < 200) return 'text-emerald-500'
  if (ms <= 500) return 'text-amber-500'
  return 'text-red-500'
}

export default function Monitors() {
  const navigate = useNavigate()
  const { monitors, loading, error, refetch } = useMonitors({ limit: 500 })
  const { delete: deleteMonitor } = useDeleteMonitor()
  const { pause } = usePauseMonitor()
  const { resume } = useResumeMonitor()
  const { usernameFor } = useUsers()
  const { toasts, push } = useToasts()

  const [search, setSearch] = useState('')
  const [typeFilter, setTypeFilter] = useState<MonitorType | ''>('')
  const [statusFilter, setStatusFilter] = useState<MonitorStatus | ''>('')
  const [sortKey, setSortKey] = useState<SortKey>('name')
  const [sortAsc, setSortAsc] = useState(true)
  const [page, setPage] = useState(1)
  const [confirmId, setConfirmId] = useState<string | null>(null)

  const filtered = useMemo(() => {
    let rows = monitors
    if (search.trim()) {
      const q = search.trim().toLowerCase()
      rows = rows.filter(
        (m) => m.name.toLowerCase().includes(q) || m.url.toLowerCase().includes(q)
      )
    }
    if (typeFilter) rows = rows.filter((m) => m.type === typeFilter)
    if (statusFilter) rows = rows.filter((m) => m.current_status === statusFilter)

    const sorted = [...rows].sort((a, b) => {
      let cmp = 0
      if (sortKey === 'name') cmp = a.name.localeCompare(b.name)
      else if (sortKey === 'status') cmp = a.current_status.localeCompare(b.current_status)
      else cmp = a.last_response_time_ms - b.last_response_time_ms
      return sortAsc ? cmp : -cmp
    })
    return sorted
  }, [monitors, search, typeFilter, statusFilter, sortKey, sortAsc])

  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE))
  const safePage = Math.min(page, totalPages)
  const pageItems = filtered.slice((safePage - 1) * PAGE_SIZE, safePage * PAGE_SIZE)

  const toggleSort = (key: SortKey) => {
    if (sortKey === key) setSortAsc((s) => !s)
    else {
      setSortKey(key)
      setSortAsc(true)
    }
  }

  const handlePauseResume = async (m: Monitor) => {
    try {
      if (m.enabled) {
        await pause(m.id)
        push(`${m.name} paused`, 'info')
      } else {
        await resume(m.id)
        push(`${m.name} resumed`, 'success')
      }
      await refetch()
    } catch {
      push('Action failed', 'error')
    }
  }

  const confirmDelete = async () => {
    if (!confirmId) return
    const target = monitors.find((m) => m.id === confirmId)
    try {
      await deleteMonitor(confirmId)
      push(`${target?.name ?? 'Monitor'} deleted`, 'success')
      await refetch()
    } catch {
      push('Delete failed', 'error')
    } finally {
      setConfirmId(null)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="vs-title text-4xl">Monitors</h1>
          <p className="text-sm text-slate-400">
            {filtered.length} of {monitors.length} monitors
          </p>
        </div>
        <button className="btn-primary" onClick={() => navigate('/monitors/create')}>
          <Plus className="h-4 w-4" />
          Create New Monitor
        </button>
      </div>

      {/* Filters */}
      <div className="card flex flex-wrap items-center gap-3 p-4">
        <div className="relative flex-1 min-w-[200px]">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
          <input
            className="w-full rounded-lg border border-white/10 bg-slate-900/60 py-2 pl-9 pr-3 text-sm text-white placeholder-slate-500"
            placeholder="Search by name or URL"
            value={search}
            onChange={(e) => {
              setSearch(e.target.value)
              setPage(1)
            }}
          />
        </div>
        <select
          className="rd-select"
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value as MonitorType | '')}
        >
          <option value="">All types</option>
          <option value="http">HTTP</option>
          <option value="tcp">TCP</option>
          <option value="ping">Ping</option>
          <option value="dns">DNS</option>
        </select>
        <select
          className="rd-select"
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value as MonitorStatus | '')}
        >
          <option value="">All statuses</option>
          <option value="online">Online</option>
          <option value="offline">Offline</option>
          <option value="unknown">Unknown</option>
        </select>
      </div>

      {error && (
        <div className="card flex items-center justify-between border-error-300 p-4">
          <span className="text-red-400">{error.message}</span>
          <button className="btn-secondary" onClick={() => void refetch()}>
            Retry
          </button>
        </div>
      )}

      {/* Table */}
      <div className="card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-white/10 text-slate-400">
              <tr>
                <th className="px-4 py-3 font-medium">
                  <button className="flex items-center gap-1" onClick={() => toggleSort('name')}>
                    Name <ArrowUpDown className="h-3 w-3" />
                  </button>
                </th>
                <th className="px-4 py-3 font-medium">Type</th>
                <th className="px-4 py-3 font-medium">Owner</th>
                <th className="px-4 py-3 font-medium">Permission</th>
                <th className="px-4 py-3 font-medium">URL</th>
                <th className="px-4 py-3 font-medium">
                  <button className="flex items-center gap-1" onClick={() => toggleSort('status')}>
                    Status <ArrowUpDown className="h-3 w-3" />
                  </button>
                </th>
                <th className="px-4 py-3 font-medium">Last Check</th>
                <th className="px-4 py-3 font-medium">
                  <button className="flex items-center gap-1" onClick={() => toggleSort('response')}>
                    Response <ArrowUpDown className="h-3 w-3" />
                  </button>
                </th>
                <th className="px-4 py-3 text-right font-medium">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/5">
              {loading && monitors.length === 0 ? (
                Array.from({ length: 5 }).map((_, i) => (
                  <tr key={i} className="animate-pulse">
                    <td colSpan={9} className="px-4 py-4">
                      <div className="h-4 rounded bg-white/10" />
                    </td>
                  </tr>
                ))
              ) : pageItems.length === 0 ? (
                <tr>
                  <td colSpan={9} className="px-4 py-10 text-center text-slate-500">
                    No monitors. Create your first one.
                  </td>
                </tr>
              ) : (
                pageItems.map((m) => (
                  <tr
                    key={m.id}
                    className="cursor-pointer hover:bg-white/5"
                    onClick={() => navigate(`/monitors/${m.id}`)}
                  >
                    <td className="px-4 py-3">
                      <span className="font-medium">{m.name}</span>
                      {!m.enabled && <span className="ml-2 text-xs text-slate-400">paused</span>}
                    </td>
                    <td className="px-4 py-3">
                      <MonitorTypeBadge type={m.type} />
                    </td>
                    <td className="px-4 py-3 text-slate-500">
                      {monitorAccess(m).isOwner ? (
                        <span className="font-medium text-emerald-400">You</span>
                      ) : (
                        usernameFor(m.owner_id) ?? '—'
                      )}
                    </td>
                    <td className="px-4 py-3">
                      {monitorAccess(m).badge ? (
                        <span className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${badgeToneClass[monitorAccess(m).badge!.tone]}`}>
                          {monitorAccess(m).badge!.label}
                        </span>
                      ) : (
                        <span className="text-xs text-slate-400">Owner</span>
                      )}
                    </td>
                    <td className="max-w-[220px] truncate px-4 py-3 text-slate-500">{m.url}</td>
                    <td className="px-4 py-3">
                      <span
                        className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium ${
                          m.current_status === 'online'
                            ? 'border-emerald-500/30 bg-emerald-500/20 text-emerald-400'
                            : m.current_status === 'offline'
                              ? 'border-red-500/30 bg-red-500/20 text-red-400'
                              : 'border-slate-500/30 bg-slate-500/20 text-slate-400'
                        }`}
                      >
                        <span
                          className={`h-1.5 w-1.5 rounded-full ${
                            m.current_status === 'online'
                              ? 'bg-emerald-400'
                              : m.current_status === 'offline'
                                ? 'bg-red-400'
                                : 'bg-slate-400'
                          }`}
                        />
                        {m.current_status === 'online'
                          ? 'Up'
                          : m.current_status === 'offline'
                            ? 'Down'
                            : 'Pending'}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-slate-500">
                      {m.last_check_at ? formatDate(m.last_check_at) : 'Never'}
                    </td>
                    <td className={`px-4 py-3 font-medium ${responseColor(m.last_response_time_ms)}`}>
                      {formatResponseTime(m.last_response_time_ms)}
                    </td>
                    <td className="px-4 py-3" onClick={(e) => e.stopPropagation()}>
                      <div className="flex justify-end gap-1">
                        <button
                          className="btn-secondary !px-2 !py-1"
                          title="Details"
                          onClick={() => navigate(`/monitors/${m.id}`)}
                        >
                          <ExternalLink className="h-4 w-4" />
                        </button>
                        {monitorAccess(m).canEdit && (
                          <button
                            className="btn-secondary !px-2 !py-1"
                            title="Edit"
                            onClick={() => navigate(`/monitors/${m.id}/edit`)}
                          >
                            <Pencil className="h-4 w-4" />
                          </button>
                        )}
                        {monitorAccess(m).canEdit && (
                          <button
                            className="btn-secondary !px-2 !py-1"
                            title={m.enabled ? 'Pause' : 'Resume'}
                            onClick={() => void handlePauseResume(m)}
                          >
                            {m.enabled ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
                          </button>
                        )}
                        {monitorAccess(m).canDelete && (
                          <button
                            className="btn-secondary !px-2 !py-1 text-error-600"
                            title="Delete"
                            onClick={() => setConfirmId(m.id)}
                          >
                            <Trash2 className="h-4 w-4" />
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      <div className="flex items-center justify-between text-sm text-slate-500">
        <span>
          Page {safePage} of {totalPages}
        </span>
        <div className="flex gap-2">
          <button className="btn-secondary" disabled={safePage <= 1} onClick={() => setPage(safePage - 1)}>
            Previous
          </button>
          <button
            className="btn-secondary"
            disabled={safePage >= totalPages}
            onClick={() => setPage(safePage + 1)}
          >
            Next
          </button>
        </div>
      </div>

      {/* Delete confirmation modal */}
      {confirmId && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div className="card w-full max-w-sm p-6">
            <h3 className="text-lg font-semibold">Delete monitor?</h3>
            <p className="mt-2 text-sm text-slate-400">
              This permanently deletes the monitor and all its check history. This cannot be undone.
            </p>
            <div className="mt-6 flex justify-end gap-2">
              <button className="btn-secondary" onClick={() => setConfirmId(null)}>
                Cancel
              </button>
              <button
                className="btn bg-error-600 text-white hover:bg-error-700"
                onClick={() => void confirmDelete()}
              >
                Delete
              </button>
            </div>
          </div>
        </div>
      )}

      <Toaster toasts={toasts} />
    </div>
  )
}
