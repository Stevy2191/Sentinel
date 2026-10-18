import type { ComponentType } from 'react'
import type { PortView, UnitFaceplate } from '@/hooks/usePorts'
import { flicker, portLight, type FaceplateIndicators, type Flicker, type Light } from '@/utils/faceplateLights'
import { portState } from '@/utils/network'
import type { CagePort, Hit } from '@/components/network/drawings/parts'

/** What a model's drawing is given. dark: Sentinel isn't hearing from the
 *  device, so every light is off; lastKnown is then the time of its last
 *  port data ("" when unknown), and undefined otherwise. */
export interface DrawingProps {
  unit: UnitFaceplate
  byNumber: Map<number, PortView>
  dark: boolean
  indicators?: FaceplateIndicators
  selected: number | null
  onSelect: (ifIndex: number) => void
  lastKnown: string | null | undefined
}

export interface DrawingEntry {
  component: ComponentType<DrawingProps>
  /** The drawing's size at 1×, in CSS pixels. */
  width: number
  height: number
}

/** The helpers every drawing uses to look up a port and its lights. */
export function kit(p: DrawingProps) {
  const port = (n: number) => p.byNumber.get(n)
  const alert = (n: number): Hit['alert'] => {
    const pv = port(n)
    if (p.dark || !pv) return undefined
    const s = portState(pv)
    return s === 'critical' ? 'red' : s === 'warning' ? 'yellow' : undefined
  }
  const hit = (n: number): Hit => ({ port: port(n), selected: p.selected, onSelect: p.onSelect, lastKnown: p.lastKnown, alert: alert(n) })
  const light = (n: number, opts?: { tenGigWhite?: boolean }): Light => (p.dark ? 'off' : portLight(port(n), opts))
  const fl = (n: number): Flicker => (p.dark ? '' : flicker(port(n)))
  const cage = (n: number): CagePort => ({ hit: hit(n), light: light(n), flicker: fl(n) })
  return { port, hit, light, fl, cage }
}
