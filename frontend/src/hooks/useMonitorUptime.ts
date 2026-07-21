import { useEffect, useState } from 'react'
import api from '@/services/api'
import type { ApiResponse } from '@/types'

export type HourStatus = 'up' | 'down' | 'partial' | 'nodata'

export interface HourPoint {
  hour: number // clock hour 0-23 of the bucket
  uptime: number // 0-100
  status: HourStatus
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
}

export type UptimeRange = '24h' | '7d' | '30d'

/**
 * useMonitorUptime fetches a monitor's consolidated uptime history (three uptime
 * windows + a 24-bucket hourly sparkline series + a 24h response-time series) in
 * one request. `enabled` gates the fetch so callers can defer it.
 */
export function useMonitorUptime(monitorID: string, range: UptimeRange = '24h', enabled = true) {
  const [data, setData] = useState<UptimeHistory | null>(null)
  const [loading, setLoading] = useState(enabled)

  useEffect(() => {
    if (!enabled) return
    let active = true
    setLoading(true)
    api
      .get<ApiResponse<UptimeHistory>>(`/monitors/${monitorID}/uptime-history`, { params: { range } })
      .then((r) => active && setData(r.data.data))
      .catch(() => active && setData(null))
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [monitorID, range, enabled])

  return { data, loading }
}
