// The site map's model: the tree layout (routers, core, distribution
// columns with their access switches stacked under them), link colours,
// widths, flow and state, the paths links are drawn along, and the words on
// them. Only type imports from `@/`, so it can be checked on its own.
import type { LinkRate, MapDevice, MapLink, MapSnapshot } from '@/types/siteMap'

export type Tier = 'router' | 'core' | 'dist' | 'access' | 'loose'

export interface Placed {
  id: string
  tier: Tier
  /** The distribution column, for dist and access devices. */
  col: number | null
  /** The row within a column (access), else 0. */
  row: number
  x: number
  y: number
  parent: string | null
  /** How many devices hang below it in the tree. */
  below: number
}

/** How large each tier's realistic drawings are drawn. */
export const TIER_SCALE: Record<Tier, number> = { router: 0.5, core: 0.5, dist: 0.42, access: 0.38, loose: 0.38 }

/** Map units, as the approved mockup lays out 21 switches. */
export const LAYOUT = {
  width: 1960,
  colX: (c: number) => 120 + c * 620,
  colPad: 40,
  rowY: { router: 70, core: 220, dist: 390 } as Record<'router' | 'core' | 'dist', number>,
  accessY: 540,
  step: 104,
  looseGap: 70,
  /** A router or core slot's width, for centring a row. */
  topSlot: 580,
  looseSlot: 360,
  /** How far apart a column's lanes run. */
  laneGap: 10,
}

/** A node's widest drawing in its tier, for the box the map fits to. */
const NODE_W: Record<Tier, number> = { router: 600, core: 600, dist: 504, access: 460, loose: 460 }
const NODE_H = 80

/** The number in a port label, for ordering ("49" → 49, "1/4" → 104). */
function portOrder(label: string | undefined): number {
  if (!label) return 99999
  const m = label.match(/^(?:(\d+)\/)?(\d+)$/)
  if (!m) return 99998
  return (m[1] ? Number(m[1]) * 100 : 0) + Number(m[2])
}

const byName = (a: MapDevice, b: MapDevice) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })

/**
 * layout places every visible device. A device with a saved position keeps
 * it; the rest follow the tree: routers on top (or, with none, the switch
 * with the most links), then the core, then a column per distribution
 * switch with everything below it stacked in pre-order. A folded device's
 * descendants are left out and reported in `folded`.
 */
