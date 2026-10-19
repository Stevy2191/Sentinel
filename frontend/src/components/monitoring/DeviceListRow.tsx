import { Pencil } from 'lucide-react'
import ListRowLayout, { ago } from '@/components/monitoring/ListRowLayout'
import StatusPill from '@/components/monitoring/StatusPill'
import { upFor } from '@/components/monitoring/ServerListRow'
import { kindLabel, type ListRow } from '@/utils/monitoringList'

interface Props {
  row: Extract<ListRow, { kind: 'devices' }>
  isAdmin: boolean
  onEdit: () => void
}

/** A network device on the Monitoring list: how long it has been up as its
 *  health (or why it is not answering), and its 30-day availability. */
export default function DeviceListRow({ row, isAdmin, onEdit }: Props) {
  const d = row.device
  const failing = d.status === 'down' || d.status === 'error'
  const healthText = failing ? d.status_detail || 'not responding' : upFor(d.sys_uptime_seconds)
  const avail = d.availability_30d

  return (
    <ListRowLayout
      kind="devices"
      name={<span className="truncate">{d.name}</span>}
      line={row.line}
      kindText={kindLabel(row)}
      site={row.siteName}
      status={<StatusPill status={row.status} detail={d.status_detail} />}
      health={
        <span className={`block max-w-[14rem] truncate ${failing ? 'text-red-300' : 'text-slate-300'}`} title={healthText}>
          {healthText}
        </span>
      }
      healthText={healthText}
      uptime={
        avail == null ? (
          <span className="text-slate-500">—</span>
        ) : (
          <div className="w-28" title="Availability over the last 30 days">
            <div className="whitespace-nowrap text-xs font-medium tabular-nums text-slate-200">
              {avail.toFixed(2)}% <span className="font-normal text-slate-500">30 d</span>
            </div>
            <div className="mt-1 h-1 w-full overflow-hidden rounded-full bg-white/10">
              <div
                className={`h-full rounded-full ${avail >= 99.9 ? 'bg-emerald-500' : avail >= 99 ? 'bg-amber-500' : 'bg-red-500'}`}
                style={{ width: `${Math.max(0, Math.min(100, avail))}%` }}
              />
            </div>
          </div>
        )
      }
      seen={ago(d.last_seen_at)}
      actions={
        isAdmin && (
          <button
            className="rounded p-1.5 text-slate-400 transition hover:bg-white/10 hover:text-white"
            aria-label={`Edit ${d.name}`}
            title="Edit"
            onClick={onEdit}
          >
            <Pencil className="h-4 w-4" />
          </button>
        )
      }
      to={`/network/devices/${d.id}`}
    />
  )
}
