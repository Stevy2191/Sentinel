import { useEffect, useState } from 'react'
import { Search, X } from 'lucide-react'
import { useDebounced } from '@/hooks/useDebounced'
import {
  SECTIONS,
  SECTION_CHIP,
  VIEW_STATUS_LABEL,
  type MonitoringFilters,
  type Section,
  type ViewStatus,
} from '@/utils/monitoringView'

interface Props {
  filters: MonitoringFilters
  /** replace: change the address without a new history entry (typing). */
  onChange: (next: MonitoringFilters, replace?: boolean) => void
  counts: Record<Section, number>
  statuses: ViewStatus[]
  sites: { id: string; name: string }[]
  groups: { id: string; name: string }[]
}

const chipCls = (on: boolean) =>
  `rounded-full px-3 py-1 text-xs font-medium transition ${
    on ? 'bg-primary-600 text-white' : 'bg-slate-800 text-slate-300 hover:bg-slate-700'
  }`

/** The filters every section shares: search, status, site, group and the
 *  type chips. */
export default function MonitoringToolbar({ filters, onChange, counts, statuses, sites, groups }: Props) {
  const [text, setText] = useState(filters.q)
  const debounced = useDebounced(text, 300)

  // Follow the address when it changes from outside (Back, Clear filters).
  useEffect(() => {
    setText(filters.q)
  }, [filters.q])

  useEffect(() => {
    if (debounced !== filters.q) onChange({ ...filters, q: debounced }, true)
    // Runs on the debounced text only: filters and onChange change on every
    // address change, and re-running then would undo Back.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debounced])

  const total = counts.uptime + counts.servers + counts.devices

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2.5">
        <div className="relative min-w-[200px] flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
          <input
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="Search by name or address"
            aria-label="Search by name or address"
            className="rd-input py-2 pl-9 pr-8"
          />
          {text && (
            <button
              onClick={() => setText('')}
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-slate-500 transition hover:text-white"
              aria-label="Clear search"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
        <select
          className="rd-select"
          aria-label="Filter by status"
          value={filters.status ?? ''}
          onChange={(e) => onChange({ ...filters, status: (e.target.value || null) as ViewStatus | null })}
        >
          <option value="">All statuses</option>
          {statuses.map((s) => (
            <option key={s} value={s}>
              {VIEW_STATUS_LABEL[s]}
            </option>
          ))}
        </select>
        <select
          className="rd-select"
          aria-label="Filter by site"
          value={filters.site ?? ''}
          onChange={(e) => onChange({ ...filters, site: e.target.value || null })}
        >
          <option value="">All sites</option>
          <option value="none">No site</option>
          {sites.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
        {groups.length > 0 && (
          <select
            className="rd-select"
            aria-label="Filter by group"
            value={filters.group ?? ''}
            onChange={(e) => onChange({ ...filters, group: e.target.value || null })}
          >
            <option value="">All groups</option>
            <option value="ungrouped">Ungrouped</option>
            {groups.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </select>
        )}
      </div>
      <div className="flex flex-wrap gap-1.5" role="group" aria-label="Show">
        <button className={chipCls(filters.show === null)} aria-pressed={filters.show === null} onClick={() => onChange({ ...filters, show: null })}>
          All {total}
        </button>
        {SECTIONS.map((s) => (
          <button key={s} className={chipCls(filters.show === s)} aria-pressed={filters.show === s} onClick={() => onChange({ ...filters, show: s })}>
            {SECTION_CHIP[s]} {counts[s]}
          </button>
        ))}
      </div>
    </div>
  )
}
