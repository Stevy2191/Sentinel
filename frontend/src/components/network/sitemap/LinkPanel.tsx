import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { X } from 'lucide-react'
import { useMetricsQuery } from '@/hooks/useMetrics'
import { bundleState, directional, endText, rate, speedText } from '@/utils/siteMap'
import type { LinkRate, MapDevice, MapLink } from '@/types/siteMap'

interface Props {
  link: MapLink
  devices: Map<string, MapDevice>
  rate: LinkRate | undefined
  /** Every link's own figures: a bundle's members are read from here. */
  rates?: Record<string, LinkRate>
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
    else if (s === 'cdp') parts.push('Found by CDP')
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
 *  found, and (for editors) what can be done with it. A bundle shows its
 *  cables instead of the ends, and no last hour (it would be one cable's). */
export default function LinkPanel(p: Props) {
  const [confirm, setConfirm] = useState(false)
  const { link: l, devices } = p
  const A = devices.get(l.a.device_id)
  const B = devices.get(l.b.device_id)
  const bundle = l.members ? bundleState(l, p.rates) : null
  const speed = bundle ? bundle.liveSpeed : l.speed_bps
  const dir = directional(l, bundle ? bundle.rate : p.rate, speed)
  const pct = (v: number | null) => (v == null || !speed ? '' : ` · ${((v / speed) * 100).toFixed(1)}%`)
  const portLink = (e: MapLink['a'], d: MapDevice | undefined) => {
    const text = endText(e, d?.type)
    return !p.isPublic && e.if_index != null ? (
      <Link className="text-sky-400 hover:underline" to={`/network/devices/${e.device_id}/ports/${e.if_index}`}>
        {text}
      </Link>
    ) : (
      text
    )
  }
  const end = (e: MapLink['a'], d: MapDevice | undefined) => {
    const named = endText(e, d?.type)
    // A port shown by its number also gets its interface name, once.
    const text = `${d?.name ?? '?'} · ${named}${e.if_index != null && e.name && e.name !== e.label && !named.includes(e.name) ? ` (${e.name})` : ''}`
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
        <h3 className="text-[15px] font-medium text-white">{l.members ? (l.lag ? 'LAG' : 'Cables') : 'Link'}</h3>
        <button type="button" className="rounded p-1 text-slate-400 hover:text-white" onClick={p.onClose} aria-label="Close">
          <X className="h-4 w-4" />
        </button>
      </div>
      {l.members && bundle ? (
        <div className="space-y-2">
          <p className="text-sm text-slate-200">
            {A?.name} ↔ {B?.name}
          </p>
          <p className="text-xs text-slate-400">
            {l.lag
              ? `LAG: ${[l.lag.a && `${l.lag.a} on ${A?.name}`, l.lag.b && `${l.lag.b} on ${B?.name}`].filter(Boolean).join(' · ')}`
              : `${l.members.length} cables, not a LAG (each runs on its own)`}
          </p>
          {bundle.down > 0 && (
            <p className="rounded-md bg-amber-900/30 px-2 py-1.5 text-xs text-amber-200">
              {bundle.down} of {l.members.length} cables down: running on {speedText(bundle.liveSpeed)} of {speedText(l.speed_bps)}
            </p>
          )}
          <table className="w-full text-xs">
            <thead className="text-slate-500">
              <tr>
                <th className="py-1 text-left font-normal">{A?.name}</th>
                <th className="py-1 text-left font-normal">{B?.name}</th>
                <th className="py-1 text-right font-normal">To {B?.name} / back</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/5">
              {l.members.map((m) => {
                const r = p.rates?.[m.key]
                const d = directional(m, r)
                const dn = r?.oper_up === false
                return (
                  <tr key={m.key} className="text-slate-300">
                    <td className="py-1">{portLink(m.a, A)}</td>
                    <td className="py-1">{portLink(m.b, B)}</td>
                    <td className={`py-1 text-right tabular-nums ${dn ? 'text-red-300' : ''}`}>
                      {dn ? 'down' : d.aToB == null && d.bToA == null ? '—' : `${rate(d.aToB ?? 0)} / ${rate(d.bToA ?? 0)}`}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="flex flex-col gap-1">
          {end(l.a, A)}
          {end(l.b, B)}
        </div>
      )}
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
      {!p.isPublic && l.rate_end !== '' && !l.members && (
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
