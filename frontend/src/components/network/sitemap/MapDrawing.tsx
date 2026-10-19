import { useMemo } from 'react'
import type { DevicePorts, UnitFaceplate } from '@/hooks/usePorts'
import { DRAWINGS } from '@/components/network/drawings/registry'
import { unitPorts } from '@/components/network/drawings/kit'
import { offline } from '@/utils/faceplateLights'
import '@/components/network/drawings/drawings.css'

/** The units of a device that have a realistic drawing. */
// eslint-disable-next-line react-refresh/only-export-components
export function drawnUnits(ports: DevicePorts | null): UnitFaceplate[] {
  return (ports?.faceplates ?? []).filter((u) => u.drawing && DRAWINGS[u.drawing])
}

interface Props {
  ports: DevicePorts
  scale: number
  isPublic: boolean
  /** A port clicked on the drawing. */
  onPort: (ifIndex: number) => void
}

/** A device's front panel on the map, scaled for its tier, with live port
 *  lights. Each port carries data-if-index, which links attach to. */
export default function MapDrawing({ ports, scale, isPublic, onPort }: Props) {
  const units = drawnUnits(ports)
  const state = offline(ports.device_state, new Date(), isPublic)
  const lastKnown = state.dark ? (state.since ?? '') : undefined
  const mapped = useMemo(() => units.map((u) => ({ u, ...unitPorts(u, ports.ports) })), [units, ports.ports])
  return (
    <div className="flex flex-col gap-1">
      {mapped.map(({ u, byNumber, modules }) => {
        const entry = DRAWINGS[u.drawing!]
        const Drawing = entry.component
        return (
          <div key={u.unit} style={{ width: entry.width * scale, height: entry.height * scale }}>
            <div className="rf" style={{ width: entry.width, height: entry.height, transform: `scale(${scale})`, transformOrigin: '0 0' }}>
              <Drawing
                unit={u}
                byNumber={byNumber}
                modules={modules}
                dark={state.dark}
                indicators={ports.indicators}
                selected={null}
                onSelect={onPort}
                lastKnown={lastKnown}
              />
            </div>
          </div>
        )
      })}
    </div>
  )
}
