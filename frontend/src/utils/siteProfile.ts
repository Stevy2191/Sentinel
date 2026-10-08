// What the site page and the sites list compute from a site's profile and the
// lists the Monitoring page loads. Only type imports from `@/` and relative
// value imports, so it can be checked on its own.
import type { Monitor } from '@/types'
import type { Agent } from '@/hooks/useAgents'
import type { Device } from '@/hooks/useDevices'
import type { CircuitKind } from '@/hooks/useSiteProfile'
import { summaryCounts, type ViewStatus } from './monitoringView'

export const CIRCUIT_KINDS: { value: CircuitKind; label: string }[] = [
  { value: 'fiber', label: 'Fiber' },
  { value: 'cable', label: 'Cable' },
  { value: 'dsl', label: 'DSL' },
  { value: 'fixed_wireless', label: 'Fixed wireless' },
  { value: 'cellular', label: 'Cellular' },
  { value: 'copper', label: 'T1/copper' },
  { value: 'other', label: 'Other' },
]

export function circuitKindLabel(kind: string): string {
  return CIRCUIT_KINDS.find((k) => k.value === kind)?.label ?? 'Other'
}

const mb = (v: number) => `${+v.toFixed(2)}`

/** "500/500 Mb", "300 Mb down", "20 Mb up", or '' with no speeds. */
export function circuitSpeed(c: { download_mbps: number | null; upload_mbps: number | null }): string {
  const d = c.download_mbps
  const u = c.upload_mbps
  if (d != null && u != null) return `${mb(d)}/${mb(u)} Mb`
  if (d != null) return `${mb(d)} Mb down`
  if (u != null) return `${mb(u)} Mb up`
  return ''
}

export interface Usage {
  /** The rate as a share of the speed (may pass 100), or null with no speed. */
  pct: number | null
  /** The bar's width, 0-100. */
  bar: number
}

/** usage is a rate against a circuit speed; null when the rate is unknown. */
export function usage(bps: number | null | undefined, mbps: number | null | undefined): Usage | null {
  if (bps == null) return null
  if (!mbps || mbps <= 0) return { pct: null, bar: 0 }
  // Multiplied first so whole shares stay whole (0.42 * 100 is not 42).
  const pct = (bps * 100) / (mbps * 1e6)
  return { pct, bar: Math.max(0, Math.min(100, pct)) }
}

/** scanChoices are the saved networks Scan subnet can offer: IPv4, /22 to /32. */
export function scanChoices<T extends { cidr: string }>(networks: T[]): T[] {
  return networks.filter((n) => {
    const m = /^\d{1,3}(?:\.\d{1,3}){3}\/(\d{1,2})$/.exec(n.cidr)
    return !!m && Number(m[1]) >= 22 && Number(m[1]) <= 32
  })
}

export interface SiteCounts {
  /** null when that list has not loaded (or failed). */
  devices: number | null
  servers: number | null
  checks: number | null
  watched: number
  down: number
}

/** siteCounts counts what is at a site in each loaded list (null for one that
 *  has not loaded). Down uses the shared status scale. */
export function siteCounts(
  siteId: string,
  lists: { monitors: Monitor[] | null; agents: Agent[] | null; devices: Device[] | null }
): SiteCounts {
  const monitors = lists.monitors?.filter((m) => m.site_id === siteId) ?? null
  const agents = lists.agents?.filter((a) => a.site_id === siteId) ?? null
  const devices = lists.devices?.filter((d) => d.site_id === siteId) ?? null
  const s = summaryCounts(monitors ?? [], agents ?? [], devices ?? [])
  return {
    devices: devices?.length ?? null,
    servers: agents?.length ?? null,
    checks: monitors?.length ?? null,
    watched: s.watching,
    down: s.down,
  }
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

/** "18 devices · 3 servers · 9 checks", leaving out lists that did not load. */
export function siteCountsText(c: SiteCounts): string {
  const parts: string[] = []
  if (c.devices != null) parts.push(plural(c.devices, 'device', 'devices'))
  if (c.servers != null) parts.push(plural(c.servers, 'server', 'servers'))
  if (c.checks != null) parts.push(plural(c.checks, 'check', 'checks'))
  return parts.join(' · ')
}

/** The dot colour for each state on the shared scale. */
export const STATE_DOT: Record<ViewStatus, string> = {
  up: 'bg-emerald-400',
  down: 'bg-red-400',
  pending: 'bg-amber-300',
  paused: 'bg-slate-500',
  maintenance: 'bg-sky-400',
  error: 'bg-orange-400',
}

/** The site page's right-column tabs, in order. */
export type SiteTab = 'devices' | 'servers' | 'checks' | 'traffic'
export const SITE_TABS: SiteTab[] = ['devices', 'servers', 'checks', 'traffic']

/** siteTab reads the tab from the address (?tab=); anything else is Devices. */
export function siteTab(value: string | null): SiteTab {
  return SITE_TABS.find((t) => t === value) ?? 'devices'
}
