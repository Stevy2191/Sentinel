import { useEffect, useMemo, useState } from 'react'
import { Search, X } from 'lucide-react'
import { useDebounced } from '@/hooks/useDebounced'
import FilterMenu, { type FilterOption } from '@/components/monitoring/FilterMenu'
import { VIEW_STATUS_LABEL, type ViewStatus } from '@/utils/monitoringView'
import {
  CHECK_LEAVES,
  DEVICE_LEAVES,
  KIND_PARENT_LABEL,
  NO_FILTERS,
  facetCounts,
  filterPills,
  kindValueLabel,
  uptimeOnly,
  type GroupBy,
  type KindValue,
  type ListFilters,
  type ListRow,
} from '@/utils/monitoringList'
import type { MonitorGroup } from '@/types'

interface Props {
  /** Every loaded row, before filtering: the counts and the tag choices. */
  rows: ListRow[]
  filters: ListFilters
  /** replace: change the address without a new history entry (typing). */
  onChange: (next: ListFilters, replace?: boolean) => void
  sites: { id: string; name: string }[]
  groups: MonitorGroup[]
  by: GroupBy
  onBy: (by: GroupBy) => void
}

const GROUP_BY: { value: GroupBy; label: string }[] = [
  { value: 'none', label: 'None' },
  { value: 'site', label: 'Site' },
  { value: 'kind', label: 'Kind' },
  { value: 'group', label: 'Monitor group' },
]

/** The Monitoring list's filters: search, Kind, Status, Site, Monitor group,
 *  Tags and Group by, with the chosen values as removable pills below. */
export default function ListFilterBar({ rows, filters, onChange, sites, groups, by, onBy }: Props) {
  const [text, setText] = useState(filters.q)
  const debounced = useDebounced(text, 300)

  // Follow the address when it changes from outside (Back, Clear all).
  useEffect(() => {
    setText(filters.q)
  }, [filters.q])

  useEffect(() => {
    if (debounced !== filters.q) onChange({ ...filters, q: debounced }, true)
    // Runs on the debounced text only: filters and onChange change on every
    // address change, and re-running then would undo Back.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debounced])

  const counts = useMemo(() => facetCounts(rows, filters), [rows, filters])

  const kindOptions: FilterOption[] = [
    { value: 'uptime', label: KIND_PARENT_LABEL.uptime, n: counts.kind.uptime },
    ...CHECK_LEAVES.map((k) => ({ value: k, label: kindValueLabel(k), n: counts.kind[k], child: true })),
    { value: 'servers', label: KIND_PARENT_LABEL.servers, n: counts.kind.servers, separatorBefore: true },
    { value: 'devices', label: KIND_PARENT_LABEL.devices, n: counts.kind.devices, separatorBefore: true },
    ...DEVICE_LEAVES.map((k) => ({ value: k, label: kindValueLabel(k), n: counts.kind[k], child: true })),
  ]
  const statusOptions: FilterOption[] = (Object.keys(VIEW_STATUS_LABEL) as ViewStatus[]).map((s) => ({
    value: s,
    label: VIEW_STATUS_LABEL[s],
    n: counts.status[s] ?? 0,
  }))
  const siteName = new Map(sites.map((s) => [s.id, s.name]))
  const siteOptions: FilterOption[] = [
    ...sites.map((s) => ({ value: s.id, label: s.name, n: counts.site[s.id] ?? 0 })),
    { value: 'none', label: 'No site', n: counts.site.none ?? 0 },
    // A bookmarked site that no longer exists stays listed so it can be unticked.
    ...filters.sites.filter((s) => s !== 'none' && !siteName.has(s)).map((s) => ({ value: s, label: 'Unknown site', n: 0 })),
  ]
  const groupName = new Map(groups.map((g) => [g.id, g.name]))
  const groupOptions: FilterOption[] = [
    ...groups.map((g) => ({ value: g.id, label: g.name, n: counts.group[g.id] ?? 0 })),
    { value: 'ungrouped', label: 'Ungrouped', n: counts.group.ungrouped ?? 0 },
    ...(filters.group && filters.group !== 'ungrouped' && !groupName.has(filters.group)
      ? [{ value: filters.group, label: 'Unknown group', n: 0 }]
      : []),
  ]
  // Selected tags stay listed even when no check has them, so each can be
  // switched off.
  const tags = useMemo(
    () => [...new Set([...rows.flatMap((r) => r.tags), ...filters.tags])].sort((a, b) => a.localeCompare(b)),
    [rows, filters.tags]
  )
  const tagOptions: FilterOption[] = tags.map((t) => ({ value: t, label: t, n: counts.tags[t] ?? 0 }))

  const pills = filterPills(filters, { site: (id) => siteName.get(id) ?? null, group: (id) => groupName.get(id) ?? null })

  return (
    <div className="space-y-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-[200px] flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
          <input
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="Search by name, address, site or kind"
            aria-label="Search"
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
        <FilterMenu
          label="Kind"
          multi
          options={kindOptions}
          selected={filters.kinds}
          onChange={(v) => onChange({ ...filters, kinds: v as KindValue[] })}
        />
        <FilterMenu
          label="Status"
          multi
          options={statusOptions}
          selected={filters.statuses}
          onChange={(v) => onChange({ ...filters, statuses: v as ViewStatus[] })}
        />
        <FilterMenu label="Site" multi options={siteOptions} selected={filters.sites} onChange={(v) => onChange({ ...filters, sites: v })} />
        {(groups.length > 0 || filters.group) && (
          <FilterMenu
            label="Monitor group"
            multi={false}
            hint="Uptime checks only"
            options={groupOptions}
            selected={filters.group ? [filters.group] : []}
            onChange={(v) => onChange({ ...filters, group: v[0] ?? null })}
          />
        )}
        {tags.length > 0 && (
          <FilterMenu
            label="Tags"
            multi
            hint="Uptime checks only"
            options={tagOptions}
            selected={filters.tags}
            onChange={(v) => onChange({ ...filters, tags: v })}
          />
        )}
        <label className="flex items-center gap-2 text-sm text-slate-400">
          Group by
          <select className="rd-select" aria-label="Group by" value={by} onChange={(e) => onBy(e.target.value as GroupBy)}>
            {GROUP_BY.map((g) => (
              <option key={g.value} value={g.value}>
                {g.label}
              </option>
            ))}
          </select>
        </label>
      </div>
      {pills.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5" aria-label="Active filters">
          {pills.map((p) => (
            <span
              key={p.id}
              className="inline-flex items-center gap-1 rounded-full border border-white/10 bg-slate-800 py-0.5 pl-2.5 pr-1 text-xs text-slate-200"
            >
              {p.label}
              <button
                className="rounded-full p-0.5 text-slate-400 transition hover:bg-white/10 hover:text-white"
                aria-label={`Remove ${p.label}`}
                onClick={() => onChange(p.remove(filters))}
              >
                <X className="h-3 w-3" />
              </button>
            </span>
          ))}
          {uptimeOnly(filters) && (
            <span className="rounded-full border border-primary-500/30 bg-primary-500/10 px-2.5 py-0.5 text-xs text-primary-300">
              uptime checks only — group and tag filters apply to them alone
            </span>
          )}
          <button className="ml-1 text-xs text-slate-400 underline-offset-2 hover:text-white hover:underline" onClick={() => onChange(NO_FILTERS)}>
            Clear all
          </button>
        </div>
      )}
    </div>
  )
}
