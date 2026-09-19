
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

/**
 * Formats the response time of a monitor's last check, given its status.
 *
 * A check that got no reply stores 0 rather than null, so formatting the
 * number alone renders "0ms" — an impossibly fast response — for a monitor
 * that is in fact unreachable. Nothing was measured, so nothing is shown.
 *
 * The zero is only read as "no reply" when the monitor is down; a genuinely
 * sub-millisecond check on a local target rounds to 0 too, and that one really
 * did respond.
 */
export function formatLastResponseTime(
  ms: number | null | undefined,
  status: string | null | undefined,
): string {
  if (status === 'offline' && (ms === 0 || ms === null || ms === undefined)) return '—'
  return formatResponseTime(ms)
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

/**
 * The zone every timestamp in the UI is displayed in — the instance's report
 * timezone, so the screen and a rendered report describe the same clock.
 *
 * Module state set once by AppConfigContext rather than a parameter on each
 * call: these formatters are used from well over a hundred places, and threading
 * a zone through all of them would be a lot of churn for a value that is global
 * by definition. Undefined means "not loaded yet" and falls back to the
 * browser's zone, which is what the whole UI did before.
 */
let displayTimeZone: string | undefined

/** Sets the display zone. Called by AppConfigContext when config loads. */
export function setDisplayTimezone(tz: string | undefined): void {
  if (!tz) {
    displayTimeZone = undefined
    return
  }
  // Verified before being adopted: an unknown zone makes Intl throw on every
  // subsequent call, which would take out every timestamp in the app.
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: tz })
    displayTimeZone = tz
  } catch {
    displayTimeZone = undefined
  }
}

/** The zone currently used for display, or the browser's when none is set. */
export function getDisplayTimezone(): string {
  return displayTimeZone ?? Intl.DateTimeFormat().resolvedOptions().timeZone
}

function formatIn(date: Date | string, options: Intl.DateTimeFormatOptions): string {
  const d = typeof date === 'string' ? new Date(date) : date
  if (Number.isNaN(d.getTime())) return '—'
  return new Intl.DateTimeFormat('en-US', { ...options, timeZone: displayTimeZone }).format(d)
}

/** "Jan 15, 2024" */
export function formatDate(date: Date | string): string {
  return formatIn(date, { month: 'short', day: 'numeric', year: 'numeric' })
}

/** "Jan 15, 2024, 10:30 AM" */
export function formatDatetime(date: Date | string): string {
  return formatIn(date, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  })
}

/** "Jan 15, 2024, 10:30 AM CST" — used where the zone itself matters. */
export function formatDatetimeWithZone(date: Date | string): string {
  return formatIn(date, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    timeZoneName: 'short',
  })
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
