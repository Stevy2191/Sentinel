import type { SeriesLine, WidgetType } from '@/types/dashboards'
import type { Unit } from '@/components/network/TrafficChart'
import { formatBps } from '@/utils/network'

export const GRID_COLS = 12
export const ROW_HEIGHT = 80
export const GRID_MARGIN: [number, number] = [12, 12]

export const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

export interface WidgetTypeInfo {
  type: WidgetType
  label: string
  description: string
  w: number
  h: number
  minW: number
  minH: number
  timeBased: boolean
}

/** The widget types, in the order the picker lists them. */
export const WIDGET_TYPES: WidgetTypeInfo[] = [
  { type: 'label', label: 'Label', description: 'A heading or a note, e.g. "Main Campus".', w: 4, h: 1, minW: 2, minH: 1, timeBased: false },
  { type: 'timeseries', label: 'Time series', description: "A chart of up to 10 metrics, or a site's internet traffic.", w: 6, h: 4, minW: 3, minH: 3, timeBased: true },
  { type: 'stat', label: 'Stat', description: 'One number, amber or red past its thresholds.', w: 3, h: 2, minW: 2, minH: 2, timeBased: true },
  { type: 'port_grid', label: 'Port grid', description: "A device's faceplate with live port status.", w: 8, h: 3, minW: 4, minH: 2, timeBased: false },
  { type: 'device_health', label: 'Device health', description: 'CPU, temperatures, fans and power supplies.', w: 6, h: 4, minW: 3, minH: 2, timeBased: false },
  { type: 'site_power', label: 'Site power', description: 'Every UPS in a site at a glance.', w: 4, h: 4, minW: 3, minH: 2, timeBased: false },
  { type: 'top_n', label: 'Top ports', description: 'The busiest ports, or those with the most errors.', w: 6, h: 4, minW: 3, minH: 3, timeBased: true },
  { type: 'event_log', label: 'Event log', description: 'Recent port events and incidents.', w: 6, h: 4, minW: 3, minH: 3, timeBased: false },
  { type: 'device_table', label: 'Device table', description: "A site's devices and how they are doing.", w: 12, h: 5, minW: 6, minH: 3, timeBased: false },
  { type: 'open_incidents', label: 'Open incidents', description: 'What is down or degraded right now.', w: 6, h: 4, minW: 3, minH: 2, timeBased: false },
  { type: 'monitors', label: 'Monitors', description: 'Websites, services and servers, up or down.', w: 6, h: 4, minW: 3, minH: 2, timeBased: false },
]

const UNKNOWN: WidgetTypeInfo = { type: 'label', label: 'Widget', description: '', w: 4, h: 2, minW: 1, minH: 1, timeBased: false }

export function widgetInfo(t: WidgetType): WidgetTypeInfo {
  return WIDGET_TYPES.find((w) => w.type === t) ?? UNKNOWN
}

export function isTimeBased(t: WidgetType): boolean {
  return widgetInfo(t).timeBased
}

/** A widget being edited. key is its id, or "new-N" until it is saved. */
export interface DraftWidget {
  key: string
  id?: string
  type: WidgetType
  title: string
  config: Record<string, unknown>
  x: number
  y: number
  w: number
  h: number
}

/** Top to bottom, left to right: the phone-width order. */
export function stackOrder<T extends { x: number; y: number }>(items: T[]): T[] {
  return [...items].sort((a, b) => a.y - b.y || a.x - b.x)
}

/** The first free row below every widget. */
export function nextY(items: { y: number; h: number }[]): number {
  return items.reduce((max, w) => Math.max(max, w.y + w.h), 0)
}

/** Older than three refresh periods: the screen must not look healthy just
 *  because it stopped updating. */
export function isStale(lastSuccess: number | null, refreshSeconds: number, now: number): boolean {
  if (lastSuccess == null || refreshSeconds <= 0) return false
  return now - lastSuccess > 3 * refreshSeconds * 1000
}

