import { format, formatDistanceToNow, parseISO } from 'date-fns'
import type { MonitorStatus } from '@/types'

/** Format an ISO timestamp as a readable absolute date-time. */
export function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return '—'
  try {
    return format(parseISO(iso), 'MMM d, yyyy HH:mm:ss')
  } catch {
    return iso
  }
}

/** Format an ISO timestamp as a relative "x ago" string. */
export function formatRelative(iso: string | null | undefined): string {
  if (!iso) return 'never'
  try {
    return formatDistanceToNow(parseISO(iso), { addSuffix: true })
  } catch {
    return iso
  }
}

/** Format a response time in milliseconds. */
export function formatMs(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return '—'
  return `${ms}ms`
}

/** Tailwind text-color class for a monitor status. */
export function statusColor(status: MonitorStatus): string {
  switch (status) {
    case 'online':
      return 'text-primary-600 dark:text-primary-400'
    case 'offline':
      return 'text-error-600 dark:text-error-400'
    default:
      return 'text-neutral-500 dark:text-neutral-400'
  }
}

/** Tailwind badge classes for a monitor status. */
export function statusBadge(status: MonitorStatus): string {
  switch (status) {
    case 'online':
      return 'bg-primary-100 text-primary-700 dark:bg-primary-900/40 dark:text-primary-300'
    case 'offline':
      return 'bg-error-100 text-error-700 dark:bg-error-900/40 dark:text-error-300'
    default:
      return 'bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300'
  }
}
