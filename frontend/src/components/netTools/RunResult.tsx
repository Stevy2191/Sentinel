import type { ToolRun, ToolRunEvent } from '@/types/netTools'
import { isFinalStatus } from '@/utils/netTools'
import DNSResult from './DNSResult'
import PingResult from './PingResult'
import PortsResult from './PortsResult'
import TracerouteResult from './TracerouteResult'

/** The result panel for a run's tool, from its events (stored, live, or
 *  both merged). A finished run with no events shows nothing here: the
 *  status line already says why. */
export default function RunResult({ run, events }: { run: ToolRun; events: ToolRunEvent[] }) {
  if (events.length === 0) {
    return isFinalStatus(run.status) ? null : <p className="text-sm text-slate-500">No results yet.</p>
  }
  if (run.tool === 'ping') return <PingResult run={run} events={events} />
  if (run.tool === 'traceroute') return <TracerouteResult run={run} events={events} />
  if (run.tool === 'dns') return <DNSResult run={run} events={events} />
  return <PortsResult run={run} events={events} />
}
