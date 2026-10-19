// The one status scale monitors, servers and devices share, the summary
// counts over all three, and the Monitoring page's address. The list itself
// (rows, filters, sorting) is in monitoringList.ts. Only type imports from
// `@/`, so it can be checked on its own.
import type { Monitor } from '@/types'
import type { Agent } from '@/hooks/useAgents'
import type { Device } from '@/hooks/useDevices'

export type Section = 'uptime' | 'servers' | 'devices'

export type ViewStatus = 'down' | 'up' | 'pending' | 'paused' | 'maintenance' | 'error'
export const VIEW_STATUS_LABEL: Record<ViewStatus, string> = {
  down: 'Down',
  up: 'Up',
  pending: 'Pending',
  paused: 'Paused',
  maintenance: 'Maintenance',
  error: 'Error',
}

/** monitorState places a monitor on the shared scale. Paused and maintenance
 *  win over the last check, as the Uptime page has always shown them. */
export function monitorState(m: Pick<Monitor, 'enabled' | 'is_in_maintenance' | 'current_status'>): ViewStatus {
  if (!m.enabled) return 'paused'
  if (m.is_in_maintenance) return 'maintenance'
  if (m.current_status === 'online') return 'up'
  if (m.current_status === 'offline') return 'down'
  return 'pending'
}

export function serverState(a: Pick<Agent, 'status'>): ViewStatus {
  if (a.status === 'active') return 'up'
  if (a.status === 'offline') return 'down'
  return 'pending'
}

export function deviceState(d: Pick<Device, 'status'>): ViewStatus {
  return d.status
}

export interface SummaryCounts {
  watching: number
  down: number
  paused: number
}

/** summaryCounts is the strip under the toolbar, over everything loaded.
 *  Error and Pending are not Down; servers cannot be paused. */
export function summaryCounts(monitors: Monitor[], agents: Agent[], devices: Device[]): SummaryCounts {
  let down = 0
  let paused = 0
  for (const m of monitors) {
    const s = monitorState(m)
    if (s === 'down') down++
    else if (s === 'paused') paused++
  }
  for (const a of agents) if (serverState(a) === 'down') down++
  for (const d of devices) {
    const s = deviceState(d)
    if (s === 'down') down++
    else if (s === 'paused') paused++
  }
  return { watching: monitors.length + agents.length + devices.length, down, paused }
}

/** monitoringPath is the Monitoring list, optionally narrowed to one kind. */
export function monitoringPath(show?: Section): string {
  return show ? `/monitoring?kind=${show}` : '/monitoring'
}
