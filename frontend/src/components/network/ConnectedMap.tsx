import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import { endpointStale, endpointTitle, type APGroup, type BehindGroup, type ConnectedView, type Endpoint } from '@/hooks/useEndpoints'
import { timeAgo } from '@/utils/network'
import { KIND_ICON, KIND_TONE } from '@/components/network/endpointKind'

/** At most this many nodes around the centre; the rest fold into
 *  "+ N more · list view". */
const MAX_ITEMS = 24

type Item =
  | { key: string; type: 'endpoint'; e: Endpoint }
  | { key: string; type: 'ap'; g: APGroup }
  | { key: string; type: 'behind'; g: BehindGroup }
  | { key: string; type: 'more'; n: number }

type Focus = { type: 'ap' | 'behind'; ifIndex: number } | null

const W = 960

function portOf(i: Item): number {
  switch (i.type) {
    case 'endpoint':
      return i.e.if_index ?? Number.MAX_SAFE_INTEGER
    case 'ap':
    case 'behind':
      return i.g.if_index
    default:
      return Number.MAX_SAFE_INTEGER
  }
}

function hasMAC(i: Item, mac: string): boolean {
  switch (i.type) {
    case 'endpoint':
      return i.e.mac === mac
    case 'ap':
      return i.g.mac === mac || i.g.clients.some((c) => c.mac === mac)
    case 'behind':
      return (i.g.endpoints ?? []).some((c) => c.mac === mac)
    default:
      return false
  }
}

/** Folds a long list to MAX_ITEMS, keeping the highlighted one in view. */
function fold(items: Item[], highlight: string | null): Item[] {
  if (items.length <= MAX_ITEMS) return items
  const shown = items.slice(0, MAX_ITEMS - 1)
  const h = highlight ? items.findIndex((i) => hasMAC(i, highlight)) : -1
  if (h >= MAX_ITEMS - 1) shown[MAX_ITEMS - 2] = items[h]
  return [...shown, { key: 'more', type: 'more', n: items.length - shown.length }]
}

/** Two lines of label: the port, then the name (or IP, or MAC). */
function labelLines(i: Item): { port: string; title: string; detail: string } {
  switch (i.type) {
    case 'endpoint': {
      const e = i.e
      const stale = endpointStale(e)
      return {
        port: e.if_index == null ? 'port unknown' : `Port ${e.port_label}`,
        title: endpointTitle(e),
        detail: stale ? `last seen ${timeAgo(e.last_seen)}` : e.maker,
      }
    }
    case 'ap':
      return {
        port: `Port ${i.g.port_label}`,
        title: i.g.name || 'Access point',
        detail: `${i.g.clients.length} wireless ${i.g.clients.length === 1 ? 'client' : 'clients'}`,
      }
    case 'behind':
      return { port: `Port ${i.g.port_label}`, title: `${i.g.count} devices behind port ${i.g.port_label}`, detail: 'unmanaged switch?' }
    default:
      return { port: '', title: `+ ${i.n} more · list view`, detail: '' }
  }
}

function clip(s: string, n = 24): string {
  return s.length > n ? `${s.slice(0, n - 1)}…` : s
}

/** The Connected map: the switch in the centre with its end devices, APs
 *  (which open to their wireless clients) and "behind" groups around it.
 *  A device the last poll missed is drawn faded with when it was seen. */
