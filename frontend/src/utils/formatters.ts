import { format } from 'date-fns'

/** "99.87%" */
export function formatUptimePercent(percent: number): string {
  return `${percent.toFixed(2)}%`
}

/** Format a duration given in minutes, e.g. 90 -> "1h 30m". */
export function formatDowntime(minutes: number): string {
  if (minutes <= 0) return '0m'
  const h = Math.floor(minutes / 60)
  const m = Math.round(minutes % 60)
  const parts: string[] = []
  if (h > 0) parts.push(`${h}h`)
  if (m > 0 || parts.length === 0) parts.push(`${m}m`)
  return parts.join(' ')
}

/** "145ms" */
export function formatResponseTime(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return '—'
  return `${Math.round(ms)}ms`
}

/** Human-readable status with an indicator, e.g. "🟢 Online". */
export function formatStatus(status: string): string {
  switch (status) {
    case 'online':
      return '🟢 Online'
    case 'offline':
      return '🔴 Offline'
    case 'unknown':
      return '⚪ Unknown'
    default:
      return status
  }
}

/** "Jan 15, 2024" */
export function formatDate(date: Date | string): string {
  const d = typeof date === 'string' ? new Date(date) : date
  return format(d, 'MMM d, yyyy')
}

/** "Jan 15, 2024 10:30 AM" */
export function formatDatetime(date: Date | string): string {
  const d = typeof date === 'string' ? new Date(date) : date
  return format(d, 'MMM d, yyyy h:mm a')
}

/** Format a duration given in seconds, e.g. 5445 -> "1h 30m 45s". */
export function formatDuration(seconds: number): string {
  if (seconds <= 0) return '0s'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  const parts: string[] = []
  if (h > 0) parts.push(`${h}h`)
  if (m > 0) parts.push(`${m}m`)
  if (s > 0 || parts.length === 0) parts.push(`${s}s`)
  return parts.join(' ')
}

/** Tailwind text-color class for a status. */
export function getStatusColor(status: string): string {
  switch (status) {
    case 'online':
    case 'success':
      return 'text-emerald-500'
    case 'offline':
    case 'failed':
    case 'timeout':
      return 'text-red-500'
    default:
      return 'text-slate-400'
  }
}

/** Tailwind background-color class for a status badge. */
export function getStatusBgColor(status: string): string {
  switch (status) {
    case 'online':
    case 'success':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'offline':
    case 'failed':
    case 'timeout':
      return 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
    default:
      return 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300'
  }
}
