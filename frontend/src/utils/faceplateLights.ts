import type { PortView } from '@/hooks/usePorts'
import { busiestUtil, portState } from '@/utils/network'
import { DEFAULTS, getString, PREF } from '@/utils/preferences'

/** A light on a realistic drawing. */
export type Light = 'green' | 'white' | 'yellow' | 'red' | 'off'
/** How a lit light flickers: not at all, or slow / fast with traffic. */
export type Flicker = '' | 'slow' | 'fast'

/** The device's reachability, as the ports view serves it. */
export interface FaceplateDeviceState {
  status: string
  status_detail: string
  last_seen_at: string | null
  last_stats_at: string | null
  poll_interval: number
}

/** A drawing's lights that are not ports. */
export interface FaceplateIndicators {
  drive_bay?: string
}

/** A port's light: Sentinel's state colour; on a speed panel (the ES-16-XG)
 *  a 10G link is white, as the real legend shows. */
export function portLight(p: PortView | undefined, opts?: { tenGigWhite?: boolean }): Light {
  if (!p) return 'off'
  switch (portState(p)) {
    case 'warning':
      return 'yellow'
    case 'critical':
      return 'red'
    case 'up':
      return opts?.tenGigWhite && p.speed_bps >= 10_000_000_000 ? 'white' : 'green'
    default:
      return 'off'
  }
}

/** A lit port flickers while it carries traffic, faster when over half full. */
export function flicker(p: PortView | undefined): Flicker {
  if (!p || portState(p) === 'down' || portState(p) === 'disabled') return ''
  const u = busiestUtil(p)
  if (u == null) return ''
  if (u > 50) return 'fast'
  if (u > 0.5) return 'slow'
  return ''
}

/** "10:42" (or "10:42 AM"), in the user's time format. */
export function clockTime(iso: string): string {
  const twelve = getString(PREF.timeFormat, DEFAULTS.timeFormat) === '12h'
  return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: twelve })
}

/** Whether Sentinel is not hearing from the device, and the badge saying
 *  why. dark means every light on the drawing is off; since is when its
 *  port data was last read, for the popover's "Last known" header. */
export function offline(
  s: FaceplateDeviceState | undefined,
  now: Date,
  isPublic = false
): { dark: boolean; badge: string | null; since: string | null } {
  if (!s) return { dark: false, badge: null, since: null }
  const since = s.last_stats_at ? clockTime(s.last_stats_at) : null
  switch (s.status) {
    case 'up':
      break
    case 'down':
      return { dark: true, badge: s.last_seen_at ? `Not responding since ${clockTime(s.last_seen_at)}` : 'Not responding', since }
    case 'paused':
      return { dark: true, badge: 'Paused', since }
    case 'error':
      return { dark: true, badge: isPublic || !s.status_detail ? 'Error' : `Error: ${s.status_detail}`, since }
    default:
      return { dark: true, badge: 'Waiting for the first poll', since }
  }
  if (!s.last_stats_at) return { dark: true, badge: 'No port data yet', since: null }
  const stale = now.getTime() - new Date(s.last_stats_at).getTime() > 3 * Math.max(s.poll_interval, 1) * 1000
  return stale ? { dark: true, badge: `No port data since ${since}`, since } : { dark: false, badge: null, since: null }
}

/** The UDM-SE drive bay light from its state. */
export function driveBayLight(state: string | undefined): { light: Light; flicker: Flicker; title: string } {
  switch (state) {
    case 'active':
      return { light: 'white', flicker: 'slow', title: '3.5″ drive bay: drive installed, active' }
    case 'present':
      return { light: 'white', flicker: '', title: '3.5″ drive bay: drive installed' }
    case 'absent':
      return { light: 'off', flicker: '', title: '3.5″ drive bay: empty' }
    default:
      return { light: 'off', flicker: '', title: "3.5″ drive bay: Sentinel can't read this drive's status" }
  }
}
