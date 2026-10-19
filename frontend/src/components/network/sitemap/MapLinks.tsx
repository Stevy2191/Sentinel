import { directional, flowSeconds, linkColour, linkState, linkTitle, linkWidth, rate, type LinkColour } from '@/utils/siteMap'
import type { MapDevice, MapLink, MapTraffic } from '@/types/siteMap'

/** Where a link is drawn: its path, its middle, and whether the path starts
 *  at its A end. */
export interface LinkGeom {
  d: string
  mid: [number, number]
  startsAtA: boolean
}

const COLOUR: Record<LinkColour, string> = { idle: '#64748b', ok: '#22c55e', warn: '#f59e0b', bad: '#ef4444' }

interface Props {
  links: MapLink[]
  geoms: Map<string, LinkGeom>
  traffic: MapTraffic | null
  devices: Map<string, MapDevice>
  selected: string | null
  now: number
  onSelect: (key: string) => void
}

function clock(iso: string | null): string {
  return iso ? new Date(iso).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }) : '?'
}

/** The links layer: colour by how busy, width by speed, dots flowing each
 *  way; grey for no data, dashed for a guess, a down device or a stale link. */
export default function MapLinks({ links, geoms, traffic, devices, selected, now, onSelect }: Props) {
  const names = new Map([...devices].map(([id, d]) => [id, { name: d.name, type: d.type }] as const))
  const titled = links.flatMap((l) => {
    const g = geoms.get(l.key)
    return g ? [{ l, g, title: linkTitle(l, traffic?.links[l.key], names) + (l.guess ? ' · guessed from a MAC table' : '') }] : []
  })
  return (
    <svg className="pointer-events-none absolute left-0 top-0 overflow-visible" width="100%" height="100%" aria-hidden={false}>
      {links.map((l) => {
        const g = geoms.get(l.key)
        if (!g) return null
        const r = traffic?.links[l.key]
        const state = linkState(l, r, devices, now)
        const dir = directional(l, r)
        const { width, dashedEnds } = linkWidth(l.speed_bps)
        let stroke = COLOUR[linkColour(dir.util, l.threshold_pct)]
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
            <path d={g.d} fill="none" stroke={stroke} strokeWidth={width} strokeLinecap="round" strokeLinejoin="round" strokeDasharray={dash} data-state={state} />
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
              <text className="sm-still" x={g.mid[0] + 12} y={g.mid[1] + 4} fontSize={10.5} fontWeight={600} fill="#cbd5e1">
                ↓{rate(fromStart ?? 0)} ↑{rate(toStart ?? 0)}
              </text>
            )}
          </g>
        )
      })}
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
