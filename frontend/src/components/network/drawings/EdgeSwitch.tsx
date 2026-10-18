import type { ReactNode } from 'react'
import { kit, type DrawingEntry, type DrawingProps } from '@/components/network/drawings/kit'
import { Legend, PrintedLabel, Reset, Rj45Jack, SfpHousing, type LegendItem } from '@/components/network/drawings/parts'

// The EdgeSwitches the owner runs, as their approved mockups. Two looks:
// the newer generation (ES-8-150W, ES-24-250W, ES-48-500W) and the older
// (ES-24-500W, ES-48-750W). Both share the jack and SFP cage parts; each
// jack has a PoE LED (left, unlit: PoE isn't read yet) and a state LED.

const COL = 33 // a jack and the gap after it
const BLOCK_GAP = 6 // the divider between blocks of 12
const colX = (c: number) => 1 + c * COL + Math.floor(c / 6) * BLOCK_GAP
// Frame width for a two-row run of cols columns.
const frameW = (cols: number) => colX(cols - 1) + COL + 3
const SFP_W = 44

const GREY = '#8b9096'
const ORANGE = '#f08a24'
const GREEN = '#3fe07a'
const YELLOW = '#f2c230'
const NEW_POE: LegendItem[] = [{ label: 'OFF', colour: GREY }, { label: 'PoE+', colour: ORANGE }, { label: '24V', colour: GREEN }]
const NEW_LINK: LegendItem[] = [{ label: 'DOWN', colour: GREY }, { label: '10/100', colour: ORANGE }, { label: '1G', colour: GREEN }]
const OLD_POE: LegendItem[] = [{ label: 'OFF', colour: GREY }, { label: '24V', colour: YELLOW }, { label: '48V', colour: GREEN }]
const OLD_LINK: LegendItem[] = [{ label: 'DOWN', colour: GREY }, { label: '10/100', colour: YELLOW }, { label: '1000', colour: GREEN }]
const sfpLegend = (old: boolean, tenGig: boolean): LegendItem[] => [
  { label: 'DOWN', colour: '#d6dade', arrows: old ? '▽△' : '▲▼' },
  { label: old ? (tenGig ? '1G' : '1000') : '1G', colour: GREEN, arrows: old ? '▼▲' : '▲▼' },
  ...(tenGig ? [{ label: '10G', colour: '#f2f4f6', arrows: old ? '▼▲' : '▲▼' }] : []),
]

function Ears() {
  return (
    <>
      <div className="rf-ear" style={{ left: 12, top: 12 }} />
      <div className="rf-ear" style={{ left: 12, top: 74 }} />
      <div className="rf-ear" style={{ right: 12, top: 12 }} />
      <div className="rf-ear" style={{ right: 12, top: 74 }} />
    </>
  )
}

type Kit = ReturnType<typeof kit>

function leds(k: Kit, n: number) {
  return [
    { side: 'left' as const, light: 'off' as const, flicker: '' as const },
    { side: 'right' as const, light: k.light(n), flicker: k.fl(n) },
  ]
}

/** Ports 1..cols*2 in two rows (odd on top) in one frame with a divider
 *  after every 12, and their numbers above and below. */
function TwoRowPorts({ k, cols }: { k: Kit; cols: number }) {
  return (
    <>
      {Array.from({ length: cols }, (_, c) => (
        <PrintedLabel key={`t${c}`} x={colX(c) + 12} y={2} n={c * 2 + 1} />
      ))}
      <div className="rf-rjblock" style={{ left: 0, top: 12, width: frameW(cols), height: 62 }}>
        {Array.from({ length: cols / 6 - 1 }, (_, b) => (
          <i key={b} className="rf-div" style={{ left: colX((b + 1) * 6) - BLOCK_GAP - 1 }} />
        ))}
        {Array.from({ length: cols }, (_, c) => {
          const top = c * 2 + 1
          return [
            <Rj45Jack key={top} x={colX(c)} y={1} leds={leds(k, top)} hit={k.hit(top)} />,
            <Rj45Jack key={top + 1} x={colX(c)} y={30} flip leds={leds(k, top + 1)} hit={k.hit(top + 1)} />,
          ]
        })}
      </div>
      {Array.from({ length: cols }, (_, c) => (
        <PrintedLabel key={`b${c}`} x={colX(c) + 12} y={79} n={c * 2 + 2} />
      ))}
    </>
  )
}

