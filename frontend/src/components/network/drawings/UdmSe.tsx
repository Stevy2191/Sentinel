import { driveBayLight } from '@/utils/faceplateLights'
import { kit, type DrawingProps } from '@/components/network/drawings/kit'
import { lightClass, PrintedLabel, Reset, Rj45Jack, SfpHousing, SpeedTag } from '@/components/network/drawings/parts'

export const UDM_SE_SIZE = { width: 920, height: 112 }

/** UniFi Dream Machine SE, as udm-se.html: touchscreen, vents along the
 *  top, the 3.5″ drive bay and its light, LAN 1-8 two high, WAN 9 level
 *  with the bottom row, SFP+ 10 over 11 with their state on ▲ ▼. */
export default function UdmSe(props: DrawingProps) {
  const k = kit(props)
  const bay = props.dark ? { light: 'off' as const, flicker: '' as const, title: driveBayLight(undefined).title } : driveBayLight(props.indicators?.drive_bay)
  const lanX = [1, 34, 67, 100]
  return (
    <div className="rf-case rf-silver">
      <div className="rf-vents" style={{ left: 86, right: 16 }}>
        {Array.from({ length: 6 }, (_, i) => (
          <i key={i} />
        ))}
      </div>
      <div className="rf-udmtouch" />
      <div className="rf-name" style={{ left: 16, top: 82 }}>
        Dream Machine SE
      </div>
      <div className="rf-bay" title={bay.title}>
        <i className={`rf-baylight ${lightClass(bay.light, bay.flicker)}`} />
      </div>

      <div className="rf-sect" style={{ left: 600, top: 10, width: 300, height: 100 }}>
        {lanX.map((x, c) => (
          <PrintedLabel key={`t${c}`} x={x + 8} y={2} n={c * 2 + 1} poe={c === 0 ? '+' : ''} />
        ))}
        <div className="rf-rjblock" style={{ left: 0, top: 12, width: 138, height: 62 }}>
          {lanX.map((x, c) => {
            const top = c * 2 + 1
            const bottom = top + 1
            return [
              <Rj45Jack key={top} x={x} y={1} leds={[{ side: 'right', light: k.light(top), flicker: k.fl(top) }]} hit={k.hit(top)} />,
              <Rj45Jack key={bottom} x={x} y={30} flip leds={[{ side: 'right', light: k.light(bottom), flicker: k.fl(bottom) }]} hit={k.hit(bottom)} />,
            ]
          })}
        </div>
        {lanX.map((x, c) => (
          <PrintedLabel key={`b${c}`} x={x + 8} y={79} n={c * 2 + 2} poe={c === 0 ? '+' : ''} />
        ))}

        <div className="rf-wan" style={{ left: 154, top: 42 }}>
          <Rj45Jack x={1} y={1} leds={[{ side: 'right', light: k.light(9), flicker: k.fl(9) }]} hit={k.hit(9)} />
        </div>
        <PrintedLabel x={165} y={79} n={9} globe />
        <SpeedTag x={154} y={88} w={37} text="2.5G" />

        <PrintedLabel x={217} y={2} n={10} globe />
        <SfpHousing x={204} y={12} top={k.cage(10)} bottom={k.cage(11)} bar="arrows" />
        <PrintedLabel x={222} y={79} n={11} />
        <SpeedTag x={204} y={88} w={44} text="10G" />

        <Reset x={266} y={79} />
      </div>
    </div>
  )
}
