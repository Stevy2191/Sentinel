import { kit, type DrawingProps } from '@/components/network/drawings/kit'
import { SfpHousing } from '@/components/network/drawings/parts'

export const C4500X_16_SIZE = { width: 1120, height: 96 }

const HOUSING = 46 // a housing and the gap after it

function Vents({ x, y, cols, rows, skip = 0 }: { x: number; y: number; cols: number; rows: number; skip?: number }) {
  const cells = []
  for (let r = 0; r < rows; r++)
    for (let c = 0; c < cols; c++) if (!(r === rows - 1 && c < skip)) cells.push(<i key={`${r}-${c}`} style={{ left: c * 9, top: r * 9 }} />)
  return (
    <div className="rf-c45-vents" style={{ left: x, top: y }}>
      {cells}
    </div>
  )
}

function Lamp({ x, y, on, title }: { x: number; y: number; on: boolean; title: string }) {
  return <div className={`rf-c45-lamp${on ? ' rf-on' : ''}`} style={{ left: x, top: y }} title={title} />
}

/** Catalyst 4500-X-16, as c4500x mockup: silver, the black channel along
 *  the top, logo and vents, SID/STATUS/PS1/PS2/FAN lights (STATUS lit while
 *  Sentinel hears from it; the rest aren't read, so unlit), USB, built-in
 *  SFP+ Te1/1/1-16 in eight housings with state on their ▲ ▼ bars, the
 *  blank centre panel, and the uplink bay: the 8-port SFP+ module
 *  (Te1/2/1-8) with its STATUS light, or a blank plate. */
export default function C4500X16(props: DrawingProps) {
  const k = kit(props)
  const module = k.hasModule(2)
  return (
    <div className="rf-case rf-c45">
      <div className="rf-c45-top">
        <i style={{ left: 590 }} />
        <i style={{ left: 990 }} />
      </div>
      <div className="rf-c45-logo" style={{ left: 10, top: 20 }}>
        ılıılı
        <br />
        CISCO
      </div>
      <div className="rf-c45-name" style={{ left: 44, top: 21 }}>
        Catalyst 4500X Series
      </div>
      <Vents x={10} y={34} cols={10} rows={4} skip={3} />
      <div className="rf-c45-btn" style={{ left: 12, top: 64 }} />
      {['SID', 'STATUS', 'PS1', 'PS2', 'FAN'].map((name, i) => (
        <div key={name}>
          <Lamp x={31 + i * 15} y={76} on={name === 'STATUS' && !props.dark} title={name === 'STATUS' ? 'Status' : `${name}: not read by Sentinel`} />
          <div className="rf-c45-lbl" style={{ left: 28 + i * 15 - (name === 'STATUS' ? 3 : 0), top: 85 }}>
            {name}
          </div>
        </div>
      ))}
      <div className="rf-c45-usb" style={{ left: 112, top: 30 }}>
        <i className="rf-a" />
        <i className="rf-b" />
      </div>
      <div className="rf-c45-lbl" style={{ left: 121, top: 85 }}>
        USB
      </div>

      <div className="rf-c45-frame" style={{ left: 154, top: 18, width: 372, height: 67 }}>
        {Array.from({ length: 8 }, (_, c) => (
          <SfpHousing key={c} x={2 + c * HOUSING} y={1} top={k.cage(c * 2 + 1)} bottom={k.cage(c * 2 + 2)} bar="arrows" />
        ))}
      </div>
      <Vents x={156} y={88} cols={41} rows={1} />
      <Vents x={532} y={20} cols={2} rows={8} />
      <div className="rf-c45-blank" style={{ left: 552, top: 18, width: 224, height: 66 }} />
      <Vents x={780} y={20} cols={2} rows={8} />

      <div className="rf-c45-module" style={{ left: 802, top: 16, width: 310, height: 74 }}>
        <div className="rf-c45-screw" style={{ left: 8, top: 6 }} />
        <div className="rf-c45-screw" style={{ right: 8, top: 6 }} />
        {module ? (
          <>
            <div className="rf-c45-frame" style={{ left: 56, top: 3, width: 188, height: 67 }}>
              {Array.from({ length: 4 }, (_, c) => (
                <SfpHousing key={c} x={2 + c * HOUSING} y={1} top={k.mcage(2, c * 2 + 1)} bottom={k.mcage(2, c * 2 + 2)} bar="arrows" />
              ))}
            </div>
            <Lamp x={30} y={58} on={!props.dark} title="Module status" />
            <div className="rf-c45-lbl" style={{ left: 22, top: 65 }}>
              STATUS
            </div>
            <Lamp x={262} y={54} on={false} title="OIR" />
            <div className="rf-c45-lbl" style={{ left: 259, top: 47 }}>
              OIR
            </div>
            <div className="rf-c45-oir" style={{ left: 282, top: 50 }} />
          </>
        ) : (
          <div className="rf-c45-lbl" style={{ left: 130, top: 34 }}>
            no uplink module
          </div>
        )}
      </div>
    </div>
  )
}