export function layout(
  map: MapSnapshot,
  folds: Set<string>
): {
  placed: Map<string, Placed>
  width: number
  height: number
  /** Everything drawn, saved positions included: what Fit frames. */
  bounds: { minX: number; minY: number; maxX: number; maxY: number }
  folded: { id: string; count: number; down: number }[]
} {
  const devs = new Map(map.devices.map((d) => [d.id, d] as const))
  // Neighbours, each with the port on this side that leads there.
  const adj = new Map<string, { id: string; port: number }[]>()
  for (const d of map.devices) adj.set(d.id, [])
  for (const l of map.links) {
    if (!devs.has(l.a.device_id) || !devs.has(l.b.device_id)) continue
    adj.get(l.a.device_id)!.push({ id: l.b.device_id, port: portOrder(l.a.label) })
    adj.get(l.b.device_id)!.push({ id: l.a.device_id, port: portOrder(l.b.label) })
  }
  const sortedKids = (id: string) =>
    [...adj.get(id)!].sort((x, y) => x.port - y.port || byName(devs.get(x.id)!, devs.get(y.id)!))

  let roots = map.devices.filter((d) => d.type === 'router').sort(byName)
  if (roots.length === 0 && map.devices.length > 0) {
    const most = [...map.devices].sort((a, b) => adj.get(b.id)!.length - adj.get(a.id)!.length || byName(a, b))[0]
    if (adj.get(most.id)!.length > 0) roots = [most]
  }

  // Breadth first from the roots: depth and parent.
  const depth = new Map<string, number>()
  const parent = new Map<string, string | null>()
  const order: string[] = []
  for (const r of roots) {
    depth.set(r.id, 0)
    parent.set(r.id, null)
    order.push(r.id)
  }
  for (let i = 0; i < order.length; i++) {
    const id = order[i]
    for (const k of sortedKids(id)) {
      if (depth.has(k.id)) continue
      depth.set(k.id, depth.get(id)! + 1)
      parent.set(k.id, id)
      order.push(k.id)
    }
  }
  // A neighbour reached by two cables (a LAG, a redundant pair) is still one child.
  const children = (id: string) => [...new Set(sortedKids(id).filter((k) => parent.get(k.id) === id).map((k) => k.id))]
  const below = (id: string): number => children(id).reduce((n, c) => n + 1 + below(c), 0)
  const descendants = (id: string): string[] => children(id).flatMap((c) => [c, ...descendants(c)])

  const hidden = new Set<string>()
  const folded: { id: string; count: number; down: number }[] = []
  for (const id of order) {
    if (!folds.has(id) || hidden.has(id)) continue
    const ds = descendants(id)
    if (ds.length === 0) continue
    ds.forEach((d) => hidden.add(d))
    folded.push({ id, count: ds.length, down: ds.filter((d) => devs.get(d)!.status === 'down').length })
  }

  const placed = new Map<string, Placed>()
  const tierOf = (d: number): Tier => (d === 0 ? 'router' : d === 1 ? 'core' : d === 2 ? 'dist' : 'access')
  const put = (id: string, p: Omit<Placed, 'id' | 'x' | 'y' | 'below'> & { x: number; y: number }) => {
    const d = devs.get(id)!
    placed.set(id, { id, ...p, x: d.x ?? p.x, y: d.y ?? p.y, below: below(id) })
  }

  // The width: the distribution columns, or a wide router or core row.
  const dists = order.filter((id) => depth.get(id) === 2)
  const topRows = (['router', 'core'] as const).map((t) => order.filter((id) => tierOf(depth.get(id)!) === t))
  const cols = Math.max(3, dists.length)
  const width = Math.max(
    LAYOUT.width,
    LAYOUT.colX(cols) + LAYOUT.colPad,
    ...topRows.map((row) => row.length * LAYOUT.topSlot + 2 * LAYOUT.colX(0))
  )
  // The top rows, centred.
  for (const [i, t] of (['router', 'core'] as const).entries()) {
    const row = topRows[i]
    const left = (width - row.length * LAYOUT.topSlot) / 2
    row.forEach((id, i) => put(id, { tier: t, col: null, row: 0, parent: parent.get(id) ?? null, x: left + i * LAYOUT.topSlot, y: LAYOUT.rowY[t] }))
  }
  // A column per distribution switch, in the order of the ports they hang off.
  let maxRows = 0
  dists.forEach((id, c) => {
    const x = LAYOUT.colX(c) + LAYOUT.colPad
    put(id, { tier: 'dist', col: c, row: 0, parent: parent.get(id) ?? null, x, y: LAYOUT.rowY.dist })
    if (hidden.has(descendants(id)[0] ?? '')) return
    const stack = descendants(id)
    stack.forEach((d, row) => put(d, { tier: 'access', col: c, row, parent: parent.get(d) ?? null, x, y: LAYOUT.accessY + row * LAYOUT.step }))
    maxRows = Math.max(maxRows, stack.length)
  })
  // Everything the tree didn't reach.
  const loose = map.devices.filter((d) => !depth.has(d.id)).sort(byName)
  const bottom = dists.length ? LAYOUT.accessY + maxRows * LAYOUT.step + LAYOUT.looseGap : order.length ? LAYOUT.rowY.dist : LAYOUT.rowY.router
  const perRow = Math.max(1, Math.floor((width - LAYOUT.colX(0)) / LAYOUT.looseSlot))
  loose.forEach((d, i) =>
    put(d.id, {
      tier: 'loose',
      col: null,
      row: Math.floor(i / perRow),
      parent: null,
      x: LAYOUT.colX(0) + LAYOUT.colPad + (i % perRow) * LAYOUT.looseSlot,
      y: bottom + 30 + Math.floor(i / perRow) * LAYOUT.step,
    })
  )
  for (const id of hidden) placed.delete(id)

  let height = 0
  const bounds = { minX: 0, minY: 0, maxX: width, maxY: 0 }
  for (const p of placed.values()) {
    height = Math.max(height, p.y + 140)
    bounds.minX = Math.min(bounds.minX, p.x)
    bounds.minY = Math.min(bounds.minY, p.y - 30)
    bounds.maxX = Math.max(bounds.maxX, p.x + NODE_W[p.tier])
    bounds.maxY = Math.max(bounds.maxY, p.y + NODE_H)
  }
  height = Math.max(height, 400)
  bounds.maxY = Math.max(bounds.maxY, height)
  return { placed, width, height, bounds, folded }
}

