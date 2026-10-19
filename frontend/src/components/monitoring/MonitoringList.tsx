import { ChevronDown, ChevronRight, Pencil } from 'lucide-react'
import { useRememberedToggles } from '@/hooks/useRememberedToggles'
import { uptimeColor } from '@/components/UptimeSparkline'
import MonitorListRow from '@/components/monitoring/MonitorListRow'
import ServerListRow from '@/components/monitoring/ServerListRow'
import DeviceListRow from '@/components/monitoring/DeviceListRow'
import type { GroupBy, ListRow, ListSort, RowGroup, SortKey } from '@/utils/monitoringList'
import type { Agent } from '@/hooks/useAgents'
import type { Device } from '@/hooks/useDevices'
import type { MonitorGroup } from '@/types'

interface Props {
  /** Already filtered and sorted. */
  rows: ListRow[]
  /** Headings to split the rows under, or null for one flat list. */
  groups: RowGroup[] | null
  by: GroupBy
  sort: ListSort
  onSort: (key: SortKey) => void
  isAdmin: boolean
  uptimeById: Map<string, number>
  usernameFor: (id: string | null | undefined) => string | undefined
  refreshKey: unknown
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
  onMonitorsChanged: () => void
  onServerInstructions: (a: Agent) => void
  onServerDelete: (a: Agent) => void
  onDeviceEdit: (d: Device) => void
  onEditGroup: (g: MonitorGroup) => void
}

const COLUMNS: { label: string; key?: SortKey }[] = [
  { label: 'Name', key: 'name' },
  { label: 'Kind', key: 'kind' },
  { label: 'Site', key: 'site' },
  { label: 'Status', key: 'status' },
  { label: 'Health' },
  { label: 'Uptime' },
  { label: 'Last seen', key: 'seen' },
  { label: '' },
]

/** Everything Sentinel watches in one table: uptime checks, servers and
 *  devices side by side, optionally under collapsible headings. */
export default function MonitoringList(p: Props) {
  // Collapsed headings, per way of grouping, kept in this browser.
  const collapsed = useRememberedToggles('sentinel:monitoringListGroups')

  const row = (r: ListRow) => {
    if (r.kind === 'uptime') {
      return (
        <MonitorListRow
          key={r.key}
          row={r}
          uptime24h={p.uptimeById.get(r.monitor.id) ?? null}
          ownerUsername={p.usernameFor(r.monitor.owner_id)}
          refreshKey={p.refreshKey}
          onChanged={p.onMonitorsChanged}
          push={p.push}
        />
      )
    }
    if (r.kind === 'servers') {
      return (
        <ServerListRow
          key={r.key}
          row={r}
          isAdmin={p.isAdmin}
          onInstructions={() => p.onServerInstructions(r.agent)}
          onDelete={() => p.onServerDelete(r.agent)}
        />
      )
    }
    return <DeviceListRow key={r.key} row={r} isAdmin={p.isAdmin} onEdit={() => p.onDeviceEdit(r.device)} />
  }

  const heading = (g: RowGroup) => {
    const id = `${p.by}:${g.key}`
    const shut = !!collapsed.on[id]
    const mg = g.monitorGroup
    return (
      <tr key={'h:' + g.key} className="bg-slate-900/70" data-group-heading={g.label}>
        <td colSpan={9} className="border-l-4 px-3 py-2" style={{ borderLeftColor: mg?.color ?? 'transparent' }}>
          <div className="flex items-center gap-3">
            <button className="flex min-w-0 flex-1 items-center gap-2 text-left" onClick={() => collapsed.toggle(id)} aria-expanded={!shut}>
              {shut ? <ChevronRight className="h-4 w-4 shrink-0 text-slate-400" /> : <ChevronDown className="h-4 w-4 shrink-0 text-slate-400" />}
              <span className="truncate text-sm font-medium text-white">{g.label}</span>
              <span className="shrink-0 rounded-full bg-white/5 px-2 py-0.5 text-xs tabular-nums text-slate-300">{g.rows.length}</span>
              {g.down > 0 && (
                <span className="shrink-0 rounded-full border border-red-500/30 bg-red-500/10 px-2 py-0.5 text-xs font-medium text-red-400">
                  {g.down} down
                </span>
              )}
            </button>
            {mg && (
              <>
                <span className={`shrink-0 text-sm font-medium tabular-nums ${uptimeColor(mg.group_uptime)}`}>{mg.group_uptime.toFixed(1)}%</span>
                <button
                  onClick={() => p.onEditGroup(mg)}
                  className="shrink-0 rounded p-1 text-slate-400 hover:bg-white/10 hover:text-white"
                  title="Edit group"
                  aria-label={`Edit group ${mg.name}`}
                >
                  <Pencil className="h-4 w-4" />
                </button>
              </>
            )}
          </div>
        </td>
      </tr>
    )
  }

  const body = p.groups
    ? p.groups.flatMap((g) => {
        const shut = !!collapsed.on[`${p.by}:${g.key}`]
        if (shut) return [heading(g)]
        if (g.rows.length === 0) {
          return [
            heading(g),
            <tr key={'e:' + g.key}>
              <td colSpan={9} className="px-4 py-2 text-sm text-slate-400">
                No monitors in this group yet &mdash; assign one from a row&rsquo;s Group dropdown.
              </td>
            </tr>,
          ]
        }
        return [heading(g), ...g.rows.map(row)]
      })
    : p.rows.map(row)

  return (
    <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40">
      {/* One box scrolls both ways so the sticky header stays pinned (the
          same arrangement as the device and port tables). */}
      <div className="max-h-[65vh] overflow-auto">
        {/* Fixed layout on phones, so the one card cell takes the box width and
            long lines truncate instead of pushing the status off screen. */}
        <table className="w-full table-fixed text-sm md:table-auto">
          <thead className="sticky top-0 z-10 hidden bg-slate-900 md:table-header-group">
            <tr className="border-b border-white/10 text-left text-xs text-slate-400">
              {COLUMNS.map((c, i) => {
                const on = c.key !== undefined && p.sort.key === c.key
                return (
                  <th
                    key={i}
                    className="whitespace-nowrap px-4 py-3 font-medium"
                    aria-sort={on ? (p.sort.desc ? 'descending' : 'ascending') : c.key ? 'none' : undefined}
                  >
                    {c.key ? (
                      <button
                        type="button"
                        className="hover:text-slate-200"
                        title={c.key === 'status' ? 'Down first' : undefined}
                        onClick={() => p.onSort(c.key!)}
                      >
                        {c.label}
                        {on ? (p.sort.desc ? ' ↓' : ' ↑') : ''}
                      </button>
                    ) : (
                      c.label
                    )}
                  </th>
                )
              })}
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5">{body}</tbody>
        </table>
      </div>
    </div>
  )
}
