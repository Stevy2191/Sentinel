import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Plus, Trash2, Terminal, Loader2, ChevronRight } from 'lucide-react'
import { useToasts, Toaster } from '@/components/Toast'
import { useCardShimmer } from '@/hooks/useCardShimmer'
import ShimmerStatCard from '@/components/ShimmerStatCard'
import AddServerAgentModal from '@/components/AddServerAgentModal'
import {
  useAgents,
  useAgentStatus,
  useAgentActions,
  type Agent,
  type CreatedAgent,
} from '@/hooks/useAgents'
import { useAuthContext } from '@/context/AuthContext'
import type { ApiError } from '@/services/api'

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

export default function ServerMonitoring() {
  const { toasts, push } = useToasts()
  const shimmer = useCardShimmer(['srvTotal', 'srvActive', 'srvOffline', 'srvPending'])
  const { currentUser } = useAuthContext()
  const isAdmin = !!currentUser?.is_admin

  const navigate = useNavigate()
  const { agents, loading, error, refetch } = useAgents()
  const { getWithToken, remove, busy } = useAgentActions()

  const [addOpen, setAddOpen] = useState(false)
  const [instructionsFor, setInstructionsFor] = useState<CreatedAgent | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<Agent | null>(null)


  const counts = useMemo(() => {
    let active = 0
    let offline = 0
    let pending = 0
    for (const a of agents) {
      if (a.status === 'active') active++
      else if (a.status === 'offline') offline++
      else pending++
    }
    return { active, offline, pending, total: agents.length }
  }, [agents])

  const showInstructions = async (agent: Agent) => {
    try {
      // Re-fetched rather than held in memory: the token is not in the list
      // payload, and the install command is meaningless without it.
      const full = await getWithToken(agent.agent_id)
      setInstructionsFor(full)
      setAddOpen(true)
    } catch (err) {
      push((err as ApiError).message || 'Could not load install instructions', 'error')
    }
  }

  const doDelete = async () => {
    if (!confirmDelete) return
    try {
      await remove(confirmDelete.agent_id)
      push(`${confirmDelete.name} unregistered`, 'success')
      setConfirmDelete(null)
      await refetch()
    } catch (err) {
      push((err as ApiError).message || 'Could not unregister the agent', 'error')
    }
  }

  return (
    <div className="space-y-6">
      <Toaster toasts={toasts} />

      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-3xl font-light text-white">Server Monitoring</h1>
          <p className="mt-2 text-sm text-slate-400">
            Hosts reporting system and container metrics through an agent
          </p>
        </div>
        {isAdmin && (
          <button
            className="btn-primary"
            onClick={() => {
              setInstructionsFor(null)
              setAddOpen(true)
            }}
          >
            <Plus className="h-4 w-4" /> Add Server Agent
          </button>
        )}
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <ShimmerStatCard
          title="Servers"
          value={counts.total}
          subtitle="registered"
          colorType="agents"
          onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'srvTotal')}
          onMouseEnter={() => shimmer.handleCardMouseEnter('srvTotal')}
          onMouseLeave={() => shimmer.handleCardMouseLeave('srvTotal')}
          showShimmer={shimmer.isShown('srvTotal')}
          shimmerStyle={shimmer.getShimmerStyle('srvTotal')}
        />
        <ShimmerStatCard
          title="Active"
          value={counts.active}
          subtitle="reporting"
          colorType="monitoring"
          onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'srvActive')}
          onMouseEnter={() => shimmer.handleCardMouseEnter('srvActive')}
          onMouseLeave={() => shimmer.handleCardMouseLeave('srvActive')}
          showShimmer={shimmer.isShown('srvActive')}
          shimmerStyle={shimmer.getShimmerStyle('srvActive')}
        />
        <ShimmerStatCard
          title="Offline"
          value={counts.offline}
          subtitle="stopped reporting"
          colorType="incidents"
          onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'srvOffline')}
          onMouseEnter={() => shimmer.handleCardMouseEnter('srvOffline')}
          onMouseLeave={() => shimmer.handleCardMouseLeave('srvOffline')}
          showShimmer={shimmer.isShown('srvOffline')}
          shimmerStyle={shimmer.getShimmerStyle('srvOffline')}
        />
        <ShimmerStatCard
          title="Awaiting Install"
          value={counts.pending}
          subtitle="never reported"
          colorType="responseTime"
          onMouseMove={(e) => shimmer.handleCardMouseMove(e, 'srvPending')}
          onMouseEnter={() => shimmer.handleCardMouseEnter('srvPending')}
          onMouseLeave={() => shimmer.handleCardMouseLeave('srvPending')}
          showShimmer={shimmer.isShown('srvPending')}
          shimmerStyle={shimmer.getShimmerStyle('srvPending')}
        />
      </div>

      {error && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">
          {error}
        </div>
      )}

      {loading ? (
        <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-slate-800/40 p-10 text-sm text-slate-400">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading agents…
        </div>
      ) : agents.length === 0 ? (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-10 text-center backdrop-blur-sm">
          <p className="text-sm text-slate-300">No servers are being monitored yet.</p>
          <p className="mx-auto mt-2 max-w-md text-xs text-slate-500">
            Register a server here, then run the install command it gives you on that host. The
            agent reports CPU, memory, disk, network and any Docker containers it finds.
          </p>
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40 backdrop-blur-sm">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-white/10 bg-slate-800/20 text-xs font-medium text-slate-400">
                  <th className="px-4 py-3 text-left">Server</th>
                  <th className="px-4 py-3 text-left">Status</th>
                  <th className="px-4 py-3 text-left">CPU</th>
                  <th className="px-4 py-3 text-left">Memory</th>
                  <th className="px-4 py-3 text-left">Disk</th>
                  <th className="px-4 py-3 text-left">Uptime</th>
                  <th className="px-4 py-3 text-left">Last Report</th>
                  <th className="px-4 py-3 text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {agents.map((a, i) => (
                  <AgentRow
                    key={a.id}
                    agent={a}
                    striped={i % 2 === 1}
                    onOpen={() => navigate(`/servers/${a.agent_id}`)}
                    isAdmin={isAdmin}
                    onInstructions={() => void showInstructions(a)}
                    onDelete={() => setConfirmDelete(a)}
                  />
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}


      <AddServerAgentModal
        isOpen={addOpen}
        existing={instructionsFor}
        onClose={() => {
          setAddOpen(false)
          setInstructionsFor(null)
        }}
        onCreated={() => void refetch()}
        push={push}
      />

      {confirmDelete && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
          onMouseDown={(e) => e.target === e.currentTarget && setConfirmDelete(null)}
        >
          <div role="dialog" aria-modal="true" className="w-full max-w-md rounded-xl border border-white/10 bg-slate-900/95 p-6">
            <h3 className="text-lg font-semibold text-white">Unregister {confirmDelete.name}?</h3>
            <p className="mt-2 text-sm text-slate-400">
              Its metric history is deleted with it. The agent on that host will keep running and
              start failing to report until it is stopped and removed.
            </p>
            <div className="mt-6 flex justify-end gap-2">
              <button className="btn-secondary" onClick={() => setConfirmDelete(null)} disabled={busy}>
                Cancel
              </button>
              <button
                className="rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-red-500 disabled:opacity-50"
                onClick={() => void doDelete()}
                disabled={busy}
              >
                {busy ? 'Removing…' : 'Unregister'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function AgentRow({
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