export type LinkColour = 'idle' | 'ok' | 'warn' | 'bad'

/** Below this much traffic either way a link is drawn idle. */
export const IDLE_BPS = 1e6

/**
 * linkColour: idle under 1 Mb/s the busier way (busiestBps), whatever the
 * link's speed, then the busier direction's utilisation against the port's
 * busy threshold. Without busiestBps, idle is under 1%.
 */
export function linkColour(u: number | null, thresholdPct: number, busiestBps?: number | null): LinkColour {
  if (busiestBps != null ? busiestBps < IDLE_BPS : u == null || u < 1) return 'idle'
  if (u == null || u < thresholdPct / 2) return 'ok'
  if (u < thresholdPct) return 'warn'
  return 'bad'
}

/** Width by link speed; an unknown speed is drawn as 1 Gb, dashed at the ends. */
export function linkWidth(speedBps: number): { width: number; dashedEnds: boolean } {
  if (!speedBps) return { width: 3, dashedEnds: true }
  if (speedBps < 1e9) return { width: 2, dashedEnds: false }
  if (speedBps < 2.5e9) return { width: 3, dashedEnds: false }
  if (speedBps < 1e10) return { width: 4, dashedEnds: false }
  if (speedBps < 2.5e10) return { width: 5, dashedEnds: false }
  return { width: 7, dashedEnds: false }
}

/** How long one dot takes along a link: faster with more traffic, on a log
 *  scale; null (no dots) below half a megabit. */
export function flowSeconds(bps: number): number | null {
  const mbps = bps / 1e6
  if (mbps < 0.5) return null
  return Math.max(0.5, 7 - 1.5 * Math.log10(mbps + 1))
}

/** Traffic each way along a link, and the busier way's utilisation of
 *  speedBps (a bundle passes the speed of its members that are up). */
export function directional(
  l: MapLink,
  r: LinkRate | undefined,
  speedBps = l.speed_bps
): { aToB: number | null; bToA: number | null; util: number | null } {
  if (!r || l.rate_end === '') return { aToB: null, bToA: null, util: null }
  // in arrives at the rate end's device.
  const [aToB, bToA] = l.rate_end === 'b' ? [r.in_bps, r.out_bps] : [r.out_bps, r.in_bps]
  const busiest = Math.max(aToB ?? 0, bToA ?? 0)
  const util = speedBps > 0 && (aToB != null || bToA != null) ? Math.round((busiest / speedBps) * 1000) / 10 : null
  return { aToB, bToA, util }
}

export type LinkState = 'live' | 'down-port' | 'down-device' | 'no-data' | 'stale'

/** NO_DATA_MS: a link with no figures this long goes grey with "no data". */
export const NO_DATA_MS = 30_000

export function linkState(l: MapLink, r: LinkRate | undefined, devices: Map<string, MapDevice>, now: number): LinkState {
  if (devices.get(l.a.device_id)?.status === 'down' || devices.get(l.b.device_id)?.status === 'down') return 'down-device'
  if (r?.oper_up === false) return 'down-port'
  if (l.stale) return 'stale'
  if (!r?.at || now - new Date(r.at).getTime() > NO_DATA_MS) return 'no-data'
  return 'live'
}

type Pt = { x: number; y: number }

/** A path through points with rounded corners. */
function ortho(pts: [number, number][], r = 7): string {
  let d = `M${pts[0][0]},${pts[0][1]}`
  for (let i = 1; i < pts.length - 1; i++) {
    const [x0, y0] = pts[i - 1]
    const [x1, y1] = pts[i]
    const [x2, y2] = pts[i + 1]
    const a = Math.min(r, Math.hypot(x1 - x0, y1 - y0) / 2)
    const b = Math.min(r, Math.hypot(x2 - x1, y2 - y1) / 2)
    const ux = Math.sign(x1 - x0), uy = Math.sign(y1 - y0), vx = Math.sign(x2 - x1), vy = Math.sign(y2 - y1)
    d += ` L${x1 - ux * a},${y1 - uy * a} Q${x1},${y1} ${x1 + vx * b},${y1 + vy * b}`
  }
  const z = pts[pts.length - 1]
  return `${d} L${z[0]},${z[1]}`
}

