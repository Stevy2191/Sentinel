import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, Server, Loader2, Terminal, Trash2 } from 'lucide-react'
import { useToasts, Toaster } from '@/components/Toast'
import ServerStatusCards from '@/components/ServerStatusCards'
import HistoricalPerformance, { type RangeValue } from '@/components/HistoricalPerformance'
import AddServerAgentModal from '@/components/AddServerAgentModal'
import {
  useAgentStatus,
  useAgentMetrics,
  useAgentActions,
  type CreatedAgent,
} from '@/hooks/useAgents'
import { useAuthContext } from '@/context/AuthContext'
import { useAppConfig } from '@/context/AppConfigContext'
import type { ApiError } from '@/services/api'

/** Label and value, the shape every row in the sidebar takes. */
function InfoRow({ label, value }: { label: string; value: string | null | undefined }) {
  return (
    <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-0.5 border-b border-white/5 py-2 last:border-0">
      <dt className="text-xs text-slate-500">{label}</dt>
      <dd className="break-all text-right font-mono text-xs text-slate-200">{value || '—'}</dd>
    </div>
  )
}

export default function ServerDetail() {
  const { agentID = '' } = useParams()
  const navigate = useNavigate()
  const { toasts, push } = useToasts()
  const { currentUser } = useAuthContext()
  const { appName } = useAppConfig()
  const isAdmin = !!currentUser?.is_admin

  const { detail, loading, error } = useAgentStatus(agentID)
  const [range, setRange] = useState<RangeValue>('1h')
  const { metrics, loading: metricsLoading } = useAgentMetrics(agentID, range)
  const { getWithToken, remove, busy } = useAgentActions()

  const [instructions, setInstructions] = useState<CreatedAgent | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)

  const agent = detail?.agent
  const latest = detail?.latest ?? null

  // The tab is how someone finds this page again among several open servers,
  // so it carries the server's name rather than the app's alone.
  useEffect(() => {
    if (!agent) return
    const previous = document.title
    document.title = `${agent.name} · ${appName}`
    return () => {
      document.title = previous
    }
  }, [agent, appName])

  const showInstructions = async () => {
    try {
      setInstructions(await getWithToken(agentID))
    } catch (err) {
      push((err as ApiError).message || 'Could not load install instructions', 'error')
    }
  }

  const doDelete = async () => {
    try {
      await remove(agentID)
      push(`${agent?.name ?? 'Agent'} unregistered`, 'success')
      navigate('/servers')
    } catch (err) {
      push((err as ApiError).message || 'Could not unregister the agent', 'error')
    }
  }

  if (loading && !detail) {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-slate-800/40 p-10 text-sm text-slate-400">
        <Loader2 className="h-4 w-4 animate-spin" /> Loading server…
      </div>
    )
  }

  if (error || !agent) {
    return (
      <div className="space-y-4">
        <Link to="/servers" className="inline-flex items-center gap-2 text-sm text-slate-400 transition hover:text-white">
          <ArrowLeft className="h-4 w-4" /> Back to Servers
        </Link>
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-6 text-sm text-red-400">
          {error ?? 'That server could not be found.'}
        </div>
      </div>
    )
  }

  const containers = detail?.containers ?? []

  return (
    <div className="space-y-6">
      <Toaster toasts={toasts} />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <Link
            to="/servers"
            className="inline-flex items-center gap-2 text-sm text-slate-400 transition hover:text-white"
          >
            <ArrowLeft className="h-4 w-4" /> Back to Servers
          </Link>
          <nav aria-label="Breadcrumb" className="mt-1 text-xs text-slate-600">
            <Link to="/servers" className="transition hover:text-slate-400">
              Server Monitoring
            </Link>
            <span className="px-1">›</span>
            <span className="text-slate-500">{agent.name}</span>
          </nav>
        </div>
        {isAdmin && (
          <div className="flex items-center gap-2">
            <button className="btn-secondary !py-1.5" onClick={() => void showInstructions()}>
              <Terminal className="h-4 w-4" /> Install instructions
            </button>
            <button
              className="rounded-lg border border-red-500/30 px-3 py-1.5 text-sm text-red-400 transition hover:bg-red-500/10"
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 className="inline h-4 w-4" />
            </button>
          </div>
        )}
      </div>

      {/* The sidebar sits after the main column on narrow screens: the metrics
          are what someone came for, and the machine's specification is
          reference material. */}
      <div className="grid gap-6 xl:grid-cols-[minmax(0,3fr)_minmax(260px,1fr)]">
        <div className="min-w-0 space-y-6">
          <div className="flex items-center gap-3">
            <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg border border-white/10 bg-slate-800/60 text-slate-300">
              <Server className="h-5 w-5" aria-hidden />
            </span>
            <div className="min-w-0">
              <h1 className="truncate text-2xl font-light text-white">{agent.name}</h1>
              <p className="truncate text-sm text-slate-500">
                {[
                  agent.os_version || agent.os_type,
                  agent.ip_address,
                  agent.hostname,
                ]
                  .filter(Boolean)
                  .join(' · ')}
              </p>
            </div>
          </div>

          <ServerStatusCards agent={agent} metric={latest} />

          <HistoricalPerformance
            metrics={metrics}
            loading={metricsLoading}
            range={range}
            onRangeChange={setRange}
          />

          <section>
            <h2 className="mb-3 text-lg font-light text-white">
              Containers {containers.length > 0 && <span className="text-slate-500">({containers.length})</span>}
            </h2>
            {containers.length === 0 ? (
              <p className="rounded-lg border border-white/10 bg-slate-800/40 p-6 text-sm text-slate-500">
                {agent.docker_available === false
                  ? 'This host is not running Docker.'
                  : 'No containers reported yet.'}
              </p>
            ) : (
              <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40 backdrop-blur-sm">
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b border-white/10 bg-slate-800/20 text-xs font-medium text-slate-400">
                        <th className="px-4 py-2 text-left">Name</th>
                        <th className="px-4 py-2 text-left">Image</th>
                        <th className="px-4 py-2 text-left">Status</th>
                        <th className="px-4 py-2 text-left">CPU</th>
                        <th className="px-4 py-2 text-left">Memory</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-white/5">
                      {containers.map((c) => (
                        <tr key={c.id}>
                          <td className="px-4 py-2 text-slate-200">
                            {c.container_name || c.container_id}
                          </td>
                          <td className="max-w-[240px] truncate px-4 py-2 text-slate-400" title={c.image}>
                            {c.image}
                          </td>
                          <td className="px-4 py-2 text-slate-400">{c.status}</td>
                          <td className="px-4 py-2 tabular-nums text-slate-300">
                            {c.cpu_percent.toFixed(2)}%
                          </td>
                          <td className="px-4 py-2 tabular-nums text-slate-300">
                            {c.memory_used_mb} MB
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}
          </section>
        </div>

        <aside className="xl:sticky xl:top-6 xl:self-start">
          <div className="rounded-lg border border-white/10 bg-slate-800/40 p-5 backdrop-blur-sm">
            <h2 className="mb-2 text-xs font-semibold uppercase tracking-widest text-slate-300">
              System Information
            </h2>
            <dl>
              <InfoRow label="OS" value={agent.os_version} />
              <InfoRow label="Kernel" value={agent.kernel_version} />
              <InfoRow label="Architecture" value={agent.architecture} />
              <InfoRow
                label="CPU"
                value={
                  agent.cpu_model
                    ? `${agent.cpu_model}${agent.cpu_cores ? ` (${agent.cpu_cores} cores)` : ''}`
                    : agent.cpu_cores
                      ? `${agent.cpu_cores} cores`
                      : null
                }
              />
              <InfoRow
                label="RAM"
                value={agent.memory_total_mb ? `${(agent.memory_total_mb / 1024).toFixed(1)} GB` : null}
              />
              <InfoRow label="Go version" value={agent.go_version} />
              <InfoRow label="IP" value={agent.ip_address} />
              <InfoRow label="Hostname" value={agent.hostname} />
              <InfoRow
                label="Docker"
                value={agent.docker_available == null ? null : String(agent.docker_available)}
              />
              <InfoRow label="Agent" value={agent.agent_version} />
              <InfoRow label="Agent ID" value={agent.agent_id} />
              <InfoRow label="Interval" value={`${agent.check_interval}s`} />
            </dl>
          </div>
        </aside>
      </div>

      <AddServerAgentModal
        isOpen={!!instructions}
        existing={instructions}
        onClose={() => setInstructions(null)}
        onCreated={() => {}}
        push={push}
      />

      {confirmDelete && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
          onMouseDown={(e) => e.target === e.currentTarget && setConfirmDelete(false)}
        >
          <div role="dialog" aria-modal="true" className="w-full max-w-md rounded-xl border border-white/10 bg-slate-900/95 p-6">
            <h3 className="text-lg font-semibold text-white">Unregister {agent.name}?</h3>
            <p className="mt-2 text-sm text-slate-400">
              Its metric history is deleted with it. The agent on that host keeps running and starts
              failing to report until it is stopped and removed.
            </p>
            <div className="mt-6 flex justify-end gap-2">
              <button className="btn-secondary" onClick={() => setConfirmDelete(false)} disabled={busy}>
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
