import { useEffect, useLayoutEffect, useMemo, useRef, useState, type RefObject } from 'react'
import { createPortal } from 'react-dom'
import type { FaceBlock, FacePort, PortView, UnitFaceplate } from '@/hooks/usePorts'
import { formatSpeed } from '@/hooks/useDevices'
import { busiestUtil, PORT_STATE, portState, portTitle, portTraffic, portTrafficText, type PortState } from '@/utils/network'

// RJ45 outline with the latch notch facing away from the other row, as on
// the real switch: top-row notch at the bottom, bottom-row notch at the top.
const RJ45_TOP = 'polygon(0 0,100% 0,100% 70%,72% 70%,72% 100%,28% 100%,28% 70%,0 70%)'
const RJ45_BOTTOM = 'polygon(28% 0,72% 0,72% 30%,100% 30%,100% 100%,0 100%,0 30%,28% 30%)'

interface Props {
  faceplates: UnitFaceplate[]
  ports: PortView[]
  title: string
  subtitle?: string
  selected: number | null
  onSelect: (ifIndex: number) => void
}

/** The virtual switch: every physical port coloured by state and filled from
 *  the bottom by how busy it is. One chassis per stack member; more than one
 *  is capped so a tall stack does not push the rest of the page down. */
export default function Faceplate({ faceplates, ports, title, subtitle, selected, onSelect }: Props) {
  const byIndex = useMemo(() => new Map(ports.map((p) => [p.if_index, p])), [ports])
  const stacked = faceplates.length > 1
  return (
    <div className={stacked ? 'max-h-[60vh] space-y-3 overflow-auto pr-1' : 'space-y-3'}>
      {faceplates.map((fp) => (
        <div key={fp.unit}>
          {fp.label && <p className="mb-1 text-xs font-semibold text-slate-400">{fp.label}</p>}
          <div className="overflow-x-auto pb-1">
            <div className="inline-flex items-center gap-5 rounded-lg border border-slate-700/70 bg-gradient-to-b from-slate-900 to-slate-950 px-4 py-3 shadow-inner">
              {/* Grows to fit the name and model, so neither is cut off; a
                  very long one wraps rather than pushing the ports away. */}
              <div className="min-w-28 max-w-64 shrink-0">
                <p className="flex items-start gap-1.5 text-xs font-semibold text-slate-200">
                  <span className="mt-1 h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-400" aria-hidden />
                  <span className="min-w-0 break-words">{title}</span>
                </p>
                {subtitle && <p className="break-words text-[11px] text-slate-500">{subtitle}</p>}
              </div>
              {fp.blocks.map((b, i) => (
                <Block key={i} block={b} rows={fp.rows} byIndex={byIndex} selected={selected} onSelect={onSelect} />
              ))}
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}

function Block({
  block,
  rows,
  byIndex,
  selected,
  onSelect,
}: {
  block: FaceBlock
  rows: number
  byIndex: Map<number, PortView>
  selected: number | null
  onSelect: (ifIndex: number) => void
}) {
  const cols = Math.max(block.top.length, block.bottom.length)
  const cell = (fp: FacePort | undefined, flip: boolean) =>
    fp ? (
      <PortCell fp={fp} port={byIndex.get(fp.if_index)} sfp={block.sfp} flip={flip} selected={selected === fp.if_index} onSelect={onSelect} />
    ) : (
      <span className={`h-5 ${block.sfp ? 'w-8' : 'w-6'}`} />
    )
  // A network module sits apart from the main ports, its name in the
  // chassis padding above it so every block's ports stay on one line.
  return (
    <div className={block.label ? 'relative flex gap-1 border-l border-slate-700/70 pl-4' : 'flex gap-1'}>
      {block.label && (
        <span className="absolute -top-3 left-4 whitespace-nowrap text-[9.5px] font-semibold uppercase tracking-wide text-slate-500">
          {block.label}
        </span>
      )}
      {Array.from({ length: cols }, (_, c) => (
        <div key={c} className="flex flex-col items-center gap-1">
          <Num fp={block.top[c]} />
          {cell(block.top[c], false)}
          {rows === 2 && cell(block.bottom[c], true)}
          {rows === 2 && <Num fp={block.bottom[c]} />}
        </div>
      ))}
    </div>
  )
}

function Num({ fp }: { fp?: FacePort }) {
  return <span className="h-3 text-[9.5px] leading-3 tabular-nums text-slate-500">{fp?.number ?? ''}</span>
}

function PortCell({
  fp,
  port,
  sfp,
  flip,
  selected,
  onSelect,
}: {
  fp: FacePort
  port: PortView | undefined
  sfp: boolean
  flip: boolean
  selected: boolean
  onSelect: (ifIndex: number) => void
}) {
  const ref = useRef<HTMLButtonElement>(null)
  // Hover and keyboard focus are tracked separately: a mouse leaving a
  // popover that was opened by tabbing to the port must not close it — only
  // blur or Escape should. show is either one.
  const [hovering, setHovering] = useState(false)
  const [focused, setFocused] = useState(false)
  const state: PortState = port ? portState(port) : 'down'
  const util = port ? busiestUtil(port) : null
  const detail = port ? portTrafficText(port) : ''
  const label = port ? `${portTitle(port)}: ${PORT_STATE[state].label}${detail ? ` · ${detail}` : ''}` : `Port ${fp.number}`
  const show = (hovering || focused) && !!port
  const close = () => {
    setHovering(false)
    setFocused(false)
  }
  return (
    <>
      <button
        ref={ref}
        type="button"
        onClick={() => onSelect(fp.if_index)}
        onMouseEnter={() => setHovering(true)}
        onMouseLeave={() => setHovering(false)}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        aria-label={label}
        aria-pressed={selected}
        className={`relative h-5 ${sfp ? 'w-8' : 'w-6'} overflow-hidden rounded-sm transition hover:brightness-125 focus:outline-none focus-visible:ring-2 focus-visible:ring-sky-400 ${PORT_STATE[state].fill} ${selected ? 'ring-2 ring-sky-400' : ''}`}
        style={sfp || selected ? undefined : { clipPath: flip ? RJ45_BOTTOM : RJ45_TOP }}
      >
        {util != null && util > 0 && (
          <span className="absolute inset-x-0 bottom-0 bg-white/55" style={{ height: `${Math.min(100, Math.max(8, util))}%` }} />
        )}
      </button>
      {show && port && <PortPopover anchorRef={ref} port={port} state={state} onClose={close} />}
    </>
  )
}

/** A themed popover, portalled to document.body and positioned with
 *  position: fixed from the hovered cell's own rect. A portal is required
 *  because the faceplate sits inside overflow-x-auto, which would clip an
 *  absolutely positioned child instead of letting it float above the row.
 *  Repositions on scroll (any ancestor, caught via the capture phase — scroll
 *  does not bubble) and on resize while shown, and closes on Escape. */
function PortPopover({
  anchorRef,
  port,
  state,
  onClose,
}: {
  anchorRef: RefObject<HTMLButtonElement | null>
  port: PortView
  state: PortState
  onClose: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const [style, setStyle] = useState<React.CSSProperties>({ position: 'fixed', top: 0, left: 0, visibility: 'hidden' })

  useLayoutEffect(() => {
    const reposition = () => {
      const anchor = anchorRef.current
      const pop = ref.current
      if (!anchor || !pop) return
      const a = anchor.getBoundingClientRect()
      const p = pop.getBoundingClientRect()
      const margin = 8
      let top = a.top - p.height - margin
      if (top < margin) top = a.bottom + margin // flip below near the top of the viewport
      top = Math.min(top, window.innerHeight - p.height - margin)
      let left = a.left + a.width / 2 - p.width / 2
      left = Math.min(Math.max(left, margin), window.innerWidth - p.width - margin)
      setStyle({ position: 'fixed', top, left, visibility: 'visible' })
    }
    reposition()
    window.addEventListener('scroll', reposition, true)
    window.addEventListener('resize', reposition)
    return () => {
      window.removeEventListener('scroll', reposition, true)
      window.removeEventListener('resize', reposition)
    }
  }, [anchorRef])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const rows = portTraffic(port)
  const speed = port.oper_status === 'up' ? formatSpeed(port.speed_bps) : null

  return createPortal(
    <div
      ref={ref}
      role="tooltip"
      style={style}
      className="pointer-events-none z-50 w-56 rounded-lg border border-white/10 bg-slate-800/95 px-3 py-2 text-xs shadow-xl backdrop-blur"
    >
      <p className="truncate font-medium text-slate-100">{portTitle(port)}</p>
      <p className="mt-0.5 flex items-center gap-1.5 text-slate-400">
        <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${PORT_STATE[state].dot}`} aria-hidden />
        {PORT_STATE[state].label}
        {speed && <span>&nbsp;· {speed}</span>}
      </p>
      {rows.length > 0 && (
        <dl className="mt-1.5 space-y-0.5 border-t border-white/10 pt-1.5">
          {rows.map((r) => (
            <div key={r.label} className="flex items-center justify-between gap-4">
              <dt className="text-slate-400">{r.label}</dt>
              <dd className="tabular-nums text-slate-200">{r.value}</dd>
            </div>
          ))}
        </dl>
      )}
    </div>,
    document.body
  )
}

export function FaceplateLegend() {
  const order: PortState[] = ['up', 'warning', 'critical', 'down', 'disabled']
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-slate-400">
      {order.map((s) => (
        <span key={s} className="flex items-center gap-1.5">
          <span className={`h-2.5 w-3.5 rounded-sm ${PORT_STATE[s].fill}`} aria-hidden />
          {PORT_STATE[s].label}
        </span>
      ))}
      <span className="flex items-center gap-1.5">
        <span className="relative h-2.5 w-3.5 overflow-hidden rounded-sm bg-emerald-500" aria-hidden>
          <span className="absolute inset-x-0 bottom-0 h-1/2 bg-white/55" />
        </span>
        Fill shows how busy
      </span>
    </div>
  )
}
