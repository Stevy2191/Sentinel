import { Link } from 'react-router-dom'
import { useAgentStatus, type Agent } from '@/hooks/useAgents'
import { serverState, VIEW_STATUS_LABEL } from '@/utils/monitoringView'
import { STATE_DOT } from '@/utils/siteProfile'

function ServerLine({ agent }: { agent: Agent }) {
  const { detail } = useAgentStatus(agent.agent_id, 20000)
  const cpu = detail?.latest?.cpu_percent
  const state = serverState(agent)
  return (
    <li>
      <Link to={`/servers/${agent.agent_id}`} className="flex items-center justify-between gap-3 p-3 text-sm hover:bg-white/5">
        <span className="flex min-w-0 items-center gap-2">
          <span className={`h-2 w-2 shrink-0 rounded-full ${STATE_DOT[state]}`} title={VIEW_STATUS_LABEL[state]} aria-label={VIEW_STATUS_LABEL[state]} />
          <span className="truncate text-slate-200">{agent.name}</span>
        </span>
        <span className="shrink-0 tabular-nums text-slate-400">{cpu == null ? '—' : `CPU ${Math.round(cpu)}%`}</span>
      </Link>
    </li>
  )
}

/** The servers whose site is this site. canHint: the viewer can set a
 *  server's site (admins). */
export default function SiteServersList({ agents, canHint }: { agents: Agent[]; canHint: boolean }) {
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-light text-white">Servers ({agents.length})</h2>
      {agents.length === 0 ? (
        <p className="text-sm text-slate-500">
          None at this site.{canHint && ' A server’s site is set in its Edit form.'}
        </p>
      ) : (
        <ul className="card divide-y divide-white/10">
          {agents.map((a) => (
            <ServerLine key={a.id} agent={a} />
          ))}
        </ul>
      )}
    </section>
  )
}
