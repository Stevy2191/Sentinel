import { badgeText, bundleState, directional, flowSeconds, linkColour, linkState, linkTitle, linkWidth, rate, type LinkColour } from '@/utils/siteMap'
import type { MapDevice, MapLink, MapTraffic } from '@/types/siteMap'

/** Where a link is drawn: its path, its middle, and whether the path starts
 *  at its A end; for a bundle drawn on its faceplates, the bar across its
 *  member ports at each end and a stub from each member's port to the bar. */
export interface LinkGeom {
  d: string
  mid: [number, number]
  startsAtA: boolean
  bars?: string
  stubs?: { key: string; d: string }[]
}

/** A drawn router's line from its Internet tag to the WAN port, by wanKey. */
export interface WanGeom {
  key: string
  d: string
}

const COLOUR: Record<LinkColour, string> = { idle: '#64748b', ok: '#22c55e', warn: '#f59e0b', bad: '#ef4444' }

interface Props {
  links: MapLink[]
  geoms: Map<string, LinkGeom>
  wanGeoms?: WanGeom[]
  traffic: MapTraffic | null
  devices: Map<string, MapDevice>
  selected: string | null
  now: number
  onSelect: (key: string) => void
}

/** "LAG · 2 × 10G" at a bundle's middle; amber with "1 of 2 down" when a member is. */
function BundleBadge({ text, down, mid }: { text: string; down: boolean; mid: [number, number] }) {
  const w = text.length * 5.6 + 14
  return (
    <g data-badge transform={`translate(${mid[0] - w / 2},${mid[1] - 9})`} style={{ pointerEvents: 'none' }}>
      <rect width={w} height={18} rx={9} fill={down ? '#451a03' : '#0f172a'} stroke={down ? '#f59e0b' : '#334155'} />
      <text x={w / 2} y={12.5} textAnchor="middle" fontSize={10} fontWeight={600} fill={down ? '#fde68a' : '#cbd5e1'}>
        {text}
      </text>
    </g>
  )
}

function clock(iso: string | null): string {
  return iso ? new Date(iso).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }) : '?'
}

/** The links layer: colour by how busy, width by speed, dots flowing each
 *  way; grey for no data, dashed for a guess, a down device or a stale link.
 *  A bundle is two rails with a dark core, its members' traffic added up,
 *  and amber while one of its cables is down. */
