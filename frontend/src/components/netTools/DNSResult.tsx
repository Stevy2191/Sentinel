import { useMemo } from 'react'
import type { DNSRecord, ToolRun, ToolRunEvent } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { formatMs, isFinalStatus, STATUS_PILL } from '@/utils/netTools'
import { dnsView } from '@/utils/netToolViews'

interface Props {
  run: ToolRun
  events: ToolRunEvent[]
}

function Records({ title, records }: { title: string; records: DNSRecord[] | null }) {
  const rows = records ?? []
  return (
    <section>
      <h3 className="mb-2 text-sm font-medium text-white">
        {title} <span className="text-slate-500">({rows.length})</span>
      </h3>
      {rows.length === 0 ? (
        <p className="text-sm text-slate-500">None</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-white/10">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                <th className="px-3 py-2 font-medium">Name</th>
                <th className="px-3 py-2 font-medium">Type</th>
                <th className="px-3 py-2 text-right font-medium">TTL (s)</th>
                <th className="px-3 py-2 font-medium">Data</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/5">
              {rows.map((r, i) => (
                <tr key={`${r.name}-${r.type}-${i}`}>
                  <td className="px-3 py-1.5 font-mono text-xs text-slate-300">{r.name}</td>
                  <td className="px-3 py-1.5 text-slate-400">{r.type}</td>
                  <td className="px-3 py-1.5 text-right tabular-nums text-slate-400">{r.ttl}</td>
                  <td className="break-all px-3 py-1.5 font-mono text-xs text-slate-200">{r.data}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

/** DNS: who answered and how, then the answer, authority and additional
 *  sections with their TTLs. */
export default function DNSResult({ run, events }: Props) {
  const answer = useMemo(() => dnsView(events), [events])
  if (!answer) {
    return (
      <p className="text-sm text-slate-500">
        {isFinalStatus(run.status) ? 'No answer was received.' : 'Waiting for the answer…'}
      </p>
    )
  }
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
        <span className={`rd-pill ${answer.rcode === 'NOERROR' ? STATUS_PILL.done : STATUS_PILL.failed}`}>
          {answer.rcode}
        </span>
        <span className="text-slate-300">
          from <span className="font-mono">{answer.server}</span>
        </span>
        <span className="text-slate-400">{formatMs(answer.rtt_ms)}</span>
        <span className="text-slate-400">
          {answer.authoritative ? 'Authoritative answer' : 'Non-authoritative answer'}
        </span>
        {answer.tcp && (
          <span className={`text-xs ${colors.warning.text}`}>
            {answer.truncated ? 'Truncated over UDP, so this answer came over TCP' : 'Answered over TCP'}
          </span>
        )}
      </div>
      <Records title="Answer" records={answer.answer} />
      <Records title="Authority" records={answer.authority} />
      <Records title="Additional" records={answer.additional} />
    </div>
  )
}
