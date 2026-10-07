import { useMemo } from 'react'
import type { ToolRun, ToolRunEvent, TraceSummary } from '@/types/netTools'
import { formatLoss, formatMs, isFinalStatus } from '@/utils/netTools'
import { lossTone, traceView } from '@/utils/netToolViews'

interface Props {
  run: ToolRun
  events: ToolRunEvent[]
}

/** Traceroute as an MTR table: one row per hop, updated after each round. */
export default function TracerouteResult({ run, events }: Props) {
  const final = isFinalStatus(run.status)
  const summary = final && run.summary ? (run.summary as TraceSummary) : null
  const view = useMemo(() => traceView(events, summary), [events, summary])
  const rounds = run.params.rounds ?? 5

  if (view.hops.length === 0) {
    return (
      <p className="text-sm text-slate-500">{final ? 'No hops were recorded.' : 'Waiting for the first round…'}</p>
    )
  }

  const lastAnswer = view.hops.filter((h) => h.received > 0).reduce((t, h) => Math.max(t, h.ttl), 0)
  let note: string
  if (!final) note = `After round ${view.round} of ${rounds}.`
  else if (view.reached) note = `Reached the destination in ${view.hops.length} ${view.hops.length === 1 ? 'hop' : 'hops'}.`
  else if (lastAnswer > 0) note = `The destination did not answer; the last hop that answered is hop ${lastAnswer}.`
  else note = 'No hop answered. ICMP time-exceeded replies may be blocked along the way.'

  const num = 'px-3 py-2 text-right tabular-nums text-slate-300'
  return (
    <div className="space-y-3">
      <div className="overflow-x-auto rounded-lg border border-white/10">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-white/10 text-left text-xs text-slate-400">
              <th className="px-3 py-2 font-medium">Hop</th>
              <th className="px-3 py-2 font-medium">Host</th>
              <th className="px-3 py-2 text-right font-medium">Loss</th>
              <th className="px-3 py-2 text-right font-medium">Sent</th>
              <th className="px-3 py-2 text-right font-medium">Last</th>
              <th className="px-3 py-2 text-right font-medium">Avg</th>
              <th className="px-3 py-2 text-right font-medium">Best</th>
              <th className="px-3 py-2 text-right font-medium">Worst</th>
              <th className="px-3 py-2 text-right font-medium">Stdev</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5">
            {view.hops.map((h) => {
              const addrs = h.addrs ?? []
              return (
                <tr key={h.ttl}>
                  <td className="px-3 py-2 tabular-nums text-slate-400">{h.ttl}</td>
                  <td className="px-3 py-2">
                    {h.name && <div className="text-slate-200">{h.name}</div>}
                    <div className="font-mono text-xs text-slate-400">
                      {addrs.length > 0 ? addrs.join(', ') : 'no reply'}
                    </div>
                  </td>
                  <td className={`px-3 py-2 text-right tabular-nums ${lossTone(h.loss_pct)}`}>{formatLoss(h.loss_pct)}</td>
                  <td className={num}>{h.sent}</td>
                  <td className={num}>{formatMs(h.last_ms)}</td>
                  <td className={num}>{formatMs(h.avg_ms)}</td>
                  <td className={num}>{formatMs(h.best_ms)}</td>
                  <td className={num}>{formatMs(h.worst_ms)}</td>
                  <td className={num}>{formatMs(h.stdev_ms)}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      <p className="text-xs text-slate-500">{note}</p>
    </div>
  )
}
