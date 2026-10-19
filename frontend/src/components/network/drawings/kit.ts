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
  /** The unit's main ports by the number printed by them. */
  byNumber: Map<number, PortView>
  /** Network module ports: slot, then the number printed by them. */
  modules: Map<number, Map<number, PortView>>
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

/** A unit's ports as its faceplate placed them: main ports by number,
 *  module ports ("Module N" blocks) by slot then number. Going through the
 *  blocks keeps a module's Te1/2/1 from standing in for main port 1, and a
 *  VLAN sub-interface (eth10.10) from standing in for a port. */
export function unitPorts(unit: UnitFaceplate, ports: PortView[]): { byNumber: Map<number, PortView>; modules: Map<number, Map<number, PortView>> } {
  const byIndex = new Map(ports.map((p) => [p.if_index, p]))
  const byNumber = new Map<number, PortView>()
  const modules = new Map<number, Map<number, PortView>>()
  for (const b of unit.blocks) {
    const slot = b.label ? Number(b.label.replace(/^Module /, '')) : 0
    const into = slot ? (modules.get(slot) ?? modules.set(slot, new Map()).get(slot)!) : byNumber
    for (const fp of [...b.top, ...b.bottom]) {
      const p = byIndex.get(fp.if_index)
      if (p) into.set(fp.number, p)
    }
  }
  return { byNumber, modules }
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
  const light = (n: number): Light => (p.dark ? 'off' : portLight(port(n)))
  const fl = (n: number): Flicker => (p.dark ? '' : flicker(port(n)))
  const cage = (n: number): CagePort => ({ hit: hit(n), light: light(n), flicker: fl(n) })
  // The same for a port on network module slot.
  const mport = (slot: number, n: number) => p.modules.get(slot)?.get(n)
  const mcage = (slot: number, n: number): CagePort => {
    const pv = mport(slot, n)
    const s = pv && !p.dark ? portState(pv) : undefined
    return {
      hit: { port: pv, selected: p.selected, onSelect: p.onSelect, lastKnown: p.lastKnown, alert: s === 'critical' ? 'red' : s === 'warning' ? 'yellow' : undefined },
      light: p.dark ? 'off' : portLight(pv),
      flicker: p.dark ? '' : flicker(pv),
    }
  }
  const hasModule = (slot: number) => (p.modules.get(slot)?.size ?? 0) > 0
  return { port, hit, light, fl, cage, mport, mcage, hasModule }
}
