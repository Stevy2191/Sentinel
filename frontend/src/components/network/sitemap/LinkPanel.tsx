import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { X } from 'lucide-react'
import { useMetricsQuery } from '@/hooks/useMetrics'
import { directional, endText, rate, speedText } from '@/utils/siteMap'
import type { LinkRate, MapDevice, MapLink } from '@/types/siteMap'

interface Props {
  link: MapLink
  devices: Map<string, MapDevice>
  rate: LinkRate | undefined
  canEdit: boolean
  isPublic: boolean
  busy: boolean
  onClose: () => void
  onHide: () => void
  onKeep: () => void
  onDelete: () => void
  onChangePorts: () => void
}

function sourceText(l: MapLink, devices: Map<string, MapDevice>): string {
  const parts: string[] = []
  for (const s of l.sources) {
    if (s === 'manual') parts.push('Drawn by hand')
    else if (s === 'port_setting') parts.push('Set on the port (Network link)')
    else if (s === 'lldp') parts.push('Found by LLDP')
  }
  if (l.guess) {
    const [sw, other] = l.a.if_index != null ? [l.a, l.b] : [l.b, l.a]
    const s = devices.get(sw.device_id)?.name ?? 'a switch'
    const o = devices.get(other.device_id)?.name ?? 'this switch'
    parts.push(`Guessed: ${o}'s MAC is learnt on ${s} ${endText(sw, 'switch')}, and ${o} sends no LLDP`)
  }
  if (l.stale && l.last_seen) parts.push(`not seen since ${new Date(l.last_seen).toLocaleString()}`)
  return parts.join(' · ')
}

/** The last hour of the rate end's port, busier way, against its speed. */
function LastHour({ link }: { link: MapLink }) {
  const end = link.rate_end === 'a' ? link.a : link.b
  const q = useMemo(
    () =>
      end.if_index == null
        ? null
        : { deviceIds: [end.device_id], metrics: ['if_in_bps', 'if_out_bps'], instances: [String(end.if_index)], range: '1h' as const },
    [end.device_id, end.if_index]
  )
  const { result } = useMetricsQuery(q)
  const points = useMemo(() => {
    const by = new Map<number, number>()
    for (const s of result?.series ?? []) for (const p of s.points) {
      const t = new Date(p.t).getTime()
      by.set(t, Math.max(by.get(t) ?? 0, p.avg ?? 0))
    }
    return [...by].sort((x, y) => x[0] - y[0])
  }, [result])
  if (points.length < 2) return <p className="text-xs text-slate-500">No history yet.</p>
  const max = link.speed_bps || Math.max(...points.map(([, v]) => v), 1)
  const t0 = points[0][0]
  const span = points[points.length - 1][0] - t0 || 1
  const d = points.map(([t, v], i) => `${i ? 'L' : 'M'}${(((t - t0) / span) * 290).toFixed(1)},${(44 - (v / max) * 40).toFixed(1)}`).join(' ')
  const busy = 44 - (link.threshold_pct / 100) * 40
  return (
    <svg viewBox="0 0 290 48" className="h-12 w-full" role="img" aria-label="Traffic over the last hour">
      <line x1={0} x2={290} y1={busy} y2={busy} stroke="#ef4444" strokeDasharray="3 3" strokeOpacity={0.5} />
      <path d={d} fill="none" stroke="#38bdf8" strokeWidth={2} />
    </svg>
  )
}

/** A link's both ends, its traffic now and over the last hour, where it was
 *  found, and (for editors) what can be done with it. */
export default function LinkPanel(p: Props) {
  const [confirm, setConfirm] = useState(false)
  const { link: l, devices } = p
  const A = devices.get(l.a.device_id)
  const B = devices.get(l.b.device_id)
  const dir = directional(l, p.rate)
  const pct = (v: number | null) => (v == null || !l.speed_bps ? '' : ` · ${((v / l.speed_bps) * 100).toFixed(1)}%`)
  const end = (e: MapLink['a'], d: MapDevice | undefined) => {
    const text = `${d?.name ?? '?'} · ${endText(e, d?.type)}${e.if_index != null && e.name && e.name !== e.label ? ` (${e.name})` : ''}`
    return !p.isPublic && e.if_index != null ? (
      <Link className="text-sm text-sky-400 hover:underline" to={`/network/devices/${e.device_id}/ports/${e.if_index}`}>
        {text} →
      </Link>
    ) : (
      <span className="text-sm text-slate-300">{text}</span>
    )
  }
  return (
    <div className="space-y-3" aria-label="Link">
      <div className="flex items-center justify-between">
        <h3 className="text-[15px] font-medium text-white">Link</h3>
        <button type="button" className="rounded p-1 text-slate-400 hover:text-white" onClick={p.onClose} aria-label="Close">
          <X className="h-4 w-4" />
        </button>
      </div>
      <div className="flex flex-col gap-1">
        {end(l.a, A)}
        {end(l.b, B)}
      </div>
      <dl className="divide-y divide-white/10 text-sm">
        <div className="flex justify-between py-1.5">
          <dt className="text-slate-400">Speed</dt>
          <dd>{speedText(l.speed_bps)}</dd>
        </div>
        <div className="flex justify-between py-1.5">
          <dt className="text-slate-400">To {B?.name}</dt>
          <dd className="tabular-nums">{dir.aToB == null ? '—' : rate(dir.aToB) + pct(dir.aToB)}</dd>
        </div>
        <div className="flex justify-between py-1.5">
          <dt className="text-slate-400">To {A?.name}</dt>
          <dd className="tabular-nums">{dir.bToA == null ? '—' : rate(dir.bToA) + pct(dir.bToA)}</dd>
        </div>
        <div className="flex justify-between py-1.5">
          <dt className="text-slate-400">Busy threshold</dt>
          <dd>{l.threshold_pct}%</dd>
        </div>
      </dl>
      {!p.isPublic && l.rate_end !== '' && (
        <div>
          <p className="mb-1 text-xs text-slate-400">Last hour</p>
          <LastHour link={l} />
        </div>
      )}
      <p className={`rounded-md px-2 py-1.5 text-xs ${l.guess ? 'bg-amber-900/30 text-amber-200' : 'bg-sky-900/30 text-sky-200'}`}>{sourceText(l, devices)}</p>
      {p.canEdit && (
        <div className="flex flex-wrap gap-2">
          {l.guess && (
            <button type="button" className="btn-primary" disabled={p.busy} onClick={p.onKeep}>
              Keep
            </button>
          )}
          <button type="button" className="btn-secondary" disabled={p.busy} onClick={p.onHide}>
            Hide
          </button>
          {l.manual_id && (
            <>
              <button type="button" className="btn-secondary" disabled={p.busy} onClick={p.onChangePorts}>
                Change ports
              </button>
              {confirm ? (
                <span className="flex items-center gap-2 text-xs text-slate-300">
                  Delete this link?
                  <button type="button" className="rounded bg-red-600 px-2 py-1 text-white hover:bg-red-500" disabled={p.busy} onClick={p.onDelete}>
                    Delete
                  </button>
                  <button type="button" className="text-slate-400 hover:text-white" onClick={() => setConfirm(false)}>
                    Cancel
                  </button>
                </span>
              ) : (
                <button type="button" className="btn-secondary" onClick={() => setConfirm(true)}>
                  Delete
                </button>
              )}
            </>
          )}
        </div>
      )}
    </div>
  )
}
