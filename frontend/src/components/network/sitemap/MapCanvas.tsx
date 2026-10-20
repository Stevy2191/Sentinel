import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Maximize, Minus, Plus } from 'lucide-react'
import MapNode from '@/components/network/sitemap/MapNode'
import MapLinks, { type LinkGeom, type WanGeom } from '@/components/network/sitemap/MapLinks'
import { LAYOUT, ZOOM_MAX, ZOOM_MIN, layout, routeKind, routePath, wanKey, wheelAction, type Placed } from '@/utils/siteMap'
import type { MapDevice, MapSnapshot, MapTraffic } from '@/types/siteMap'
import '@/components/network/sitemap/sitemap.css'

export type MapSelection = { kind: 'link' | 'device'; id: string }

interface Props {
  map: MapSnapshot
  traffic: MapTraffic | null
  view: 'realistic' | 'simple'
  editing?: boolean
  /** Picking ends for a new link: clicks go to onPick. */
  picking?: boolean
  publicToken?: string
  folds: Set<string>
  onFold: (id: string) => void
  selected?: MapSelection | null
  onSelect?: (s: MapSelection | null) => void
  /** A drag ended: the positions to save. Until the map has any saved, that
   *  is every device's, so nothing else moves when the layout changes. */
  onMove?: (positions: { device_id: string; x: number; y: number }[]) => void
  onPick?: (deviceId: string, ifIndex: number | null) => void
  /** Changes when the map should fit its box again (the view, not a side
   *  panel: opening one keeps the camera). */
  fitKey?: unknown
  /** No zoom buttons and no port links: the site card and the widget. */
  compact?: boolean
  className?: string
}

const GRID = 8
/** Screen pixels a pointer may move and still count as a click. */
const CLICK_SLOP = 5
const snap = (v: number) => Math.round(v / GRID) * GRID
const clampZoom = (z: number) => Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, z))

type Box = { left: number; top: number; right: number; bottom: number; cx: number }

/** The map: devices placed by the tree layout (or where an editor put them),
 *  links routed between their actual ports, pan and zoom. */