function Case({ gen, width, ears, children }: { gen: 'new' | 'old'; width: number; ears: boolean; children: ReactNode }) {
  return (
    <div className={`rf-case rf-es rf-es${gen}`} style={{ width }}>
      {ears && <Ears />}
      {children}
    </div>
  )
}

function OldPower({ x, y, dark }: { x: number; y: number; dark: boolean }) {
  return <div className={`rf-espwr${dark ? '' : ' rf-on'}`} style={{ left: x, top: y }} title="Power" />
}

function OldLogo({ x, y, ports, watts, small }: { x: number; y: number; ports: number; watts: string; small?: boolean }) {
  return (
    <div className={`rf-eslogo-old${small ? ' rf-sm' : ''}`} style={{ left: x, top: y }}>
      <span className="rf-u">U</span>
      <span className="rf-sep" />
      <span className="rf-nm">
        EdgeSwitch {ports}
        <em>{watts}</em>
      </span>
    </div>
  )
}

function NewLogo({ x, y }: { x: number; y: number }) {
  return (
    <div className="rf-eslogo" style={{ left: x, top: y }}>
      <i />
      EdgeSwitch
    </div>
  )
}

// ---- ES-8-150W (newer, desktop) ----

const ES8_W = 760
const ES8_FRAME = 1 + 8 * COL + BLOCK_GAP + 2
const es8X = (i: number) => 1 + i * COL + Math.floor(i / 4) * BLOCK_GAP

function Es8150W(props: DrawingProps) {
  const k = kit(props)
  const f = ES8_FRAME
  return (
    <Case gen="new" width={ES8_W} ears={false}>
      <NewLogo x={60} y={45} />
      <div className="rf-sect" style={{ left: 220, top: 3, width: f + 150, height: 92 }}>
        <Legend row x={150} y={10} title="PoE" items={NEW_POE} />
        <Legend row x={150} y={22} title="LINK" items={NEW_LINK} />
        <div className="rf-rjblock" style={{ left: 0, top: 40, width: f, height: 33 }}>
          <i className="rf-div" style={{ left: 1 + 4 * COL - 1 }} />
          {Array.from({ length: 8 }, (_, i) => (
            <Rj45Jack key={i} x={es8X(i)} y={1} leds={leds(k, i + 1)} hit={k.hit(i + 1)} />
          ))}
        </div>
        {Array.from({ length: 8 }, (_, i) => (
          <PrintedLabel key={i} x={es8X(i) + 13} y={79} n={i + 1} />
        ))}
        <PrintedLabel x={f + 40} y={22} n={9} />
        <PrintedLabel x={f + 38} y={60} n={10} />
        <SfpHousing x={f + 52} y={12} top={k.cage(9)} bottom={k.cage(10)} bar="arrows" />
        <Legend x={f + 102} y={12} items={sfpLegend(false, false)} />
        <div className="rf-reset" style={{ left: f + 104, top: 55 }} />
        <div className="rf-resetl" style={{ left: f + 112, top: 54 }}>
          ↻
        </div>
      </div>
    </Case>
  )
}

// ---- 24 ports: ES-24-250W (newer) and ES-24-500W (older) ----

const ES24_W = 1160
const F24 = frameW(12)
const P24 = ES24_W - F24 - 200

function Es24250W(props: DrawingProps) {
  const k = kit(props)
  return (
    <Case gen="new" width={ES24_W} ears>
      <NewLogo x={96} y={60} />
      <div className="rf-sect" style={{ left: P24, top: 3, width: F24 + 160, height: 92 }}>
        <TwoRowPorts k={k} cols={12} />
        <Legend x={F24 + 8} y={14} title="PoE" items={NEW_POE} />
        <Legend x={F24 + 8} y={46} title="LINK" items={NEW_LINK} />
        <PrintedLabel x={F24 + 44} y={2} n={25} w={SFP_W} />
        <SfpHousing x={F24 + 44} y={12} top={k.cage(25)} bottom={k.cage(26)} bar="arrows" />
        <PrintedLabel x={F24 + 44} y={79} n={26} w={SFP_W} />
        <Legend x={F24 + 94} y={14} items={sfpLegend(false, false)} />
        <Reset x={F24 + 100} y={56} />
      </div>
    </Case>
  )
}

