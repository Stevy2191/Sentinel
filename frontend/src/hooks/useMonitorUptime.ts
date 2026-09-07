import { useEffect, useRef, useState } from 'react'
import api from '@/services/api'
import type { ApiResponse } from '@/types'

export type HourStatus = 'up' | 'down' | 'partial' | 'nodata'

export interface HourPoint {
  hour: number // clock hour 0-23 of the bucket (UTC)
  uptime: number // 0-100, from the checks recorded in the hour
  status: HourStatus

  // Incident- and maintenance-derived view of the same hour, used by the
  // 24-hour health bar. Timestamps are RFC3339 UTC; null means "none in this
  // hour". down_start/down_end bound the hour's downtime, so a tooltip can name
  // the minutes a service was actually unavailable.
  bucket_start: string
  down_seconds: number
  maintenance_seconds: number
  down_start: string | null
  down_end: string | null
  maintenance_start: string | null
  maintenance_end: string | null
  observed: boolean // false for hours before the monitor was created
}

/** One individual check, as drawn by the dashboard's per-check strip. */
export interface RecentCheck {
  status: 'success' | 'failed' | 'timeout'
  response_time_ms: number
  status_code: number
  error_message: string
  timestamp: string // RFC3339 UTC
}

export interface ResponsePoint {
  time: string // "HH:00"
  responseTime: number // ms (0 when no data)
}

export interface UptimeHistory {
  uptime_24h: number
  uptime_7d: number
  uptime_30d: number
  hourly_data: HourPoint[]
  response_time_data: ResponsePoint[]
  // The last N checks, oldest first, with no time bound at all — so the strip
  // stays populated for a monitor that checks hourly or has been paused.
  recent_checks: RecentCheck[]
  // Pass rate across exactly those checks. null when the monitor has never run:
  // "no checks yet" is not the same as "every check failed".
  recent_uptime: number | null
}

export type UptimeRange = '24h' | '7d' | '30d'

/**
 * useMonitorUptime fetches a monitor's consolidated uptime history (three uptime
 * windows, a 24-bucket hourly sparkline series, a 24h response-time series, and
 * the last N individual checks) in one request. `enabled` gates the fetch so
 * callers can defer it.
 *
 * `refreshKey` re-runs the fetch whenever it changes. The dashboard passes its
 * own refresh timestamp, so the per-check strip keeps pace with the monitor
 * list instead of showing whatever was true when the row first mounted — rows
 * are keyed by monitor id and so are never remounted by a refresh.
 */
export function useMonitorUptime(
  monitorID: string,
  range: UptimeRange = '24h',
  enabled = true,
  refreshKey?: unknown
) {
  const [data, setData] = useState<UptimeHistory | null>(null)
  const [loading, setLoading] = useState(enabled)
  // Tracked in a ref, not derived from `data`, so deciding whether to show the
  // skeleton stays out of the render path.
  const loadedOnce = useRef(false)

  useEffect(() => {
    if (!enabled) return
    let active = true
    // Only the first load blanks the view. A background refresh keeps the
    // current strip on screen rather than flashing it back to skeletons.
    if (!loadedOnce.current) setLoading(true)
    api
      .get<ApiResponse<UptimeHistory>>(`/monitors/${monitorID}/uptime-history`, { params: { range } })
      .then((r) => active && setData(r.data.data))
      .catch(() => active && setData(null))
      .finally(() => {
        if (!active) return
        loadedOnce.current = true
        setLoading(false)
      })
    return () => {
      active = false
    }
  }, [monitorID, range, enabled, refreshKey])

  return { data, loading }
}