/** Lines merged into chart rows: { t: epoch ms, [line key]: value }. */
export function chartRows(lines: SeriesLine[]): Record<string, number>[] {
  const byTime = new Map<number, Record<string, number>>()
  for (const l of lines) {
    for (const p of l.points ?? []) {
      const t = new Date(p.t).getTime()
      const row = byTime.get(t) ?? { t }
      row[l.key] = p.avg
      byTime.set(t, row)
    }
  }
  return [...byTime.values()].sort((a, b) => a.t - b.t)
}

/** A metric unit as AreaSeriesChart names it. */
export function unitOf(unit: string): Unit {
  switch (unit) {
    case 'bps':
      return 'bps'
    case '%':
      return 'pct'
    case 'per_min':
      return 'per_min'
    case 'min':
      return 'min'
    default:
      return 'custom'
  }
}

/** A value in its unit, the way charts label it. */
export function formatMetric(v: number, unit: string): string {
  switch (unit) {
    case 'bps':
      return formatBps(v)
    case '%':
      return `${Math.round(v)}%`
    case 'per_min':
      return `${+v.toFixed(1)}/min`
    case 'min':
      return `${Math.round(v)} min`
    default:
      return `${+v.toFixed(1)} ${unit}`.trim()
  }
}

/** A new widget's starting config; siteId is the dashboard's site, if any. */
export function defaultConfig(type: WidgetType, siteId: string | null): Record<string, unknown> {
  switch (type) {
    case 'label':
      return { text: 'New label', size: 'm' }
    case 'timeseries':
      return siteId
        ? { source: 'site_traffic', site_id: siteId, view: 'internet', range: '24h' }
        : { source: 'metrics', metrics: [], devices: [], range: '24h' }
    case 'stat':
      return { mode: 'latest', direction: 'above', range: '24h' }
    case 'top_n':
      return siteId ? { site_id: siteId, measure: 'traffic', n: 10, range: '24h' } : { devices: [], measure: 'traffic', n: 10, range: '24h' }
    case 'event_log':
      return siteId ? { site_id: siteId, limit: 20 } : { limit: 20 }
    case 'open_incidents':
      return siteId ? { scope: 'site', site_id: siteId, limit: 20 } : { scope: 'all', limit: 20 }
    case 'monitors':
      return { monitors: [], agents: [], style: 'list', window: '24h' }
    case 'site_power':
    case 'device_table':
      return siteId ? { site_id: siteId } : {}
    default:
      return {}
  }
}

/** A one-line description of a draft widget's config, for the editor grid. */
export function widgetSummary(w: DraftWidget): string {
  const c = w.config
  const count = (k: string) => (Array.isArray(c[k]) ? (c[k] as unknown[]).length : 0)
  switch (w.type) {
    case 'label':
      return String(c.text ?? '')
    case 'timeseries':
      return c.source === 'site_traffic' ? `Site ${c.view === 'east_west' ? 'inside traffic' : 'internet traffic'}` : `${count('metrics')} metric(s) · ${c.site_id ? 'site total' : `${count('devices')} device(s)`}`
    case 'monitors':
      return `${count('monitors')} monitor(s), ${count('agents')} server(s) · ${c.style === 'bars' ? 'uptime bars' : 'list'}`
    case 'open_incidents':
      return `Scope: ${String(c.scope ?? 'all')}`
    default:
      return widgetInfo(w.type).description
  }
}

/** Reads a string field of a widget config. */
export function str(c: Record<string, unknown>, k: string): string {
  return typeof c[k] === 'string' ? (c[k] as string) : ''
}

/** Reads a string-list field of a widget config. */
export function strs(c: Record<string, unknown>, k: string): string[] {
  return Array.isArray(c[k]) ? (c[k] as unknown[]).filter((v): v is string => typeof v === 'string') : []
}

/** Reads a number field of a widget config, or def. */
export function num(c: Record<string, unknown>, k: string, def: number): number {
  return typeof c[k] === 'number' ? (c[k] as number) : def
}

/** What every widget settings form takes. siteId is the dashboard's site. */
export interface SettingsProps {
  config: Record<string, unknown>
  siteId: string | null
  onChange: (config: Record<string, unknown>) => void
}