/**
 * routePath is how a link is drawn. curve: between rows. lane: from a
 * distribution switch down a lane left of its column, then across and into
 * the port from above. chain: from one access switch straight down to the
 * one hanging off it.
 */
export function routePath(
  kind: 'curve' | 'lane' | 'chain',
  a: Pt,
  b: Pt,
  opts?: { srcBottom: number; lane: number; tgtTop: number; outIndex: number }
): { d: string; mid: [number, number] } {
  if (kind !== 'curve' && opts) {
    const yA = opts.srcBottom + 10 + (kind === 'lane' ? opts.outIndex * LAYOUT.laneGap : 0)
    if (kind === 'chain') {
      return { d: ortho([[a.x, a.y], [a.x, yA], [b.x + 0.01, yA], [b.x, b.y]]), mid: [(a.x + b.x) / 2, yA] }
    }
    const yB = opts.tgtTop - 30
    return {
      d: ortho([[a.x, a.y], [a.x, yA], [opts.lane, yA], [opts.lane, yB], [b.x, yB], [b.x, b.y]]),
      mid: [opts.lane, (yA + yB) / 2],
    }
  }
  const dy = Math.max(40, Math.abs(b.y - a.y) * 0.5)
  const s = a.y < b.y ? 1 : -1
  return { d: `M${a.x},${a.y} C${a.x},${a.y + s * dy} ${b.x},${b.y - s * dy} ${b.x},${b.y}`, mid: [(a.x + b.x) / 2, (a.y + b.y) / 2] }
}

