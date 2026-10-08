import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import SectionShell from '@/components/monitoring/SectionShell'
import EmptyLine from '@/components/monitoring/EmptyLine'
import AgentRow from '@/components/monitoring/ServerRow'
import { useAgentActions, type Agent, type CreatedAgent } from '@/hooks/useAgents'
import type { ApiError } from '@/services/api'

interface Props {
  /** Rows after every filter. */
  agents: Agent[]
  /** How many servers there are at all. */
  total: number
  isAdmin: boolean
  collapsed: boolean
  onToggle: () => void
  loading: boolean
  error: string | null
  onRetry: () => void
  onChanged: () => void
  onAdd: () => void
  /** Opens the install instructions for an existing server. */
  onInstructions: (agent: CreatedAgent) => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

export default function ServersSection(p: Props) {
  const navigate = useNavigate()
  const { getWithToken, remove, busy } = useAgentActions()
  const [confirmDelete, setConfirmDelete] = useState<Agent | null>(null)
  const offline = p.agents.filter((a) => a.status === 'offline').length
  const pending = p.agents.filter((a) => a.status === 'pending').length

  const showInstructions = async (agent: Agent) => {
    try {
      // Re-fetched rather than held in memory: the token is not in the list
      // payload, and the install command is meaningless without it.
      p.onInstructions(await getWithToken(agent.agent_id))
    } catch (err) {
      p.push((err as ApiError).message || 'Could not load install instructions', 'error')
    }
  }

  const doDelete = async () => {
    if (!confirmDelete) return
    try {
      await remove(confirmDelete.agent_id)
      p.push(`${confirmDelete.name} unregistered`, 'success')
      setConfirmDelete(null)
      p.onChanged()
    } catch (err) {
      p.push((err as ApiError).message || 'Could not unregister the agent', 'error')
    }
  }

  return (
    <SectionShell
      title="Servers"
      count={p.agents.length}
      down={offline}
      collapsed={p.collapsed}
      onToggle={p.onToggle}
      loading={p.loading}
      error={p.error}
      onRetry={p.onRetry}
      summary={
        <span>
          {offline} offline · {pending} awaiting first report
        </span>
      }
    >
      {p.total === 0 ? (
        <EmptyLine text="No servers are being monitored yet." action="Add a server agent" onAction={p.onAdd} />
      ) : (
        <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40 backdrop-blur-sm">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-white/10 bg-slate-800/20 text-xs font-medium text-slate-400">
                  <th className="px-4 py-3 text-left">Server</th>
                  <th className="px-4 py-3 text-left">Site</th>
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
                {p.agents.map((a, i) => (
                  <AgentRow
                    key={a.id}
                    agent={a}
                    striped={i % 2 === 1}
                    onOpen={() => navigate(`/servers/${a.agent_id}`)}
                    isAdmin={p.isAdmin}
                    onInstructions={() => void showInstructions(a)}
                    onDelete={() => setConfirmDelete(a)}
                  />
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {confirmDelete && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
          onMouseDown={(e) => e.target === e.currentTarget && setConfirmDelete(null)}
        >
          <div role="dialog" aria-modal="true" className="w-full max-w-md rounded-xl border border-white/10 bg-slate-900/95 p-6">
            <h3 className="text-lg font-semibold text-white">Unregister {confirmDelete.name}?</h3>
            <p className="mt-2 text-sm text-slate-400">
              Its metric history is deleted with it. The agent on that host will keep running and start failing to report
              until it is stopped and removed.
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
    </SectionShell>
  )
}
