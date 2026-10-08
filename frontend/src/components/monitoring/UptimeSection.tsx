import { useMemo, useState } from 'react'
import GroupSection from '@/components/GroupSection'
import MonitorTable from '@/components/MonitorTable'
import SectionShell from '@/components/monitoring/SectionShell'
import EmptyLine from '@/components/monitoring/EmptyLine'
import { useRememberedToggles } from '@/hooks/useRememberedToggles'
import { MONITOR_SORTS, monitorState, sortMonitors, type MonitorSortKey, type MonitoringFilters } from '@/utils/monitoringView'
import type { Monitor, MonitorGroup } from '@/types'

interface Props {
  /** Rows after every filter. */
  monitors: Monitor[]
  /** Every loaded monitor: the check-type and tag choices, the average. */
  all: Monitor[]
  groups: MonitorGroup[]
  filters: MonitoringFilters
  onFilters: (next: MonitoringFilters) => void
  /** Whether any filter narrows the rows (empty groups are then hidden). */
  narrowed: boolean
  uptimeById: Map<string, number>
  incidents30d: number | null
  usernameFor: (id: string | null | undefined) => string | undefined
  refreshKey: number
  collapsed: boolean
  onToggle: () => void
  loading: boolean
  error: string | null
  onRetry: () => void
  onChanged: () => void
  onAdd: () => void
  onEditGroup: (group: MonitorGroup) => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

const chipCls = (on: boolean) =>
  `rounded-full px-2.5 py-1 text-xs font-medium transition ${
    on ? 'bg-primary-600 text-white' : 'bg-slate-800 text-slate-300 hover:bg-slate-700'
  }`

export default function UptimeSection(p: Props) {
  const [sort, setSort] = useState<MonitorSortKey>('down-first')
  // The same key the Uptime page used, so collapsed groups stay collapsed.
  const groupToggles = useRememberedToggles('sentinel:uptimeCollapsedGroups')

  const sorted = useMemo(() => sortMonitors(p.monitors, sort, p.uptimeById), [p.monitors, sort, p.uptimeById])
  const types = useMemo(() => [...new Set(p.all.map((m) => m.type))].sort(), [p.all])
  const tags = useMemo(() => [...new Set(p.all.flatMap((m) => m.tags ?? []))].sort(), [p.all])
  const avgResponse = useMemo(() => {
    const timed = p.all.filter((m) => m.enabled && m.last_response_time_ms > 0)
    return timed.length ? Math.round(timed.reduce((sum, m) => sum + m.last_response_time_ms, 0) / timed.length) : 0
  }, [p.all])
  const down = p.monitors.filter((m) => monitorState(m) === 'down').length

  const ungrouped = sorted.filter((m) => !m.group_id)
  const byGroup = useMemo(() => {
    const map = new Map<string, Monitor[]>()
    for (const m of sorted) {
      if (!m.group_id) continue
      const list = map.get(m.group_id) ?? []
      list.push(m)
      map.set(m.group_id, list)
    }
    return map
  }, [sorted])

  const toggleTag = (t: string) =>
    p.onFilters({ ...p.filters, tags: p.filters.tags.includes(t) ? p.filters.tags.filter((x) => x !== t) : [...p.filters.tags, t] })

  const table = (rows: Monitor[]) => (
    <MonitorTable
      monitors={rows}
      uptimeById={p.uptimeById}
      usernameFor={p.usernameFor}
      onChanged={p.onChanged}
      push={p.push}
      refreshKey={p.refreshKey}
    />
  )

  return (
    <SectionShell
      title="Uptime checks"
      count={p.monitors.length}
      down={down}
      collapsed={p.collapsed}
      onToggle={p.onToggle}
      loading={p.loading}
      error={p.error}
      onRetry={p.onRetry}
      summary={
        <span>
          {avgResponse > 0 ? `Avg ${avgResponse} ms` : 'No response times yet'} · {p.incidents30d ?? 0} incidents (30 days)
        </span>
      }
      controls={
        <>
          <select
            className="rd-select"
            aria-label="Filter by check type"
            value={p.filters.type ?? ''}
            onChange={(e) => p.onFilters({ ...p.filters, type: e.target.value || null })}
          >
            <option value="">All check types</option>
            {types.map((t) => (
              <option key={t} value={t}>
                {t.toUpperCase()}
              </option>
            ))}
          </select>
          <select className="rd-select" aria-label="Sort monitors" value={sort} onChange={(e) => setSort(e.target.value as MonitorSortKey)}>
            {MONITOR_SORTS.map((s) => (
              <option key={s.key} value={s.key}>
                {s.label}
              </option>
            ))}
          </select>
        </>
      }
    >
      {p.all.length === 0 ? (
        <EmptyLine text="No uptime checks yet." action="Add an uptime monitor" onAction={p.onAdd} />
      ) : (
        <div className="space-y-5">
          {tags.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="vs-eyebrow mr-1">Tags</span>
              {tags.map((t) => (
                <button key={t} className={chipCls(p.filters.tags.includes(t))} aria-pressed={p.filters.tags.includes(t)} onClick={() => toggleTag(t)}>
                  {t}
                </button>
              ))}
            </div>
          )}
          {p.groups.map((g) => {
            const members = byGroup.get(g.id) ?? []
            if (p.narrowed && members.length === 0) return null
            return (
              <GroupSection
                key={g.id}
                title={g.name}
                color={g.color}
                uptime={g.group_uptime}
                count={members.length}
                expanded={!groupToggles.on[g.id]}
                onToggle={() => groupToggles.toggle(g.id)}
                onEdit={() => p.onEditGroup(g)}
              >
                {members.length === 0 ? (
                  <p className="px-2 py-1 text-sm text-slate-400">
                    No monitors in this group yet &mdash; assign one from a row&rsquo;s Group dropdown.
                  </p>
                ) : (
                  table(members)
                )}
              </GroupSection>
            )
          })}
          {ungrouped.length > 0 &&
            (p.groups.length > 0 ? (
              <GroupSection
                title="Ungrouped"
                color={null}
                uptime={null}
                count={ungrouped.length}
                expanded={!groupToggles.on.__ungrouped}
                onToggle={() => groupToggles.toggle('__ungrouped')}
              >
                {table(ungrouped)}
              </GroupSection>
            ) : (
              table(ungrouped)
            ))}
        </div>
      )}
    </SectionShell>
  )
}