/** "412 Mb/s", "8.8 Gb/s", "1.2 Mb/s", "50 kb/s". */
export function rate(bps: number): string {
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)} Gb/s`
  if (bps >= 1e7) return `${Math.round(bps / 1e6)} Mb/s`
  if (bps >= 1e5) return `${(bps / 1e6).toFixed(1)} Mb/s`
  return `${Math.round(bps / 1e3)} kb/s`
}

/** "1 Gb", "10 Gb", "2.5 Gb", "100 Mb". */
export function speedText(bps: number): string {
  if (!bps) return 'speed unknown'
  if (bps >= 1e9) return `${Number((bps / 1e9).toFixed(1))} Gb`
  return `${Math.round(bps / 1e6)} Mb`
}

/** How an end is named: a switch's port by its number, a router's or other
 *  device's by its interface name ("eth10"), which is what people know it by. */
export function endText(end: MapLink['a'], type: string | undefined): string {
  if (end.if_index == null) return 'port unknown'
  if (type !== 'switch' && end.name) return end.name
  return `port ${end.label || end.name}`
}

/**
 * linkTitle is a link's tooltip, from its rate end: "QuantumLink port 51 ↔
 * Quantum-Gate eth10 · ↓ 412 Mb/s ↑ 38 Mb/s · 1 Gb". ↓ is what arrives at
 * the rate end, ↑ what leaves it.
 */
export function linkTitle(l: MapLink, r: LinkRate | undefined, devices: Map<string, { name: string; type: string }>): string {
  const [near, far] = l.rate_end === 'a' ? [l.a, l.b] : [l.b, l.a]
  const n = devices.get(near.device_id)
  const f = devices.get(far.device_id)
  const ends = `${n?.name ?? '?'} ${endText(near, n?.type)} ↔ ${f?.name ?? '?'} ${endText(far, f?.type)}`
  const traffic = r && (r.in_bps != null || r.out_bps != null) ? ` · ↓ ${rate(r.in_bps ?? 0)} ↑ ${rate(r.out_bps ?? 0)}` : ''
  return `${ends}${traffic} · ${speedText(l.speed_bps)}`
}

/** The live indicator's age: "Updated 12 s ago", "Updated 3 min ago". */
export function ago(iso: string | null, now: number): string {
  if (!iso) return 'No data yet'
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000))
  if (s < 60) return `Updated ${s} s ago`
  return `Updated ${Math.floor(s / 60)} min ago`
}

/**
 * bundleState is a link's traffic as one: a bundle's members added up, each
 * turned to the bundle's way by its own rate end (the bundle's rate end is
 * b, so in is a→b). oper_up is false only when every member is down; down
 * counts the members whose port is; liveSpeed is the speed of those up,
 * which utilisation is measured against. A single link is its own.
 */
export function bundleState(l: MapLink, rates: Record<string, LinkRate> | undefined): { rate: LinkRate; down: number; liveSpeed: number } {
  if (!l.members?.length) {
    return { rate: rates?.[l.key] ?? { in_bps: null, out_bps: null, oper_up: null, at: null }, down: 0, liveSpeed: l.speed_bps }
  }
  let aToB: number | null = null, bToA: number | null = null, at: string | null = null
  let down = 0, known = 0, liveSpeed = 0
  for (const m of l.members) {
    const r = rates?.[m.key]
    if (r?.oper_up === false) down++
    else liveSpeed += m.speed_bps
    if (r?.oper_up != null) known++
    const d = directional(m, r)
    if (d.aToB != null) aToB = (aToB ?? 0) + d.aToB
    if (d.bToA != null) bToA = (bToA ?? 0) + d.bToA
    if (r?.at && (!at || new Date(r.at) > new Date(at))) at = r.at
  }
  const operUp = known === 0 ? null : down < l.members.length
  return { rate: { in_bps: aToB, out_bps: bToA, oper_up: operUp, at }, down, liveSpeed }
}

/** "10G", "1G", "2.5G", "100M"; "?" unknown. */
function shortSpeed(bps: number): string {
  if (!bps) return '?'
  if (bps >= 1e9) return `${Number((bps / 1e9).toFixed(1))}G`
  return `${Math.round(bps / 1e6)}M`
}

/** A bundle's badge: "LAG · 2 × 10G", "2 cables · 2 × 1G", "… · 1 of 2 down". */
export function badgeText(l: MapLink, down: number): string {
  const ms = l.members ?? []
  const speeds = ms.map((m) => shortSpeed(m.speed_bps))
  const same = speeds.every((s) => s === speeds[0])
  const parts = [l.lag ? 'LAG' : `${ms.length} cables`, same ? `${ms.length} × ${speeds[0]}` : speeds.join(' + ')]
  if (down > 0) parts.push(`${down} of ${ms.length} down`)
  return parts.join(' · ')
}

/** The traffic key a router's WAN port comes under. */
export function wanKey(deviceId: string, ifIndex: number): string {
  return `wan:${deviceId}:${ifIndex}`
}

export const ZOOM_MIN = 0.15
export const ZOOM_MAX = 2.5

/**
 * wheelAction is what one wheel event does to the camera. A pinch (ctrlKey,
 * which is also Ctrl + wheel) zooms by exp(-deltaY × 0.01); a mouse wheel's
 * notch (lines or pages, or a whole deltaY of 50 or more with no deltaX)
 * zooms by exp(-deltaY × 0.001), about 10% a notch; anything else, a
 * trackpad's two-finger scroll, pans. A line counts as 16 px and a page as
 * pageHeight; each event's factor stays within 0.8–1.25.
 */
export function wheelAction(
  e: { deltaX: number; deltaY: number; deltaMode: number; ctrlKey: boolean },
  pageHeight: number
): { kind: 'pan'; dx: number; dy: number } | { kind: 'zoom'; factor: number } {
  const unit = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? pageHeight : 1
  const dx = e.deltaX * unit, dy = e.deltaY * unit
  const zoom = (f: number) => ({ kind: 'zoom' as const, factor: Math.round(Math.min(1.25, Math.max(0.8, f)) * 1e4) / 1e4 })
  if (e.ctrlKey) return zoom(Math.exp(-dy * 0.01))
  const notch = e.deltaMode !== 0 || (e.deltaX === 0 && Math.abs(e.deltaY) >= 50 && Number.isInteger(e.deltaY))
  if (notch) return zoom(Math.exp(-dy * 0.001))
  return { kind: 'pan', dx, dy }
}

/**
 * routeKind picks how a tree link is drawn: a distribution switch's link to
 * the first access switch under it drops straight down (chain), to the
 * ones below that down a lane beside the column; an access switch's to the
 * one hanging off it is a chain; everything else a curve.
 */
export function routeKind(upTier: Tier, loTier: Tier, child: boolean, loRow: number): 'curve' | 'lane' | 'chain' {
  if (child && upTier === 'dist' && loTier === 'access') return loRow === 0 ? 'chain' : 'lane'
  if (child && upTier === 'access' && loTier === 'access') return 'chain'
  return 'curve'
}
