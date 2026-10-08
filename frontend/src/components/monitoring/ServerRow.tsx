import { ChevronRight, Terminal, Trash2 } from 'lucide-react'
import { useAgentStatus, type Agent } from '@/hooks/useAgents'

const STATUS_STYLE: Record<string, { label: string; cls: string; dot: string }> = {
  active: {
    label: 'Active',
    cls: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-400',
    dot: 'bg-emerald-400',
  },
  offline: {
    label: 'Offline',
    cls: 'border-red-500/30 bg-red-500/10 text-red-400',
    dot: 'bg-red-400',
  },
  pending: {
    label: 'Awaiting first report',
    cls: 'border-amber-500/30 bg-amber-500/10 text-amber-300',
    dot: 'bg-amber-300',
  },
}

/** Colours a utilisation figure by how much headroom is left. */
function usageClass(pct: number | null | undefined): string {
  if (pct == null) return 'text-slate-500'
  if (pct >= 90) return 'text-red-400'
  if (pct >= 75) return 'text-amber-400'
  return 'text-slate-200'
}

function pct(v: number | null | undefined): string {
  return v == null ? '—' : `${v.toFixed(1)}%`
}

function relative(iso: string | null): string {
  if (!iso) return 'never'
  const secs = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000)
  if (secs < 60) return `${Math.floor(secs)}s ago`
  if (secs < 3600) return `${Math.floor(secs / 60)}m ago`
  if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`
  return `${Math.floor(secs / 86400)}d ago`
}

function uptime(seconds: number | null | undefined): string {
  if (!seconds) return '—'
  const days = Math.floor(seconds / 86400)
  if (days > 0) return `${days}d ${Math.floor((seconds % 86400) / 3600)}h`
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
}

/** A slim bar for a percentage, so a row reads at a glance. */
function UsageBar({ value }: { value: number | null | undefined }) {
  const v = Math.max(0, Math.min(100, value ?? 0))
  const colour = v >= 90 ? 'bg-red-500' : v >= 75 ? 'bg-amber-500' : 'bg-emerald-500'
  return (
    <div className="mt-1 h-1 w-full overflow-hidden rounded-full bg-white/10">
      <div className={`h-full rounded-full ${colour}`} style={{ width: `${v}%` }} />
    </div>
  )
}

export default function AgentRow({
  agent,
  striped,
  onOpen,
  isAdmin,
  onInstructions,
  onDelete,
}: {
  agent: Agent
  striped: boolean
  onOpen: () => void
  isAdmin: boolean
  onInstructions: () => void
  onDelete: () => void
}) {
  const { detail } = useAgentStatus(agent.agent_id, 20000)
  const m = detail?.latest
  const style = STATUS_STYLE[agent.status] ?? STATUS_STYLE.pending

  return (
    <tr
      className={`cursor-pointer transition hover:bg-white/5 ${striped ? 'bg-white/[0.02]' : ''}`}
      onClick={onOpen}
    >
      <td className="px-4 py-3">
        <div className="flex items-center gap-1 font-medium text-slate-200">
          {agent.name}
          <ChevronRight className="h-3.5 w-3.5 text-slate-600" aria-hidden />
        </div>
        <div className="text-xs text-slate-500">
          {agent.hostname ?? agent.agent_id}
          {agent.os_version ? ` · ${agent.os_version}` : ''}
        </div>
      </td>
      <td className="px-4 py-3 text-sm text-slate-400">{agent.site_name ?? '—'}</td>
      <td className="px-4 py-3">
        <span className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium ${style.cls}`}>
          <span className={`h-1.5 w-1.5 rounded-full ${style.dot}`} />
          {style.label}
        </span>
      </td>
      <td className="w-28 px-4 py-3">
        <span className={`tabular-nums ${usageClass(m?.cpu_percent)}`}>{pct(m?.cpu_percent)}</span>
        <UsageBar value={m?.cpu_percent} />
      </td>
      <td className="w-32 px-4 py-3">
        <span className={`tabular-nums ${usageClass(m?.memory_percent)}`}>{pct(m?.memory_percent)}</span>
        {m?.memory_total_mb ? (
          <div className="text-xs text-slate-500">
            {Math.round((m.memory_used_mb ?? 0) / 1024)} / {Math.round(m.memory_total_mb / 1024)} GB
          </div>
        ) : null}
      </td>
      <td className="w-32 px-4 py-3">
        <span className={`tabular-nums ${usageClass(m?.disk_percent)}`}>{pct(m?.disk_percent)}</span>
        {m?.disk_total_gb ? (
          <div className="text-xs text-slate-500">
            {Math.round(m.disk_used_gb ?? 0)} / {Math.round(m.disk_total_gb)} GB
          </div>
        ) : null}
      </td>
      <td className="whitespace-nowrap px-4 py-3 text-slate-400">{uptime(m?.uptime_seconds)}</td>
      <td className="whitespace-nowrap px-4 py-3 text-slate-500">{relative(agent.last_heartbeat)}</td>
      <td className="px-4 py-3" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-end gap-1">
          {isAdmin && (
            <>
              <button
                className="rounded p-1.5 text-slate-400 transition hover:bg-white/10 hover:text-white"
                aria-label={`Install instructions for ${agent.name}`}
                title="Install instructions"
                onClick={onInstructions}
              >
                <Terminal className="h-4 w-4" />
              </button>
              <button
                className="rounded p-1.5 text-red-400 transition hover:bg-red-500/10 hover:text-red-300"
                aria-label={`Unregister ${agent.name}`}
                title="Unregister"
                onClick={onDelete}
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </>
          )}
        </div>
      </td>
    </tr>
  )
}