export default function ConnectedMap({
  view,
  deviceName,
  highlight,
  highlightKey,
  selected,
  onSelect,
  onShowList,
}: {
  view: ConnectedView
  deviceName: string
  highlight: string | null
  /** Changes on each arrival from search, so the same device searched
   *  again is shown again. */
  highlightKey: string
  selected: string | null
  onSelect: (mac: string) => void
  onShowList: () => void
}) {
  const [focus, setFocus] = useState<Focus>(null)
  const handled = useRef<string | null>(null)

  // A searched-for wireless client opens its AP, once per arrival (the
  // minute refresh must not pull the user back in); a device behind an
  // unmanaged switch only highlights the group.
  useEffect(() => {
    if (!highlight || handled.current === highlightKey) return
    handled.current = highlightKey
    const ap = view.aps.find((g) => g.clients.some((c) => c.mac === highlight))
    if (ap) setFocus({ type: 'ap', ifIndex: ap.if_index })
  }, [highlight, highlightKey, view.aps])

  const ap = focus?.type === 'ap' ? view.aps.find((g) => g.if_index === focus.ifIndex) : undefined
  const group = focus?.type === 'behind' ? view.behind.find((g) => g.if_index === focus.ifIndex) : undefined

  const items = useMemo(() => {
    let all: Item[]
    if (ap) all = ap.clients.map((e) => ({ key: e.mac, type: 'endpoint', e }))
    else if (group) all = (group.endpoints ?? []).map((e) => ({ key: e.mac, type: 'endpoint', e }))
    else {
      all = [
        ...view.endpoints.map((e): Item => ({ key: e.mac, type: 'endpoint', e })),
        ...view.aps.map((g): Item => ({ key: `ap-${g.if_index}`, type: 'ap', g })),
        ...view.behind.map((g): Item => ({ key: `behind-${g.if_index}`, type: 'behind', g })),
      ]
      all.sort((a, b) => portOf(a) - portOf(b) || a.key.localeCompare(b.key))
    }
    return fold(all, highlight)
  }, [ap, group, view, highlight])

  const twoRings = items.length > 12
  const H = twoRings ? 820 : 640
  const cx = W / 2
  const cy = H / 2
  const centreTitle = ap ? ap.name || 'Access point' : group ? `Behind port ${group.port_label}` : deviceName

  const nodes = items.map((item, n) => {
    const angle = -Math.PI / 2 + (2 * Math.PI * n) / items.length
    const r = twoRings ? (n % 2 ? 320 : 200) : 230
    return { item, angle, x: cx + r * Math.cos(angle), y: cy + r * Math.sin(angle) }
  })

  const click = (i: Item) => {
    switch (i.type) {
      case 'endpoint':
        onSelect(i.e.mac)
        break
      case 'ap':
        setFocus({ type: 'ap', ifIndex: i.g.if_index })
        break
      case 'behind':
        setFocus({ type: 'behind', ifIndex: i.g.if_index })
        break
      default:
        onShowList()
    }
  }

  return (
    <div className="card relative overflow-x-auto p-2">
      {focus && (
        <button
          type="button"
          onClick={() => setFocus(null)}
          className="absolute left-3 top-3 z-10 inline-flex items-center gap-1 rounded-md border border-white/10 bg-slate-900/80 px-2 py-1 text-xs text-slate-300 hover:text-white"
        >
          <ArrowLeft className="h-3.5 w-3.5" aria-hidden /> {deviceName}
        </button>
      )}
      {ap?.ap_device_id && (
        <Link
          to={`/network/devices/${ap.ap_device_id}`}
          className="absolute right-3 top-3 z-10 rounded-md border border-white/10 bg-slate-900/80 px-2 py-1 text-xs text-primary-400 hover:underline"
        >
          Open {ap.name || 'access point'} →
        </Link>
      )}
      <svg viewBox={`0 0 ${W} ${H}`} className="h-auto w-full min-w-[640px]" role="img" aria-label={`Devices connected to ${centreTitle}`}>
        {nodes.map(({ item, x, y }) => {
          const stale = item.type === 'endpoint' && endpointStale(item.e)
          const tone =
            item.type === 'ap'
              ? 'stroke-sky-500/50'
              : item.type === 'endpoint' && item.e.how === 'wireless'
                ? 'stroke-sky-500/40'
                : 'stroke-slate-600'
          const dash = item.type === 'behind' ? '6 5' : item.type === 'endpoint' && item.e.how === 'router' ? '2 5' : undefined
          return item.type === 'more' ? null : (
            <line key={`l-${item.key}`} x1={cx} y1={cy} x2={x} y2={y} className={tone} strokeWidth={1.5} strokeDasharray={dash} opacity={stale ? 0.4 : 1} />
          )
        })}

        <g>
          <rect x={cx - 90} y={cy - 24} width={180} height={48} rx={10} className={ap ? 'fill-sky-500/15 stroke-sky-400/70' : 'fill-slate-800 stroke-slate-500'} strokeWidth={1.5} />
          <text x={cx} y={cy + 5} textAnchor="middle" className="fill-slate-100 text-[15px] font-medium">
            {clip(centreTitle, 22)}
          </text>
        </g>

        {nodes.map(({ item, angle, x, y }) => {
          const { port, title, detail } = labelLines(item)
          const cos = Math.cos(angle)
          const anchor = cos > 0.35 ? 'start' : cos < -0.35 ? 'end' : 'middle'
          const below = Math.sin(angle) > -0.35 || anchor !== 'middle'
          const lx = x + (anchor === 'start' ? 30 : anchor === 'end' ? -30 : 0)
          // Above a node, clear of the highlight ring.
          const ly = anchor === 'middle' ? (below ? y + 44 : y - 68) : y - 12
          const isHi = !!highlight && hasMAC(item, highlight)
          const isSel = item.type === 'endpoint' && item.e.mac === selected
          const stale = item.type === 'endpoint' && endpointStale(item.e)
          const kind = item.type === 'endpoint' ? item.e.kind : item.type === 'ap' ? 'ap' : 'behind'
          const Icon = item.type === 'more' ? null : KIND_ICON[kind]
          const r = item.type === 'ap' ? 24 : 20
          const name =
            item.type === 'endpoint'
              ? `${title}, ${port}`
              : item.type === 'ap'
                ? `${title}, ${detail}, open`
                : item.type === 'behind'
                  ? `${title}, open`
                  : title
          return (
            <g
              key={item.key}
              role="button"
              tabIndex={0}
              aria-label={name}
              data-mac={item.type === 'endpoint' ? item.e.mac : undefined}
              data-highlight={isHi || undefined}
              onClick={() => click(item)}
              onKeyDown={(ev) => {
                if (ev.key === 'Enter' || ev.key === ' ') {
                  ev.preventDefault()
                  click(item)
                }
              }}
              className="cursor-pointer outline-none [&:focus-visible_circle]:stroke-primary-400"
              opacity={stale ? 0.5 : 1}
            >
              {item.type === 'more' ? (
                <text x={x} y={y + 5} textAnchor="middle" className="fill-primary-400 text-[14px] hover:underline">
                  {title}
                </text>
              ) : (
                <>
                  {isHi && <circle cx={x} cy={y} r={r + 8} className="animate-pulse fill-none stroke-primary-400" strokeWidth={3} />}
                  {/* An opaque base under the tint, so the link line stops at the node. */}
                  {item.type === 'behind' ? (
                    <>
                      <rect x={x - r} y={y - r} width={2 * r} height={2 * r} rx={8} className="fill-slate-900" />
                      <rect x={x - r} y={y - r} width={2 * r} height={2 * r} rx={8} className={KIND_TONE.behind.node} strokeWidth={1.5} strokeDasharray="4 3" />
                    </>
                  ) : (
                    <>
                      <circle cx={x} cy={y} r={r} className="fill-slate-900" />
                      <circle cx={x} cy={y} r={r} className={`${KIND_TONE[kind].node} ${isSel ? '!stroke-white' : ''}`} strokeWidth={isSel ? 2.5 : 1.5} />
                    </>
                  )}
                  {Icon && <Icon x={x - 10} y={y - 10} width={20} height={20} className={KIND_TONE[kind].icon} aria-hidden />}
                  {item.type === 'ap' && (
                    <g>
                      <circle cx={x + 18} cy={y - 18} r={10} className="fill-sky-500" />
                      <text x={x + 18} y={y - 14} textAnchor="middle" className="fill-slate-950 text-[11px] font-semibold">
                        {item.g.clients.length}
                      </text>
                    </g>
                  )}
                  <text x={lx} y={ly} textAnchor={anchor} className="text-[12px]">
                    <tspan x={lx} className="fill-slate-500">
                      {port}
                    </tspan>
                    <tspan x={lx} dy={16} className="fill-slate-100">
                      {clip(title, item.type === 'behind' ? 34 : 24)}
                    </tspan>
                    {detail && (
                      <tspan x={lx} dy={15} className={stale ? 'fill-amber-300/80' : 'fill-slate-500'}>
                        {clip(detail)}
                      </tspan>
                    )}
                  </text>
                </>
              )}
            </g>
          )
        })}
      </svg>
      {items.length === 0 && (
        <p className="absolute inset-x-0 top-1/2 text-center text-sm text-slate-500">
          {ap ? 'No wireless clients seen through this access point.' : 'Nothing here.'}
        </p>
      )}
    </div>
  )
}
