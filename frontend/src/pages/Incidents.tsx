import { useEffect, useMemo, useState } from 'react'
import { Search, X, ArrowUpDown, Eye, Loader2 } from 'lucide-react'
import IncidentDetailModal from '@/components/IncidentDetailModal'
import { useMonitors } from '@/hooks/useMonitors'
import {
  useIncidents,
  formatDuration,
  DEFAULT_FILTERS,
  type IncidentFilters,
  type Incident,
} from '@/hooks/useIncidents'

const PAGE_SIZES = [10, 25, 50, 100]

const STATUS_STYLE: Record<Incident['status'], string> = {
  ongoing: 'border-red-500/30 bg-red-500/20 text-red-400',
  resolved: 'border-emerald-500/30 bg-emerald-500/20 text-emerald-400',
}

const TYPE_LABEL: Record<string, string> = {
  down: 'Down',
  timeout: 'Timeout',
  error: 'Error',
}

/** Debounce so typing a monitor name does not fire a request per keystroke. */
function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(value), ms)
    return () => window.clearTimeout(t)
  }, [value, ms])
  return debounced
}

export default function Incidents() {
  const [search, setSearch] = useState('')
  const debouncedSearch = useDebounced(search, 300)
  const [filters, setFilters] = useState<IncidentFilters>(DEFAULT_FILTERS)
  const [openId, setOpenId] = useState<string | null>(null)

  // Any change to what is being asked for returns to the first page: staying on
  // page 4 of a filter that now has one page shows an empty table.
  useEffect(() => {
    setFilters((f) => ({ ...f, search: debouncedSearch, page: 1 }))
  }, [debouncedSearch])

  const { monitors } = useMonitors()
  const { incidents, total, loading, error, refetch } = useIncidents(filters)

  // Sorted by name so the list is scannable; the API filters by id.
  const monitorOptions = useMemo(
    () => [...monitors].sort((a, b) => a.name.localeCompare(b.name)),
    [monitors]
  )

  const pages = Math.max(1, Math.ceil(total / filters.limit))
  const from = total === 0 ? 0 : (filters.page - 1) * filters.limit + 1
  const to = Math.min(filters.page * filters.limit, total)

  const set = <K extends keyof IncidentFilters>(key: K, value: IncidentFilters[K]) =>
    setFilters((f) => ({ ...f, [key]: value, ...(key === 'page' ? {} : { page: 1 }) }))

  const toggleSort = (key: 'started' | 'duration') =>
    setFilters((f) => ({
      ...f,
      sort: key,
      order: f.sort === key && f.order === 'desc' ? 'asc' : 'desc',
      page: 1,
    }))

  const header = (key: 'started' | 'duration', label: string) => (
    <button
      className="inline-flex items-center gap-1 text-xs font-medium text-slate-400 transition hover:text-white"
      onClick={() => toggleSort(key)}
    >
      {label} <ArrowUpDown className="h-3 w-3" />
    </button>
  )

  const ongoing = useMemo(() => incidents.filter((i) => i.status === 'ongoing').length, [incidents])

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-4xl font-light text-white">Incidents</h1>
        <p className="mt-2 text-sm text-slate-400">View all service incidents</p>
      </div>

      <div className="flex flex-wrap items-center gap-2.5">
        <div className="relative min-w-[220px] flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search by monitor name"
            aria-label="Search by monitor name"
            className="w-full rounded-lg border border-white/10 bg-slate-900/60 py-2 pl-9 pr-8 text-sm text-white placeholder-slate-500"
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

        <select
          className="rd-select max-w-[200px]"
          value={filters.monitorId ?? ''}
          onChange={(e) => set('monitorId', e.target.value || undefined)}
          aria-label="Filter by monitor"
        >
          <option value="">All monitors</option>
          {monitorOptions.map((m) => (
            <option key={m.id} value={m.id}>
              {m.name}
            </option>
          ))}
        </select>

        <select
          className="rd-select"
          value={filters.status}
          onChange={(e) => set('status', e.target.value as IncidentFilters['status'])}
          aria-label="Filter by status"
        >
          <option value="all">All statuses</option>
          <option value="ongoing">Ongoing</option>
          <option value="resolved">Resolved</option>
        </select>

        <select
          className="rd-select"
          value={filters.limit}
          onChange={(e) => set('limit', Number(e.target.value))}
          aria-label="Rows per page"
        >
          {PAGE_SIZES.map((n) => (
            <option key={n} value={n}>
              {n} per page
            </option>
          ))}
        </select>
      </div>

      {error && (
        <div className="flex items-center justify-between rounded-lg border border-red-500/30 bg-red-500/10 p-3">
          <span className="text-sm text-red-400">{error}</span>
          <button className="btn-secondary !py-1" onClick={() => void refetch()}>
            Retry
          </button>
        </div>
      )}

      {loading ? (
        <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-slate-800/40 p-6 text-sm text-slate-400">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading incidents…
        </div>
      ) : incidents.length === 0 ? (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-12 text-center backdrop-blur-sm">
          <p className="text-sm text-slate-300">
            {filters.search || filters.status !== 'all' || filters.monitorId
              ? 'No incidents match these filters.'
              : 'No incidents recorded.'}
          </p>
          {!filters.search && filters.status === 'all' && !filters.monitorId && (
            <p className="mt-1 text-xs text-slate-500">
              An incident is opened when a monitor goes down and closed when it recovers.
            </p>
          )}
        </div>
      ) : (
        <div className="space-y-3">
          {ongoing > 0 && (
            <p className="text-xs text-red-400">
              {ongoing} incident{ongoing === 1 ? '' : 's'} on this page {ongoing === 1 ? 'is' : 'are'}{' '}
              still ongoing.
            </p>
          )}

          <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40 backdrop-blur-sm">
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-white/10 bg-slate-800/20">
                    <th className="px-4 py-3 text-left text-xs font-medium text-slate-400">Monitor</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-slate-400">Status</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-slate-400">Type</th>
                    <th className="px-4 py-3 text-left">{header('started', 'Started')}</th>
                    <th className="px-4 py-3 text-left">{header('duration', 'Duration')}</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-slate-400">Resolved</th>
                    <th className="px-4 py-3 text-right text-xs font-medium text-slate-400">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {incidents.map((inc, i) => (
                    <tr
                      key={inc.id}
                      className={`cursor-pointer transition hover:bg-white/5 ${i % 2 ? 'bg-white/[0.02]' : ''}`}
                      onClick={() => setOpenId(inc.id)}
                    >
                      <td className="px-4 py-3">
                        <div className="font-medium text-slate-200">{inc.monitor_name}</div>
                        <div className="truncate text-xs text-slate-500">{inc.monitor_url}</div>
                      </td>
                      <td className="px-4 py-3">
                        <span
                          className={`inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-medium ${STATUS_STYLE[inc.status]}`}
                        >
                          {inc.status === 'ongoing' ? 'Ongoing' : 'Resolved'}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-slate-400">
                        {TYPE_LABEL[inc.incident_type] ?? inc.incident_type}
                      </td>
                      <td className="whitespace-nowrap px-4 py-3 text-slate-300">
                        {new Date(inc.start_time).toLocaleString()}
                      </td>
                      <td
                        className={`whitespace-nowrap px-4 py-3 font-medium tabular-nums ${
                          inc.status === 'ongoing' ? 'text-red-400' : 'text-slate-300'
                        }`}
                      >
                        {inc.status === 'ongoing'
                          ? `${formatDuration(inc.duration_seconds)} …`
                          : formatDuration(inc.duration_seconds)}
                      </td>
                      <td className="whitespace-nowrap px-4 py-3 text-xs text-slate-500">
                        {inc.end_time ? new Date(inc.end_time).toLocaleString() : '—'}
                      </td>
                      <td className="px-4 py-3" onClick={(e) => e.stopPropagation()}>
                        <div className="flex justify-end">
                          <button
                            className="rounded p-1.5 text-slate-400 transition hover:bg-white/10 hover:text-white"
                            aria-label={`View details for ${inc.monitor_name}`}
                            onClick={() => setOpenId(inc.id)}
                          >
                            <Eye className="h-4 w-4" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
            <span className="text-slate-500">
              {from}–{to} of {total} incident{total === 1 ? '' : 's'}
            </span>
            <div className="flex gap-2">
              <button
                className="btn-secondary !py-1"
                disabled={filters.page <= 1}
                onClick={() => set('page', filters.page - 1)}
              >
                Previous
              </button>
              <span className="px-2 py-1 text-slate-500">
                Page {filters.page} of {pages}
              </span>
              <button
                className="btn-secondary !py-1"
                disabled={filters.page >= pages}
                onClick={() => set('page', filters.page + 1)}
              >
                Next
              </button>
            </div>
          </div>
        </div>
      )}

      <IncidentDetailModal
        incidentId={openId}
        onClose={() => setOpenId(null)}
        onSaved={() => void refetch()}
      />
    </div>
  )
}
