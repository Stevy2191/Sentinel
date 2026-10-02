import type { HealthRow } from '@/hooks/useDeviceHealth'

export const CHIP_TONE = {
  ok: 'border-emerald-500/30 bg-emerald-500/15 text-emerald-300',
  problem: 'border-red-500/30 bg-red-500/15 text-red-300',
  warn: 'border-amber-500/30 bg-amber-500/15 text-amber-300',
} as const

/** green when the row's state is OK, red while an incident is open for it,
 *  else amber (a state outside the OK list, but with no rule raising it). */
export function chipTone(row: HealthRow): keyof typeof CHIP_TONE {
  if (row.problem) return 'problem'
  if (row.ok) return 'ok'
  return 'warn'
}

export function rowValueText(row: HealthRow, units: string): string {
  return `${+row.value.toFixed(1)} ${units}`.trim()
}