export default function MapLinks({ links, geoms, wanGeoms, traffic, devices, selected, now, onSelect }: Props) {
  const names = new Map([...devices].map(([id, d]) => [id, { name: d.name, type: d.type }] as const))
  const states = new Map(links.map((l) => [l.key, bundleState(l, traffic?.links)] as const))
  const rateOf = (l: MapLink) => (l.members ? states.get(l.key)!.rate : traffic?.links[l.key])
  const titled = links.flatMap((l) => {
    const g = geoms.get(l.key)
    const kind = l.members ? ` · ${badgeText(l, states.get(l.key)!.down)}` : l.guess ? ' · guessed from a MAC table' : ''
    return g ? [{ l, g, title: linkTitle(l, rateOf(l), names) + kind }] : []
  })
  return (
    <svg className="pointer-events-none absolute left-0 top-0 overflow-visible" width="100%" height="100%" aria-hidden={false}>
      {links.map((l) => {
        const g = geoms.get(l.key)
        if (!g) return null
        const st = states.get(l.key)!
        const r = rateOf(l)
        const state = linkState(l, r, devices, now)
        const dir = directional(l, r, st.liveSpeed)
        const { width, dashedEnds } = linkWidth(l.speed_bps)
        const busiest = dir.aToB == null && dir.bToA == null ? null : Math.max(dir.aToB ?? 0, dir.bToA ?? 0)
        let stroke = COLOUR[linkColour(dir.util, l.threshold_pct, busiest)]
        let dash: string | undefined = dashedEnds ? '8 5' : undefined
        let opacity = 1
        let note: string | null = null
        let flows = state === 'live'
        switch (state) {
          case 'down-device':
            dash = '7 6'
            stroke = '#475569'
            flows = false
            break
          case 'down-port':
            stroke = '#ef4444'
            break
          case 'no-data':
            stroke = '#64748b'
            note = 'no data'
            break
          case 'stale':
            opacity = 0.4
            dash = '7 6'
            note = `not seen since ${clock(l.last_seen)}`
            break
        }
        if (l.guess && state !== 'down-device') {
          stroke = '#fbbf24'
          dash = '7 6'
        }
        // A bundle missing a cable is degraded, whatever its traffic.
        if (st.down > 0 && state === 'live') stroke = '#f59e0b'
        const downStub = (k: string) => traffic?.links[k]?.oper_up === false
        const upStubs = (g.stubs ?? []).filter((x) => !downStub(x.key))
        const downStubs = (g.stubs ?? []).filter((x) => downStub(x.key))
        const fromStart = g.startsAtA ? dir.aToB : dir.bToA
        const toStart = g.startsAtA ? dir.bToA : dir.aToB
        const fwd = flows && fromStart != null ? flowSeconds(fromStart) : null
        const rev = flows && toStart != null ? flowSeconds(toStart) : null
        const dotW = Math.max(1.4, width * 0.45)
        return (
          <g key={l.key} data-link={l.key} opacity={opacity}>
            {selected === l.key && (
              <path d={g.d} fill="none" stroke="#38bdf8" strokeOpacity={0.35} strokeWidth={width + 8} strokeLinecap="round" strokeLinejoin="round" />
            )}
            {(g.bars || upStubs.length > 0) && (
              <path d={[g.bars ?? '', ...upStubs.map((x) => x.d)].join(' ')} fill="none" stroke={stroke} strokeWidth={2} strokeLinecap="round" />
            )}
            {downStubs.length > 0 && <path data-down-forks d={downStubs.map((x) => x.d).join(' ')} fill="none" stroke="#ef4444" strokeWidth={2} strokeDasharray="3 3" />}
            {l.members ? (
              <>
                <path d={g.d} fill="none" stroke={stroke} strokeWidth={width + 4} strokeLinecap="round" strokeLinejoin="round" strokeDasharray={dash} data-state={state} />
                <path d={g.d} fill="none" stroke="#0b1222" strokeWidth={Math.max(1.5, width - 2)} strokeLinecap="round" strokeLinejoin="round" />
              </>
            ) : (
              <path d={g.d} fill="none" stroke={stroke} strokeWidth={width} strokeLinecap="round" strokeLinejoin="round" strokeDasharray={dash} data-state={state} />
            )}
            {fwd != null && (
              <path className="sm-flow" d={g.d} stroke="rgba(255,255,255,.85)" strokeWidth={dotW} strokeDasharray="1.5 30" style={{ animationDuration: `${fwd}s` }} />
            )}
            {rev != null && (
              <path
                className="sm-flow sm-flow-rev"
                d={g.d}
                stroke="rgba(186,230,253,.9)"
                strokeWidth={dotW}
                strokeDasharray="1.5 30"
                strokeDashoffset={16}
                style={{ animationDuration: `${rev}s` }}
              />
            )}
            {note && (
              <text x={g.mid[0] + 12} y={g.mid[1] + 4} fontSize={10.5} fill="#94a3b8">
                {note}
              </text>
            )}
            {flows && (dir.aToB != null || dir.bToA != null) && (
              <text className="sm-still" x={g.mid[0] + 12} y={g.mid[1] + (l.members ? 20 : 4)} fontSize={10.5} fontWeight={600} fill="#cbd5e1">
                ↓{rate(fromStart ?? 0)} ↑{rate(toStart ?? 0)}
              </text>
            )}
            {l.members && <BundleBadge text={badgeText(l, st.down)} down={st.down > 0} mid={g.mid} />}
          </g>
        )
      })}
      {(wanGeoms ?? []).map((w) => (
        <path key={w.key} data-wan={w.key.replace(/^wan:/, '')} d={w.d} fill="none" stroke="#a78bfa" strokeWidth={3} strokeLinejoin="round" strokeOpacity={0.85} />
      ))}
      {/* Hit areas above every drawn link, no wider than the lanes are apart,
          so a click lands on the link under the pointer; then the guesses'
          "?" above those. */}
      {titled.map(({ l, g, title }) => (
        <path
          key={`hit-${l.key}`}
          d={g.d}
          fill="none"
          stroke="transparent"
          strokeWidth={9}
          className="cursor-pointer"
          style={{ pointerEvents: 'stroke' }}
          data-hit={l.key}
          onClick={(e) => {
            e.stopPropagation()
            onSelect(l.key)
          }}
        >
          <title>{title}</title>
        </path>
      ))}
      {titled
        .filter(({ l }) => l.guess)
        .map(({ l, g, title }) => (
          <g
            key={`q-${l.key}`}
            data-hit={l.key}
            data-guess={l.key}
            transform={`translate(${g.mid[0]},${g.mid[1]})`}
            style={{ pointerEvents: 'all', cursor: 'pointer' }}
            onClick={(e) => {
              e.stopPropagation()
              onSelect(l.key)
            }}
          >
            <circle r={9} fill="#0f172a" stroke="#fbbf24" />
            <text x={-3.4} y={4.2} fontSize={12} fontWeight={700} fill="#fde68a">
              ?
            </text>
            <title>{title}</title>
          </g>
        ))}
    </svg>
  )
}
