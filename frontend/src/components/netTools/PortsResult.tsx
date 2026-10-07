import { useMemo, useState } from 'react'
import type { ToolRun, ToolRunEvent } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { formatMs, isFinalStatus } from '@/utils/netTools'
import { portLabel, portsView } from '@/utils/netToolViews'

interface Props {
  run: ToolRun
  events: ToolRunEvent[]
}

/** Ports: progress, open ports first with their service names, and closed
 *  and filtered counts that expand to the list. */
export default function PortsResult({ run, events }: Props) {
  const view = useMemo(() => portsView(events), [events])
  const [shown, setShown] = useState<'closed' | 'filtered' | null>(null)
  const final = isFinalStatus(run.status)
  const toggle = (which: 'closed' | 'filtered') => setShown((s) => (s === which ? null : which))
  const list = shown === 'closed' ? view.closed : shown === 'filtered' ? view.filtered : []

  return (
    <div className="space-y-4">
      <div>
        <div className="mb-1 flex justify-between text-xs text-slate-400">
          <span>{view.total == null ? 'Starting…' : `${view.done} of ${view.total} ports checked`}</span>
          <span>{view.pct}%</span>
        </div>
        <div className="h-2 overflow-hidden rounded-full bg-white/5">
          <div
            className={`h-full bg-gradient-to-r ${colors.operational.gradient} transition-all`}
            style={{ width: `${view.pct}%` }}
          />
        </div>
      </div>

      <section>
        <h3 className="mb-2 text-sm font-medium text-white">
          Open <span className="text-slate-500">({view.open.length})</span>
        </h3>
        {view.open.length === 0 ? (
          <p className="text-sm text-slate-500">{final ? 'No open ports.' : 'None yet.'}</p>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-white/10">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                  <th className="px-3 py-2 font-medium">Port</th>
                  <th className="px-3 py-2 font-medium">Service</th>
                  <th className="px-3 py-2 text-right font-medium">Connect time</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {view.open.map((p) => (
                  <tr key={p.port}>
                    <td className={`px-3 py-1.5 tabular-nums ${colors.operational.text}`}>{p.port}</td>
                    <td className="px-3 py-1.5 text-slate-300">{p.service || '—'}</td>
                    <td className="px-3 py-1.5 text-right tabular-nums text-slate-400">{formatMs(p.rtt_ms)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          className="btn-secondary !py-1"
          aria-expanded={shown === 'closed'}
          disabled={view.closed.length === 0}
          onClick={() => toggle('closed')}
        >
          Closed ({view.closed.length})
        </button>
        <button
          type="button"
          className="btn-secondary !py-1"
          aria-expanded={shown === 'filtered'}
          disabled={view.filtered.length === 0}
          onClick={() => toggle('filtered')}
        >
          Filtered ({view.filtered.length})
        </button>
      </div>
      {list.length > 0 && (
        <p className="font-mono text-xs leading-relaxed text-slate-400">{list.map(portLabel).join(', ')}</p>
      )}
      <p className="text-xs text-slate-500">
        Closed: the host refused the connection. Filtered: no answer within the timeout, or the host is unreachable.
      </p>
    </div>
  )
}
