import type { PortView } from '@/hooks/usePorts'
import type { PortRole } from '@/hooks/useDevices'

/** "940 Mb/s", "1.2 Gb/s", "12 kb/s"; "—" when unknown. */
export function formatBps(bps: number | null | undefined): string {
  if (bps == null || Number.isNaN(bps)) return '—'
  if (bps >= 1e9) return `${+(bps / 1e9).toFixed(2)} Gb/s`
  if (bps >= 1e6) return `${+(bps / 1e6).toFixed(1)} Mb/s`
  if (bps >= 1e3) return `${+(bps / 1e3).toFixed(0)} kb/s`
  return `${Math.round(bps)} b/s`
}

export function formatPct(v: number | null | undefined): string {
  if (v == null) return '—'
  return v < 1 && v > 0 ? '<1%' : `${Math.round(v)}%`
}

export const WARNING_CONDITIONS = ['errors', 'flapping', 'slow_link', 'saturated']

export type PortState = 'critical' | 'warning' | 'up' | 'down' | 'disabled'

/** The faceplate colour rule: disabled, then important-and-down (red), then
 *  down (grey), then any warning condition (yellow), else in use (green). */
export function portState(p: { admin_status: string; oper_status: string; important: boolean; conditions: string[] }): PortState {
  if (p.admin_status === 'down') return 'disabled'
  if (p.oper_status !== 'up') return p.important ? 'critical' : 'down'
  if ((p.conditions ?? []).some((c) => WARNING_CONDITIONS.includes(c))) return 'warning'
  return 'up'
}

// Literal class strings (Tailwind only emits classes it finds spelled out).
export const PORT_STATE: Record<PortState, { label: string; fill: string; dot: string; badge: string }> = {
  critical: { label: 'Important port down', fill: 'bg-red-500', dot: 'bg-red-500', badge: 'border-red-500/30 bg-red-500/15 text-red-400' },
  warning: { label: 'Problem', fill: 'bg-yellow-500', dot: 'bg-yellow-500', badge: 'border-yellow-500/30 bg-yellow-500/15 text-yellow-400' },
  up: { label: 'In use', fill: 'bg-emerald-500', dot: 'bg-emerald-500', badge: 'border-emerald-500/30 bg-emerald-500/15 text-emerald-400' },
  down: { label: 'Nothing plugged in', fill: 'bg-slate-600', dot: 'bg-slate-500', badge: 'border-slate-500/30 bg-slate-500/15 text-slate-300' },
  disabled: { label: 'Disabled', fill: 'bg-slate-800 ring-1 ring-inset ring-slate-600', dot: 'bg-slate-700', badge: 'border-slate-600/40 bg-slate-800 text-slate-400' },
}

export const CONDITION_LABEL: Record<string, string> = {
  link_down: 'Link down',
  errors: 'Errors rising',
  flapping: 'Flapping',
  slow_link: 'Slower link than usual',
  saturated: 'Nearly full',
  ups_on_battery: 'On battery',
  ups_low_battery: 'Low battery',
  ups_high_load: 'High load',
  metric: 'Metric rule',
}

/** "1 min ago", "3 h ago", or the date once it's more than a day old; "never"
 *  for null. Used for a profile's last poll time on the device Health panel. */
export function timeAgo(iso: string | null): string {
  if (!iso) return 'never'
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 90) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)} min ago`
  if (s < 86400) return `${Math.round(s / 3600)} h ago`
  return new Date(iso).toLocaleDateString()
}

export const EVENT_LABEL: Record<string, string> = {
  link_up: 'Link up',
  link_down: 'Link down',
  flapping: 'Flapping',
  speed_change: 'Speed changed',
  errors: 'Errors rising',
  saturated: 'Nearly full',
  slow_link: 'Slower link',
  admin_up: 'Enabled',
  admin_down: 'Disabled',
}

/** What a port's role means, in the "Connects to" select and the port table. */
export const PORT_ROLE_LABEL: Record<PortRole, string> = {
  access: 'Endpoint',
  uplink: 'Network link',
  wan: 'Internet (WAN)',
}

/** The one-line description shown under each role option. */
export const PORT_ROLE_HINT: Record<PortRole, string> = {
  access: 'A camera, PC or AP plugged in.',
  uplink: 'Goes to another of your network devices.',
  wan: 'The internet connection.',
}

export const PORT_ROLES: PortRole[] = ['access', 'uplink', 'wan']

/** "Port 51 · Uplink To Quantum Gate", or "Switch 2 · Port 12 · alias" for a
 *  stacked port (stack_unit > 0). */
export function portTitle(p: { number: number; label?: string; alias?: string; stack_unit?: number }): string {
  const name = p.label || String(p.number)
  const base = p.stack_unit && p.stack_unit > 0 ? `Switch ${p.stack_unit} · Port ${name}` : `Port ${name}`
  return p.alias ? `${base} · ${p.alias}` : base
}

export interface TrafficRow {
  label: string
  value: string
}

/** The hover box's Download/Upload (or Received/Sent) and Busy % rows,
 *  labelled by role and direction from the point of view of what is plugged
 *  in. A row whose value is unknown is omitted. */
export function portTraffic(p: Pick<PortView, 'role' | 'in_bps' | 'out_bps' | 'in_util_pct' | 'out_util_pct'>): TrafficRow[] {
  let downLabel = 'Download'
  let upLabel = 'Upload'
  let down = p.out_bps
  let up = p.in_bps
  if (p.role === 'wan') {
    down = p.in_bps
    up = p.out_bps
  } else if (p.role === 'uplink') {
    downLabel = 'Received'
    upLabel = 'Sent'
    down = p.in_bps
    up = p.out_bps
  }
  const rows: TrafficRow[] = []
  if (down != null) rows.push({ label: `↓ ${downLabel}`, value: formatBps(down) })
  if (up != null) rows.push({ label: `↑ ${upLabel}`, value: formatBps(up) })
  const busy = busiestUtil(p)
  if (busy != null) rows.push({ label: '▮ Busy %', value: formatPct(busy) })
  return rows
}

/** The busier direction's utilisation, or null. */
export function busiestUtil(p: Pick<PortView, 'in_util_pct' | 'out_util_pct'>): number | null {
  if (p.in_util_pct == null && p.out_util_pct == null) return null
  return Math.max(p.in_util_pct ?? 0, p.out_util_pct ?? 0)
}

/** "Download 412 Mb/s · Upload 88 Mb/s · Busy 41%", the plain-text form of
 *  portTraffic for aria-label (the hover box itself renders the rows, where
 *  the "Busy %" label and the "41%" value sit in separate cells — here
 *  they're joined into one phrase, so the row's own trailing "%" is dropped
 *  to avoid "Busy % 41%"). */
export function portTrafficText(p: Pick<PortView, 'role' | 'in_bps' | 'out_bps' | 'in_util_pct' | 'out_util_pct'>): string {
  return portTraffic(p)
    .map((r) => `${r.label.replace(/^[↓↑▮]\s*/, '').replace(/\s*%$/, '')} ${r.value}`)
    .join(' · ')
}
