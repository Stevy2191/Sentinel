import { useMemo } from 'react'
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import type { PingSummary, ToolRun, ToolRunEvent } from '@/types/netTools'
import { chartChrome, chartPalette, colors } from '@/utils/colors'
import { formatLoss, formatMs, isFinalStatus } from '@/utils/netTools'
import { lossTone, pingView, type PingRow } from '@/utils/netToolViews'

interface Props {
  run: ToolRun
  events: ToolRunEvent[]
}

const RESULT: Record<PingRow['kind'], { label: string; tone: string }> = {
  reply: { label: 'Reply', tone: colors.operational.text },
  timeout: { label: 'Timed out', tone: colors.warning.text },
  error: { label: 'Error', tone: colors.error.text },
}

function Tile({ label, value, sub, tone = 'text-white' }: { label: string; value: string; sub?: string; tone?: string }) {
  return (
    <div className="rounded-lg border border-white/10 bg-slate-900/40 p-3">
      <p className="vs-eyebrow">{label}</p>
      <p className={`vs-readout mt-1 text-xl font-light ${tone}`}>{value}</p>
      {sub && <p className="mt-0.5 text-xs text-slate-500">{sub}</p>}
    </div>
  )
}

/** Ping: loss and RTT tiles, a live RTT chart and the reply list. The tiles
 *  show the server's summary once the run is over, the live events before. */
export default function PingResult({ run, events }: Props) {
  const view = useMemo(() => pingView(events), [events])
  const s: PingSummary = isFinalStatus(run.status) && run.summary ? (run.summary as PingSummary) : view.stats
  const count = Math.max(run.params.count ?? 0, view.chart.length, 1)

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">
        <Tile
          label="Loss"
          value={s.sent ? formatLoss(s.loss_pct) : '—'}
          sub={`${s.received} of ${s.sent} replied`}
          tone={s.sent ? lossTone(s.loss_pct) : 'text-white'}
        />
        <Tile label="Min" value={formatMs(s.min_ms)} />
        <Tile label="Avg" value={formatMs(s.avg_ms)} />
        <Tile label="Max" value={formatMs(s.max_ms)} />
        <Tile label="Jitter" value={formatMs(s.jitter_ms)} />
      </div>

      {view.chart.length > 0 && (
        <div className="h-48 rounded-lg border border-white/10 bg-slate-900/30 p-3">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={view.chart} margin={{ top: 5, right: 10, bottom: 0, left: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke={chartChrome.grid} strokeOpacity={0.6} />
              <XAxis
                dataKey="seq"
                type="number"
                domain={[1, count]}
                allowDecimals={false}
                tick={{ fontSize: 10, fill: chartChrome.tick }}
              />
              <YAxis tickFormatter={(v: number) => `${v} ms`} tick={{ fontSize: 10, fill: chartChrome.tick }} width={56} />
              <Tooltip
                labelFormatter={(seq: number) => `Probe ${seq}`}
                formatter={(v: number) => [formatMs(v), 'RTT']}
                contentStyle={{
                  background: chartChrome.tooltipBg,
                  border: `1px solid ${chartChrome.tooltipBorder}`,
                  borderRadius: 8,
                  color: chartChrome.tooltipText,
                }}
              />
              <Line
                type="monotone"
                dataKey="rtt"
                stroke={chartPalette[0]}
                strokeWidth={2}
                dot={{ r: 2 }}
                connectNulls={false}
                isAnimationActive={false}
              />
            </LineChart>
          </ResponsiveContainer>
        </div>
      )}

      {view.rows.length > 0 && (
        <div className="max-h-72 overflow-y-auto rounded-lg border border-white/10">
          <table className="w-full text-sm">
            <thead className="sticky top-0 bg-slate-900">
              <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                <th className="px-3 py-2 font-medium">#</th>
                <th className="px-3 py-2 font-medium">Result</th>
                <th className="px-3 py-2 text-right font-medium">RTT</th>
                <th className="px-3 py-2 text-right font-medium">TTL</th>
                <th className="px-3 py-2 font-medium">From</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/5">
              {view.rows.map((r) => (
                <tr key={r.seq}>
                  <td className="px-3 py-1.5 tabular-nums text-slate-400">{r.seq}</td>
                  <td className={`px-3 py-1.5 ${RESULT[r.kind].tone}`}>
                    {r.kind === 'error' && r.message ? r.message : RESULT[r.kind].label}
                  </td>
                  <td className="px-3 py-1.5 text-right tabular-nums text-slate-300">{formatMs(r.rtt_ms)}</td>
                  <td className="px-3 py-1.5 text-right tabular-nums text-slate-400">{r.ttl ?? '—'}</td>
                  <td className="px-3 py-1.5 font-mono text-xs text-slate-400">{r.from ?? '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
