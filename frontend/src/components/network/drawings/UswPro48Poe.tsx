import { kit, type DrawingProps } from '@/components/network/drawings/kit'
import { PrintedLabel, Reset, Rj45Jack, SfpHousing, SpeedTag } from '@/components/network/drawings/parts'

export const USW_PRO_48_POE_SIZE = { width: 1108, height: 112 }

const COL = 33 // a jack and the gap after it
const BLOCK_GAP = 6 // the divider between blocks of 12
// Each block of 12 is 6 columns; x of column c (0-23) inside the frame.
const colX = (c: number) => 1 + c * COL + Math.floor(c / 6) * BLOCK_GAP
// The frame: the last column's right edge, 1px of housing, 2px border.
const FRAME_W = colX(23) + COL + 3
const SFP_X = FRAME_W + 14

/** USW-Pro-48-PoE, as usw-pro-48-poe.html: touchscreen, 48 jacks in one
 *  frame with a divider after every 12 (odd on top), two LEDs per jack (PoE
 *  left, unlit: UniFi doesn't report PoE over SNMP; state right), then SFP+
 *  49 over 50 and 51 over 52 with their state on ▲ ▼. */
export default function UswPro48Poe(props: DrawingProps) {
  const k = kit(props)
  const leds = (n: number) => [
    { side: 'left' as const, light: 'off' as const, flicker: '' as const },
    { side: 'right' as const, light: k.light(n), flicker: k.fl(n) },
  ]
  const poe = (n: number) => (n >= 41 ? '++' : '+') as '+' | '++'
  return (
    <div className="rf-case rf-silver">
      <div className="rf-vents" style={{ left: 130, right: 120 }}>
        {Array.from({ length: 5 }, (_, i) => (
          <i key={i} />
        ))}
      </div>
      <div className="rf-uswtouch">
        <i style={{ left: 9, top: 12 }} />
        <i style={{ left: 42, top: 12, width: 7, height: 9 }} />
        <i style={{ left: 10, top: 42, width: 8, height: 8, borderRadius: '50%' }} />
        <i style={{ left: 42, top: 42, width: 8, height: 8, borderRadius: '50%', background: '#5aa0ff' }} />
        <b />
      </div>
      <div className="rf-name" style={{ left: 22, top: 86, fontSize: 7.5 }}>
        USW Pro ⚡
      </div>

      <div className="rf-sect" style={{ left: 100, top: 10, width: SFP_X + 150, height: 100 }}>
        {Array.from({ length: 24 }, (_, c) => (
          <PrintedLabel key={`t${c}`} x={colX(c) + 9} y={2} n={c * 2 + 1} poe={poe(c * 2 + 1)} />
        ))}
        <div className="rf-rjblock" style={{ left: 0, top: 12, width: FRAME_W, height: 62 }}>
          {[1, 2, 3].map((b) => (
            <i key={b} className="rf-div" style={{ left: colX(b * 6) - BLOCK_GAP - 1 }} />
          ))}
          {Array.from({ length: 24 }, (_, c) => {
            const top = c * 2 + 1
            const bottom = top + 1
            return [
              <Rj45Jack key={top} x={colX(c)} y={1} leds={leds(top)} hit={k.hit(top)} />,
              <Rj45Jack key={bottom} x={colX(c)} y={30} flip leds={leds(bottom)} hit={k.hit(bottom)} />,
            ]
          })}
        </div>
        {Array.from({ length: 24 }, (_, c) => (
          <PrintedLabel key={`b${c}`} x={colX(c) + 9} y={79} n={c * 2 + 2} poe={poe(c * 2 + 2)} />
        ))}

        <PrintedLabel x={SFP_X + 16} y={2} n={49} />
        <PrintedLabel x={SFP_X + 68} y={2} n={51} />
        <SfpHousing x={SFP_X} y={12} top={k.cage(49)} bottom={k.cage(50)} bar="arrows" />
        <SfpHousing x={SFP_X + 52} y={12} top={k.cage(51)} bottom={k.cage(52)} bar="arrows" />
        <PrintedLabel x={SFP_X + 16} y={79} n={50} />
        <PrintedLabel x={SFP_X + 68} y={79} n={52} />
        <SpeedTag x={SFP_X} y={88} w={96} text="10G" />
        <Reset x={SFP_X + 112} y={79} />
      </div>
    </div>
  )
}