function Es24500W(props: DrawingProps) {
  const k = kit(props)
  return (
    <Case gen="old" width={ES24_W} ears>
      <OldPower x={150} y={60} dark={props.dark} />
      <OldLogo x={172} y={52} ports={24} watts="500W" />
      <Legend x={P24 - 26} y={17} title="PoE" items={OLD_POE} />
      <div className="rf-sect" style={{ left: P24, top: 3, width: F24 + 160, height: 92 }}>
        <TwoRowPorts k={k} cols={12} />
        <Legend x={F24 + 8} y={14} title="LINK" items={OLD_LINK} />
        <div className="rf-sfplbl" style={{ left: F24 + 48, top: 2, width: SFP_W }}>
          SFP 1
        </div>
        <SfpHousing x={F24 + 48} y={12} top={k.cage(25)} bottom={k.cage(26)} bar="arrows" />
        <div className="rf-sfplbl" style={{ left: F24 + 48, top: 79, width: SFP_W }}>
          SFP 2
        </div>
        <Legend x={F24 + 98} y={28} items={sfpLegend(true, false)} />
        <Reset x={F24 + 102} y={52} />
      </div>
    </Case>
  )
}

// ---- 48 ports: ES-48-500W (newer) and ES-48-750W (older) ----

const F48 = frameW(24)

function Es48500W(props: DrawingProps) {
  const k = kit(props)
  return (
    <Case gen="new" width={1160} ears>
      <Legend x={48} y={14} title="PoE" items={NEW_POE} />
      <Legend x={74} y={14} title="LINK" items={NEW_LINK} />
      <NewLogo x={48} y={66} />
      <div className="rf-sect" style={{ left: 118, top: 3, width: F48 + 170, height: 92 }}>
        <TwoRowPorts k={k} cols={24} />
        <PrintedLabel x={F48 + 16} y={2} n={49} w={SFP_W} />
        <SfpHousing x={F48 + 16} y={12} top={k.cage(49)} bottom={k.cage(50)} bar="arrows" />
        <PrintedLabel x={F48 + 16} y={76} n={50} w={SFP_W} />
        <div className="rf-pill2" style={{ left: F48 + 16, top: 84, width: SFP_W }}>
          10G
        </div>
        <PrintedLabel x={F48 + 68} y={2} n={51} w={SFP_W} />
        <SfpHousing x={F48 + 68} y={12} top={k.cage(51)} bottom={k.cage(52)} bar="arrows" />
        <PrintedLabel x={F48 + 68} y={76} n={52} w={SFP_W} />
        <Legend x={F48 + 120} y={14} items={sfpLegend(false, true)} />
        <Reset x={F48 + 124} y={56} />
      </div>
    </Case>
  )
}

function Es48750W(props: DrawingProps) {
  const k = kit(props)
  return (
    <Case gen="old" width={1200} ears>
      <OldPower x={40} y={66} dark={props.dark} />
      <OldLogo x={48} y={62} ports={48} watts="750W" small />
      <Legend x={84} y={14} title="PoE" items={OLD_POE} />
      <Legend x={110} y={14} title="LINK" items={OLD_LINK} />
      <div className="rf-sect" style={{ left: 178, top: 3, width: F48 + 170, height: 92 }}>
        <TwoRowPorts k={k} cols={24} />
        <div className="rf-sfpwhite" style={{ left: F48 + 12, top: 0, width: 52, height: 90 }}>
          <div className="rf-t" style={{ top: 2 }}>
            SFP+ 1
          </div>
          <div className="rf-t" style={{ top: 79 }}>
            SFP+ 2
          </div>
        </div>
        <SfpHousing x={F48 + 16} y={12} top={k.cage(49)} bottom={k.cage(50)} bar="arrows" />
        <div className="rf-sfplbl" style={{ left: F48 + 70, top: 2, width: SFP_W }}>
          SFP 1
        </div>
        <SfpHousing x={F48 + 70} y={12} top={k.cage(51)} bottom={k.cage(52)} bar="arrows" />
        <div className="rf-sfplbl" style={{ left: F48 + 70, top: 79, width: SFP_W }}>
          SFP 2
        </div>
        <Legend x={F48 + 122} y={26} items={sfpLegend(true, true)} />
        <Reset x={F48 + 126} y={56} />
      </div>
    </Case>
  )
}

export const EDGESWITCH_DRAWINGS: Record<string, DrawingEntry> = {
  'es-8-150w': { component: Es8150W, width: ES8_W, height: 96 },
  'es-24-250w': { component: Es24250W, width: ES24_W, height: 96 },
  'es-24-500w': { component: Es24500W, width: ES24_W, height: 96 },
  'es-48-500w': { component: Es48500W, width: 1160, height: 96 },
  'es-48-750w': { component: Es48750W, width: 1200, height: 96 },
}
