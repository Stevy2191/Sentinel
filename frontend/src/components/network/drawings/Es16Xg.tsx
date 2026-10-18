import { kit, type DrawingProps } from '@/components/network/drawings/kit'
import { lightClass, PanelLight, PrintedLabel, Reset, Rj45Jack, SfpHousing } from '@/components/network/drawings/parts'

export const ES_16_XG_SIZE = { width: 1110, height: 96 }

function Legend({ kind }: { kind: string }) {
  return (
    <div className="rf-legend">
      <b>{kind}</b>
      <span>
        <i className="rf-k rf-k-off" />
        DOWN
      </span>
      <span>
        <i className="rf-k rf-k-g" />
        1G
      </span>
      <span>
        <i className="rf-k rf-k-w" />
        10G
      </span>
    </div>
  )
}

/** EdgeSwitch 16 XG, as es-16-xg.html: black case, logo and power light,
 *  the status panel (1-12 SFP+, 13-16 RJ45; white 10G, green 1G, plus
 *  Sentinel's yellow and red), six SFP+ housings with plain dividers, ports
 *  13-16 level with the bottom row of cages, and RESET just right of them.
 *  The jacks and cages have no lights of their own. */
export default function Es16Xg(props: DrawingProps) {
  const k = kit(props)
  const lamp = (n: number) => (
    <PanelLight key={n} number={n} light={k.light(n, { tenGigWhite: true })} flicker={k.fl(n)} hit={k.hit(n)} />
  )
  const plain = (n: number) => ({ ...k.cage(n), light: 'off' as const, flicker: '' as const })
  return (
    <div className="rf-case rf-black">
      <div className="rf-logo">
        <span className="rf-u">U</span>
        <span className="rf-sep" />
        <span className="rf-nm">
          EdgeSwitch 16<em>XG</em>
        </span>
      </div>
      <div className={`rf-pwr ${lightClass(props.dark ? 'off' : 'white')}`} title="Power" />
      <div className="rf-panel" style={{ left: 150, top: 42 }}>
        {Array.from({ length: 12 }, (_, i) => lamp(i + 1))}
        <Legend kind="SFP+" />
      </div>
      <div className="rf-panel" style={{ left: 430, top: 42 }}>
        {[13, 14, 15, 16].map(lamp)}
        <Legend kind="RJ45" />
      </div>

      <div className="rf-sect" style={{ left: 580, top: 3, width: 520, height: 92 }}>
        {Array.from({ length: 6 }, (_, c) => {
          const x = c * 58
          const top = c * 2 + 1
          return [
            <PrintedLabel key={`t${top}`} x={x + 18} y={2} n={top} />,
            <SfpHousing key={`h${top}`} x={x} y={12} top={plain(top)} bottom={plain(top + 1)} bar="plain" />,
            <PrintedLabel key={`b${top}`} x={x + 18} y={79} n={top + 1} />,
          ]
        })}
        <div className="rf-rjblock rf-silver-frame" style={{ left: 352, top: 44, width: 135, height: 31 }}>
          {[13, 14, 15, 16].map((n, i) => (
            <Rj45Jack key={n} x={1 + i * 33} y={1} leds={[]} hit={k.hit(n)} />
          ))}
        </div>
        {[13, 14, 15, 16].map((n, i) => (
          <PrintedLabel key={`l${n}`} x={352 + 1 + i * 33 + 10} y={79} n={n} />
        ))}
        <Reset x={498} y={63} />
      </div>
    </div>
  )
}