export default function MapCanvas(p: Props) {
  const navigate = useNavigate()
  const vp = useRef<HTMLDivElement>(null)
  const cv = useRef<HTMLDivElement>(null)
  const [cam, setCam] = useState({ zoom: 1, x: 0, y: 0 })
  const camRef = useRef(cam)
  camRef.current = cam
  const [drag, setDrag] = useState<{ id: string; x: number; y: number } | null>(null)
  const [tick, setTick] = useState(0)
  const [now, setNow] = useState(() => Date.now())
  const [geoms, setGeoms] = useState<Map<string, LinkGeom>>(new Map())
  const [wanGeoms, setWanGeoms] = useState<WanGeom[]>([])

  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 5000)
    return () => window.clearInterval(t)
  }, [])

  const { placed: laid, width, height, bounds, folded } = useMemo(() => layout(p.map, p.folds), [p.map, p.folds])
  const placed = useMemo(() => {
    if (!drag) return laid
    const m = new Map(laid)
    const cur = m.get(drag.id)
    if (cur) m.set(drag.id, { ...cur, x: drag.x, y: drag.y })
    return m
  }, [laid, drag])

  // The fast poll's statuses are fresher than the map's.
  const devices = useMemo(() => {
    const m = new Map<string, MapDevice>()
    for (const d of p.map.devices) m.set(d.id, { ...d, status: p.traffic?.devices[d.id] ?? d.status })
    return m
  }, [p.map.devices, p.traffic])
  const links = useMemo(() => p.map.links.filter((l) => placed.has(l.a.device_id) && placed.has(l.b.device_id)), [p.map.links, placed])

  // Fit again only when what's drawn changes size, not whenever a fresh copy
  // of the same map arrives (the server re-sends it every 5 minutes and
  // after every edit).
  const boundsKey = `${bounds.minX},${bounds.minY},${bounds.maxX},${bounds.maxY}`
  const boundsRef = useRef(bounds)
  boundsRef.current = bounds
  // The window size the camera was last fitted at (null: not fitted yet).
  const fittedAt = useRef<string | null>(null)
  const fit = useCallback(() => {
    const bounds = boundsRef.current
    const el = vp.current
    if (!el) return
    const w = el.clientWidth
    const h = el.clientHeight
    if (!w || !h) return
    const bw = bounds.maxX - bounds.minX
    const bh = bounds.maxY - bounds.minY
    const zoom = Math.min(w / bw, h / bh, 1.5)
    fittedAt.current = `${window.innerWidth}x${window.innerHeight}`
    setCam({ zoom, x: (w - bw * zoom) / 2 - bounds.minX * zoom, y: Math.max(6, (h - bh * zoom) / 2) - bounds.minY * zoom })
    // boundsKey stands for bounds, whose object changes with every map.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [boundsKey])

  useLayoutEffect(() => fit(), [fit, p.fitKey])
  useEffect(() => {
    const el = vp.current
    if (!el) return
    // A resized window (or a wall display going fullscreen) re-frames the
    // map; a side panel opening narrows the viewport but keeps the camera.
    // A widget has no panel and its tile resizes on its own: it always does.
    const ro = new ResizeObserver(() => {
      if (p.compact || fittedAt.current !== `${window.innerWidth}x${window.innerHeight}`) fit()
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [fit, p.compact])

  // Measure each link's ends in map units, after the nodes have drawn.
  useLayoutEffect(() => {
    const root = cv.current
    if (!root) return
    const z = camRef.current.zoom || 1
    const origin = root.getBoundingClientRect()
    const rel = (r: DOMRect): Box => ({
      left: (r.left - origin.left) / z,
      top: (r.top - origin.top) / z,
      right: (r.right - origin.left) / z,
      bottom: (r.bottom - origin.top) / z,
      cx: (r.left + r.width / 2 - origin.left) / z,
    })
    const boxOf = (id: string): Box | null => {
      const el = root.querySelector(`[data-node="${CSS.escape(id)}"] [data-box]`)
      return el ? rel(el.getBoundingClientRect()) : null
    }
    const portOf = (id: string, ix: number | null) => {
      if (ix == null) return null
      const el = root.querySelector(`[data-node="${CSS.escape(id)}"] [data-if-index="${ix}"]`)
      if (!el) return null
      const r = rel(el.getBoundingClientRect())
      return { x: r.cx, y: (r.top + r.bottom) / 2 }
    }
    // Card ends share a side out, one slot per link, in the order of their
    // far ends (two cables to the same switch get two slots).
    const sideSlots = new Map<string, { key: string; far: string }[]>()
    const orient = links.map((l) => {
      const pa = placed.get(l.a.device_id)!
      const pb = placed.get(l.b.device_id)!
      const aUpper = pa.y <= pb.y
      return { l, upper: aUpper ? l.a : l.b, lower: aUpper ? l.b : l.a, aUpper }
    })
    for (const o of orient) {
      for (const [end, side, far] of [
        [o.upper, 'down', o.lower],
        [o.lower, 'up', o.upper],
      ] as const) {
        const k = `${end.device_id}:${side}`
        const list = sideSlots.get(k) ?? []
        list.push({ key: o.l.key, far: far.device_id })
        sideSlots.set(k, list)
      }
    }
    for (const list of sideSlots.values())
      list.sort((x, y) => placed.get(x.far)!.x - placed.get(y.far)!.x || placed.get(x.far)!.y - placed.get(y.far)!.y || x.key.localeCompare(y.key))
    const cardPoint = (id: string, side: 'up' | 'down', key: string, box: Box) => {
      const list = sideSlots.get(`${id}:${side}`) ?? []
      const i = Math.max(0, list.findIndex((x) => x.key === key))
      return { x: box.left + ((box.right - box.left) * (i + 1)) / (list.length + 1), y: side === 'down' ? box.bottom : box.top }
    }
    const laneIndex = new Map<string, string[]>()
    const out = new Map<string, LinkGeom>()
    for (const o of orient) {
      const ub = boxOf(o.upper.device_id)
      const lb = boxOf(o.lower.device_id)
      if (!ub || !lb) continue
      let a = portOf(o.upper.device_id, o.upper.if_index) ?? cardPoint(o.upper.device_id, 'down', o.l.key, ub)
      let b = portOf(o.lower.device_id, o.lower.if_index) ?? cardPoint(o.lower.device_id, 'up', o.l.key, lb)
      // A bundle leaves each drawing from a bar across its member ports,
      // just outside the faceplate, one short stub per cable, then runs as
      // one line from the bar's middle. On a card (no ports drawn) it is
      // one slot, like a link.
      const bars: string[] = []
      const stubs: { key: string; d: string }[] = []
      const members = o.l.members
      if (members) {
        const bar = (upper: boolean) => {
          const box = upper ? ub : lb
          const pts = members.flatMap((m) => {
            const e = upper === o.aUpper ? m.a : m.b
            const pt = portOf(e.device_id, e.if_index)
            return pt ? [{ m, pt }] : []
          })
          if (pts.length < 2) return null
          const xs = pts.map((x) => x.pt.x)
          const y = upper ? box.bottom + 7 : box.top - 7
          bars.push(`M${Math.min(...xs)},${y} L${Math.max(...xs)},${y}`)
          for (const x of pts) stubs.push({ key: x.m.key, d: `M${x.pt.x},${x.pt.y} L${x.pt.x},${y}` })
          return { x: (Math.min(...xs) + Math.max(...xs)) / 2, y }
        }
        a = bar(true) ?? a
        b = bar(false) ?? b
      }
      const up = placed.get(o.upper.device_id)!
      const lo = placed.get(o.lower.device_id)!
      const kind = routeKind(up.tier, lo.tier, lo.parent === up.id, lo.row)
      let outIndex = 0
      if (kind === 'lane') {
        const list = laneIndex.get(up.id) ?? []
        outIndex = list.length
        list.push(lo.id)
        laneIndex.set(up.id, list)
      }
      const lane = Math.min(ub.left, lb.left) - 12 - outIndex * LAYOUT.laneGap
      // A bundle's bar sits below the box: its route starts from there.
      const srcBottom = members && a.y > ub.bottom ? a.y : ub.bottom
      const route = routePath(kind, a, b, { srcBottom, lane, tgtTop: lb.top, outIndex })
      out.set(o.l.key, { d: route.d, mid: route.mid, startsAtA: o.aUpper, bars: bars.join(' ') || undefined, stubs: stubs.length ? stubs : undefined })
    }
    setGeoms(out)

    // A drawn router's Internet tags: a violet line from each tag to its WAN
    // port, coming at it from below like a link's stub. On a card the tag
    // carries its own short line.
    const wan: WanGeom[] = []
    // The map's devices, not the fast poll's statuses: traffic ticks don't re-measure.
    for (const d of p.map.devices) {
      const id = d.id
      if (!d.wan?.length || !placed.has(id)) continue
      const box = boxOf(id)
      for (const w of d.wan) {
        const tagEl = root.querySelector(`[data-node="${CSS.escape(id)}"] [data-wan-tag="${w.if_index}"]`)
        const port = portOf(id, w.if_index)
        if (!tagEl || !port || !box) continue
        const t = rel(tagEl.getBoundingClientRect())
        const ty = (t.top + t.bottom) / 2
        const x0 = box.left - 10
        const yb = box.bottom + 10
        wan.push({ key: wanKey(id, w.if_index), d: `M${t.right},${ty} L${x0},${ty} L${x0},${yb} L${port.x},${yb} L${port.x},${port.y}` })
      }
    }
    setWanGeoms(wan)
  }, [links, placed, p.map.devices, p.view, tick])

  const relayout = useCallback(() => setTick((t) => t + 1), [])

  // Panning, pinching, dragging devices.
  const pointers = useRef(new Map<number, { x: number; y: number }>())
  const gesture = useRef<
    | { kind: 'pan'; sx: number; sy: number; cx: number; cy: number }
    | { kind: 'pinch'; d: number; zoom: number; cx: number; cy: number; mx: number; my: number }
    | { kind: 'node'; id: string; sx: number; sy: number; ox: number; oy: number; moved: boolean }
    | null
  >(null)

  const onPointerDown = (e: React.PointerEvent) => {
    if ((e.target as HTMLElement).closest('button, a, [data-hit], [data-fold]')) return
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
    const node = (e.target as HTMLElement).closest('[data-node]') as HTMLElement | null
    if (pointers.current.size === 2) {
      const [a, b] = [...pointers.current.values()]
      gesture.current = { kind: 'pinch', d: Math.hypot(a.x - b.x, a.y - b.y), zoom: cam.zoom, cx: cam.x, cy: cam.y, mx: (a.x + b.x) / 2, my: (a.y + b.y) / 2 }
      return
    }
    if (node) {
      const id = node.dataset.node!
      const pl = placed.get(id)
      if (!pl) return
      gesture.current = { kind: 'node', id, sx: e.clientX, sy: e.clientY, ox: pl.x, oy: pl.y, moved: false }
    } else {
      gesture.current = { kind: 'pan', sx: e.clientX, sy: e.clientY, cx: cam.x, cy: cam.y }
    }
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  }

  const onPointerMove = (e: React.PointerEvent) => {
    if (!pointers.current.has(e.pointerId)) return
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
    const g = gesture.current
    if (!g) return
    if (g.kind === 'pinch' && pointers.current.size === 2) {
      const [a, b] = [...pointers.current.values()]
      const d = Math.hypot(a.x - b.x, a.y - b.y)
      const zoom = clampZoom((g.zoom * d) / g.d)
      const r = vp.current!.getBoundingClientRect()
      const mx = g.mx - r.left
      const my = g.my - r.top
      setCam({ zoom, x: mx - ((mx - g.cx) * zoom) / g.zoom, y: my - ((my - g.cy) * zoom) / g.zoom })
    } else if (g.kind === 'pan') {
      setCam((c) => ({ ...c, x: g.cx + e.clientX - g.sx, y: g.cy + e.clientY - g.sy }))
    } else if (g.kind === 'node') {
      // A drag starts after 5 screen pixels, whatever the zoom, so a tap
      // with a little jitter is still a click.
      if (Math.abs(e.clientX - g.sx) + Math.abs(e.clientY - g.sy) > CLICK_SLOP) g.moved = true
      const dx = (e.clientX - g.sx) / cam.zoom
      const dy = (e.clientY - g.sy) / cam.zoom
      if (g.moved && p.editing && !p.picking) setDrag({ id: g.id, x: snap(g.ox + dx), y: snap(g.oy + dy) })
    }
  }

  const onPointerUp = (e: React.PointerEvent) => {
    pointers.current.delete(e.pointerId)
    const g = gesture.current
    gesture.current = null
    if (!g) return
    if (g.kind === 'node') {
      if (g.moved && p.editing && !p.picking && drag) {
        const unsaved = p.map.devices.some((d) => d.x == null || d.y == null)
        const all = unsaved
          ? [...placed.values()].map((q) => ({ device_id: q.id, x: q.id === drag.id ? drag.x : snap(q.x), y: q.id === drag.id ? drag.y : snap(q.y) }))
          : [{ device_id: drag.id, x: drag.x, y: drag.y }]
        p.onMove?.(all)
        // Keep the dropped place until the new map arrives with it.
        return
      }
      if (!g.moved) {
        if (p.picking) p.onPick?.(g.id, null)
        else p.onSelect?.({ kind: 'device', id: g.id })
      }
    } else if (g.kind === 'pan' && Math.abs(e.clientX - g.sx) + Math.abs(e.clientY - g.sy) <= CLICK_SLOP) {
      p.onSelect?.(null)
    }
  }

  // The dropped place is shown until the saved map comes back.
  useEffect(() => setDrag(null), [p.map])

  // A pinch or a mouse wheel's notch zooms around the pointer; a
  // trackpad's two-finger scroll pans (wheelAction). A native listener, not
  // React's passive one, so it can keep the page from scrolling.
  useEffect(() => {
    const el = vp.current
    if (!el || p.compact) return
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const act = wheelAction(e, el.clientHeight)
      if (act.kind === 'pan') {
        setCam((c) => ({ ...c, x: c.x - act.dx, y: c.y - act.dy }))
        return
      }
      const r = el.getBoundingClientRect()
      const mx = e.clientX - r.left
      const my = e.clientY - r.top
      setCam((c) => {
        const zoom = clampZoom(c.zoom * act.factor)
        return { zoom, x: mx - ((mx - c.x) * zoom) / c.zoom, y: my - ((my - c.y) * zoom) / c.zoom }
      })
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [p.compact])

  const zoomBy = (f: number) => {
    const el = vp.current!
    const mx = el.clientWidth / 2
    const my = el.clientHeight / 2
    setCam((c) => {
      const zoom = clampZoom(c.zoom * f)
      return { zoom, x: mx - ((mx - c.x) * zoom) / c.zoom, y: my - ((my - c.y) * zoom) / c.zoom }
    })
  }

  const onPort = (deviceId: string, ifIndex: number) => {
    if (p.picking) return p.onPick?.(deviceId, ifIndex)
    if (!p.publicToken && !p.compact && !p.editing) navigate(`/network/devices/${deviceId}/ports/${ifIndex}`)
  }

  const tiers = useMemo(() => {
    const have = new Set([...laid.values()].map((x: Placed) => x.tier))
    const loose = [...laid.values()].filter((x) => x.tier === 'loose')
    const rows: [string, number][] = []
    if (have.has('router')) rows.push(['Router', LAYOUT.rowY.router - 18])
    if (have.has('core')) rows.push(['Core', LAYOUT.rowY.core - 18])
    if (have.has('dist')) rows.push(['Distribution', LAYOUT.rowY.dist - 18])
    if (have.has('access')) rows.push(['Access', LAYOUT.accessY - 30])
    if (loose.length) rows.push(['Not linked', Math.min(...loose.map((x) => x.y)) - 30])
    return rows
  }, [laid])

  const selectedLink = p.selected?.kind === 'link' ? p.selected.id : null
  return (
    <div
      ref={vp}
      className={`sm-grid relative h-full w-full touch-none overflow-hidden ${p.className ?? ''}`}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      data-map-view={p.view}
    >
      <div
        ref={cv}
        className="absolute left-0 top-0 origin-top-left"
        style={{ width, height, transform: `translate(${cam.x}px, ${cam.y}px) scale(${cam.zoom})` }}
      >
        {tiers.map(([label, y]) => (
          <div key={label} className="pointer-events-none absolute left-2 text-[11px] uppercase tracking-[0.08em] text-slate-600" style={{ top: y }}>
            {label}
          </div>
        ))}
        <MapLinks
          links={links}
          geoms={geoms}
          wanGeoms={wanGeoms}
          traffic={p.traffic}
          devices={devices}
          selected={selectedLink}
          now={now}
          onSelect={(key) => p.onSelect?.({ kind: 'link', id: key })}
        />
        {[...placed.values()].map((pl) => {
          const d = devices.get(pl.id)
          if (!d) return null
          return (
            <MapNode
              key={pl.id}
              device={d}
              placed={pl}
              view={p.view}
              publicToken={p.publicToken}
              folded={p.folds.has(pl.id)}
              editing={!!p.editing}
              selected={p.selected?.kind === 'device' && p.selected.id === pl.id}
              now={now}
              traffic={p.traffic}
              onFold={() => p.onFold(pl.id)}
              onPort={(ix) => onPort(pl.id, ix)}
              onLayout={relayout}
            />
          )
        })}
        {folded.map((f) => {
          const pl = placed.get(f.id)
          if (!pl) return null
          return (
            <button
              key={`fold-${f.id}`}
              type="button"
              className="absolute rounded-lg border border-dashed border-slate-600 bg-slate-900 px-3 py-2 text-xs text-slate-400 hover:text-white"
              style={{ left: pl.x, top: pl.y + 100 }}
              onClick={() => p.onFold(f.id)}
            >
              ▸ {f.count} switches folded
              {f.down > 0 && <span className="text-red-300"> · {f.down} down</span>}
            </button>
          )
        })}
      </div>
      {!p.compact && (
        <div className="absolute bottom-2.5 right-2.5 z-10 flex flex-col gap-1">
          <button type="button" className="rounded-md border border-white/10 bg-slate-800 p-1.5 text-slate-400 hover:text-white" aria-label="Zoom in" onClick={() => zoomBy(1.25)}>
            <Plus className="h-4 w-4" />
          </button>
          <button type="button" className="rounded-md border border-white/10 bg-slate-800 p-1.5 text-slate-400 hover:text-white" aria-label="Zoom out" onClick={() => zoomBy(1 / 1.25)}>
            <Minus className="h-4 w-4" />
          </button>
          <button type="button" className="rounded-md border border-white/10 bg-slate-800 p-1.5 text-slate-400 hover:text-white" aria-label="Fit" onClick={fit}>
            <Maximize className="h-4 w-4" />
          </button>
        </div>
      )}
    </div>
  )
}
