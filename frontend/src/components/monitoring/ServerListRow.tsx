import { useNavigate } from 'react-router-dom'
import { Terminal, Trash2 } from 'lucide-react'
import type { AgentMetric } from '@/hooks/useAgents'
import ListRowLayout, { ago } from '@/components/monitoring/ListRowLayout'
import StatusPill from '@/components/monitoring/StatusPill'
import type { ListRow } from '@/utils/monitoringList'

/** Colours a utilisation figure by how much headroom is left. */
function usageClass(pct: number | null | undefined): string {
  if (pct == null) return 'text-slate-500'
  if (pct >= 90) return 'text-red-400'
  if (pct >= 75) return 'text-amber-400'
  return 'text-slate-200'
}

const pct = (v: number | null | undefined) => (v == null ? '—' : `${Math.round(v)}%`)

/** upFor is a running time as "up 12 d", or "up 5 h" under a day. */
// eslint-disable-next-line react-refresh/only-export-components
export function upFor(seconds: number | null | undefined): string {
  if (!seconds) return '—'
  const days = Math.floor(seconds / 86400)
  if (days > 0) return `up ${days} d`
  return `up ${Math.max(1, Math.floor(seconds / 3600))} h`
}

interface Props {
  row: Extract<ListRow, { kind: 'servers' }>
  /** Its latest metrics, polled by the page; undefined until loaded. */
  latest: AgentMetric | null | undefined
  isAdmin: boolean
  onInstructions: () => void
  onDelete: () => void
}

/** A server agent on the Monitoring list: CPU, memory and disk as its health,
 *  how long the host has been up. */
export default function ServerListRow({ row, latest, isAdmin, onInstructions, onDelete }: Props) {
  const navigate = useNavigate()
  const a = row.agent
  const m = latest
  const parts: [string, number | null | undefined][] = [
    ['CPU', m?.cpu_percent],
    ['Mem', m?.memory_percent],
    ['Disk', m?.disk_percent],
  ]
  const healthText = m ? parts.map(([l, v]) => `${l} ${pct(v)}`).join(' · ') : '—'

  return (
    <ListRowLayout
      kind="servers"
      name={<span className="truncate">{a.name}</span>}
      line={row.line}
      kindText="Server"
      site={row.siteName}
      status={<StatusPill status={row.status} detail={a.status === 'pending' ? 'Awaiting first report' : undefined} />}
      health={
        m ? (
          <span className="whitespace-nowrap tabular-nums">
            {parts.map(([l, v], i) => (
              <span key={l}>
                {i > 0 && <span className="text-slate-600"> · </span>}
                <span className={usageClass(v)}>
                  {l} {pct(v)}
                </span>
              </span>
            ))}
          </span>
        ) : (
          <span className="text-slate-500">—</span>
        )
      }
      healthText={healthText}
      uptime={<span className="whitespace-nowrap text-slate-300">{upFor(m?.uptime_seconds)}</span>}
      seen={ago(a.last_heartbeat)}
      actions={
        isAdmin && (
          <div className="flex items-center gap-1">
            <button
              className="rounded p-1.5 text-slate-400 transition hover:bg-white/10 hover:text-white"
              aria-label={`Install instructions for ${a.name}`}
              title="Install instructions"
              onClick={onInstructions}
            >
              <Terminal className="h-4 w-4" />
            </button>
            <button
              className="rounded p-1.5 text-red-400 transition hover:bg-red-500/10 hover:text-red-300"
              aria-label={`Unregister ${a.name}`}
              title="Unregister"
              onClick={onDelete}
            >
              <Trash2 className="h-4 w-4" />
            </button>
          </div>
        )
      }
      onOpen={() => navigate(`/servers/${a.agent_id}`)}
    />
  )
}
