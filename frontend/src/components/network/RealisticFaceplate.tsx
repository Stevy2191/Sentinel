import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import type { PortView, UnitFaceplate } from '@/hooks/usePorts'
import Faceplate from '@/components/network/Faceplate'
import { DRAWINGS } from '@/components/network/drawings/registry'
import { offline, type FaceplateDeviceState, type FaceplateIndicators } from '@/utils/faceplateLights'
import '@/components/network/drawings/drawings.css'

// Unscaled heights around each drawn unit: its "Switch N" label (text-xs
// plus mb-1), the badge's room above it, and space-y-2 between units.
const UNIT_LABEL_H = 20
const BADGE_H = 12
const UNIT_GAP = 8

interface Props {
  faceplates: UnitFaceplate[]
  ports: PortView[]
  deviceState?: FaceplateDeviceState
  indicators?: FaceplateIndicators
  title: string
  subtitle?: string
  selected: number | null
  onSelect: (ifIndex: number) => void
  /** page: fit the width between 0.6× and 1.5×, then scroll; widget: always
   *  fit, however small. */
  fit: 'page' | 'widget'
  isPublic?: boolean
}

/** The device as it looks in the rack: each stack unit's realistic drawing,
 *  scaled to fit, or the simple faceplate for a unit without one. When
 *  Sentinel isn't hearing from the device every light is off and a badge
 *  says why. */
export default function RealisticFaceplate({ faceplates, ports, deviceState, indicators, title, subtitle, selected, onSelect, fit, isPublic }: Props) {
  const wrap = useRef<HTMLDivElement>(null)
  const [size, setSize] = useState({ width: 0, height: 0 })
  const [now, setNow] = useState(() => new Date())
  useLayoutEffect(() => {
    const el = wrap.current
    if (!el) return
    const ro = new ResizeObserver(([e]) => setSize({ width: e.contentRect.width, height: e.contentRect.height }))
    ro.observe(el)
    setSize({ width: el.clientWidth, height: el.clientHeight })
    return () => ro.disconnect()
  }, [])
  // Re-check "old port data" without waiting for a refetch.
  useEffect(() => {
    const t = window.setInterval(() => setNow(new Date()), 30_000)
    return () => window.clearInterval(t)
  }, [])
  const state = offline(deviceState, now, isPublic)
  const lastKnown = state.dark ? (state.since ?? '') : undefined

  // A widget shows the whole device: one scale for every unit, fitting the
  // widget's width and its height after the unit labels, the badge and the
  // gaps between units (which don't scale).
  const drawn = faceplates.flatMap((u) => (u.drawing && DRAWINGS[u.drawing] ? [{ u, e: DRAWINGS[u.drawing] }] : []))
  let widgetScale = 1.5
  for (const { e } of drawn) if (size.width > 0) widgetScale = Math.min(widgetScale, size.width / e.width)
  if (fit === 'widget' && size.height > 0 && drawn.length > 0 && drawn.length === faceplates.length) {
    const fixed = drawn.reduce((h, { u }) => h + (u.label ? UNIT_LABEL_H : 0) + (state.badge ? BADGE_H : 0), 0) + (drawn.length - 1) * UNIT_GAP
    const tall = drawn.reduce((h, { e }) => h + e.height, 0)
    widgetScale = Math.min(widgetScale, Math.max(0.05, (size.height - fixed) / tall))
  }

  return (
    <div ref={wrap} className={fit === 'page' ? 'space-y-4 overflow-x-auto pb-1' : 'h-full space-y-2 overflow-hidden'}>
      {faceplates.map((unit) => {
        const entry = unit.drawing ? DRAWINGS[unit.drawing] : undefined
        if (!entry) {
          return (
            <Faceplate key={unit.unit} faceplates={[unit]} ports={ports} title={title} subtitle={subtitle} selected={selected} onSelect={onSelect} />
          )
        }
        const raw = size.width > 0 ? size.width / entry.width : 1
        const scale = fit === 'page' ? Math.min(1.5, Math.max(0.6, raw)) : widgetScale
        return (
          <DrawnUnit
            key={unit.unit}
            unit={unit}
            ports={ports}
            scale={scale}
            entry={entry}
            dark={state.dark}
            badge={state.badge}
            lastKnown={lastKnown}
            indicators={indicators}
            selected={selected}
            onSelect={onSelect}
          />
        )
      })}
    </div>
  )
}

function DrawnUnit({
  unit,
  ports,
  scale,
  entry,
  dark,
  badge,
  lastKnown,
  indicators,
  selected,
  onSelect,
}: {
  unit: UnitFaceplate
  ports: PortView[]
  scale: number
  entry: (typeof DRAWINGS)[string]
  dark: boolean
  badge: string | null
  lastKnown: string | undefined
  indicators?: FaceplateIndicators
  selected: number | null
  onSelect: (ifIndex: number) => void
}) {
  // This unit's physical ports by the number printed on the case. Only
  // physical ones: a VLAN sub-interface (eth10.10) can share a number.
  const byNumber = useMemo(() => {
    const m = new Map<number, PortView>()
    for (const p of ports) if (p.physical && (unit.unit === 0 || (p.stack_unit ?? 0) === unit.unit)) m.set(p.number, p)
    return m
  }, [ports, unit.unit])
  const Drawing = entry.component
  return (
    <div>
      {unit.label && <p className="mb-1 text-xs font-semibold text-slate-400">{unit.label}</p>}
      <div className="relative" style={{ width: entry.width * scale, height: entry.height * scale, marginTop: badge ? BADGE_H : 0 }}>
        {badge && (
          <span className={`rf-badge${badge === 'Paused' || badge.startsWith('Waiting') ? ' rf-badge-quiet' : ''}`} role="status">
            {badge}
          </span>
        )}
        <div className="rf" style={{ width: entry.width, height: entry.height, transform: `scale(${scale})`, transformOrigin: '0 0' }}>
          <Drawing
            unit={unit}
            byNumber={byNumber}
            dark={dark}
            indicators={indicators}
            selected={selected}
            onSelect={onSelect}
            lastKnown={lastKnown}
          />
        </div>
      </div>
    </div>
  )
}
