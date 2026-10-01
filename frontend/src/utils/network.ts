import type { PortView } from '@/hooks/usePorts'

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

/** "Port 51 · Uplink To Quantum Gate". */
export function portTitle(p: { number: number; alias?: string }): string {
  return p.alias ? `Port ${p.number} · ${p.alias}` : `Port ${p.number}`
}

/** The busier direction's utilisation, or null. */
export function busiestUtil(p: Pick<PortView, 'in_util_pct' | 'out_util_pct'>): number | null {
  if (p.in_util_pct == null && p.out_util_pct == null) return null
  return Math.max(p.in_util_pct ?? 0, p.out_util_pct ?? 0)
}
