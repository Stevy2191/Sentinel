import { useRef, useState, type CSSProperties, type ReactNode } from 'react'
import type { PortView } from '@/hooks/usePorts'
import { PortPopover } from '@/components/network/Faceplate'
import { PORT_STATE, portState, portTitle, portTrafficText } from '@/utils/network'
import type { Flicker, Light } from '@/utils/faceplateLights'

/** What every port part needs to behave like today's faceplate. lastKnown
 *  is set (a time, or "") when the device isn't being heard from. */
export interface Hit {
  port?: PortView
  selected: number | null
  onSelect: (ifIndex: number) => void
  lastKnown: string | null | undefined
}

export const lightClass = (light: Light, fl: Flicker = '') =>
  `${light === 'off' ? '' : `rf-l-${light}`}${fl ? ` rf-fl-${fl}` : ''}`

/** A port's button: hover or focus shows the port popover, click selects
 *  it, as PortCell does on the simple faceplate. */
function PortHit({ hit, className, style, children }: { hit: Hit; className: string; style?: CSSProperties; children?: ReactNode }) {
  const ref = useRef<HTMLButtonElement>(null)
  const [hovering, setHovering] = useState(false)
  const [focused, setFocused] = useState(false)
  const { port } = hit
  if (!port) return <span className={className} style={style} aria-hidden />
  const state = portState(port)
  const detail = portTrafficText(port)
  const label = `${portTitle(port)}: ${PORT_STATE[state].label}${detail ? ` · ${detail}` : ''}`
  const selected = hit.selected === port.if_index
  return (
    <>
      <button
        ref={ref}
        type="button"
        className={`${className}${selected ? ' rf-sel' : ''}`}
        style={style}
        aria-label={label}
        aria-pressed={selected}
        onClick={() => hit.onSelect(port.if_index)}
        onMouseEnter={() => setHovering(true)}
        onMouseLeave={() => setHovering(false)}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
      >
        {children}
      </button>
      {(hovering || focused) && (
        <PortPopover
          anchorRef={ref}
          port={port}
          state={state}
          lastKnown={hit.lastKnown === undefined ? undefined : hit.lastKnown}
          onClose={() => {
            setHovering(false)
            setFocused(false)
          }}
        />
      )}
    </>
  )
}

export interface Led {
  side: 'left' | 'right'
  light: Light
  flicker: Flicker
}

/** An RJ45 jack: metal body, dark opening with its latch notch and pins
 *  (mirrored on the bottom row), and its LEDs. */
export function Rj45Jack({ x, y, flip, leds, hit }: { x: number; y: number; flip?: boolean; leds: Led[]; hit: Hit }) {
  return (
    <PortHit hit={hit} className={`rf-jack${flip ? ' rf-flip' : ''}`} style={{ left: x, top: y }}>
      <i className="rf-hole" />
      <i className="rf-pins" />
      {leds.map((l) => (
        <i key={l.side} className={`rf-led rf-${l.side} ${lightClass(l.light, l.flicker)}`} />
      ))}
    </PortHit>
  )
}

export interface CagePort {
  hit: Hit
  light: Light
  flicker: Flicker
}

/** An SFP+ housing: two empty cages, connector edges facing the bar, and the
 *  bar between them: arrows carrying each cage's state (▲ top, ▼ bottom), or
 *  a plain metal divider. */
export function SfpHousing({ x, y, top, bottom, bar }: { x: number; y: number; top: CagePort; bottom: CagePort; bar: 'arrows' | 'plain' }) {
  return (
    <div className="rf-sfpbox" style={{ left: x, top: y }}>
      <PortHit hit={top.hit} className="rf-cage" style={{ top: 1 }}>
        <i className="rf-edge" style={{ top: 12 }} />
        <i className="rf-clip" />
      </PortHit>
      {bar === 'arrows' ? (
        <div className="rf-bar">
          <span className={`rf-a ${lightClass(top.light, top.flicker)}`}>▲</span>
          <span className={`rf-b ${lightClass(bottom.light, bottom.flicker)}`}>▼</span>
        </div>
      ) : (
        <div className="rf-bar rf-plain" />
      )}
      <PortHit hit={bottom.hit} className="rf-cage" style={{ top: 35 }}>
        <i className="rf-edge" style={{ top: 4 }} />
        <i className="rf-clip" />
      </PortHit>
    </div>
  )
}

/** A status panel light with its number under it (ES-16-XG). */
export function PanelLight({ number, light, flicker, hit }: { number: number; light: Light; flicker: Flicker; hit: Hit }) {
  return (
    <PortHit hit={hit} className="rf-lamp">
      <span className={`rf-dot ${lightClass(light, flicker)}`} />
      <span className="rf-pnum">{number}</span>
    </PortHit>
  )
}

const BOLT = <path d="M3.6 0 0.8 4h1.9L2.3 7 5.2 3H3.3z" fill="currentColor" />

/** A printed port label: its number, and the PoE bolt (+ / ++) or WAN globe
 *  printed beside it. */
export function PrintedLabel({ x, y, n, poe, globe }: { x: number; y: number; n: number; poe?: '' | '+' | '++'; globe?: boolean }) {
  return (
    <div className="rf-lbl" style={{ left: x, top: y }}>
      {n}
      {poe !== undefined && <svg viewBox="0 0 6 7">{BOLT}</svg>}
      {poe ? <sup>{poe}</sup> : null}
      {globe && (
        <svg viewBox="0 0 8 8" style={{ width: 7, height: 7 }}>
          <circle cx="4" cy="4" r="3.4" fill="none" stroke="#5b8fe0" strokeWidth=".9" />
          <path d="M.6 4h6.8M4 .6c-1.6 2-1.6 4.8 0 6.8M4 .6c1.6 2 1.6 4.8 0 6.8" stroke="#5b8fe0" strokeWidth=".7" fill="none" />
        </svg>
      )}
    </div>
  )
}

export function SpeedTag({ x, y, w, text }: { x: number; y: number; w: number; text: string }) {
  return (
    <div className="rf-pill" style={{ left: x, top: y, width: w }}>
      {text}
    </div>
  )
}

export function Reset({ x, y }: { x: number; y: number }) {
  return (
    <>
      <div className="rf-reset" style={{ left: x, top: y }} />
      <div className="rf-resetl" style={{ left: x + 10, top: y + 0.5 }}>
        RESET
      </div>
    </>
  )
}
