import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export type MetricsRange = '1h' | '6h' | '24h' | '7d' | '30d' | '90d' | '1y'

export const METRICS_RANGES: { value: MetricsRange; label: string }[] = [
  { value: '1h', label: 'Last hour' },
  { value: '6h', label: 'Last 6 hours' },
  { value: '24h', label: 'Last 24 hours' },
  { value: '7d', label: 'Last 7 days' },
  { value: '30d', label: 'Last 30 days' },
  { value: '90d', label: 'Last 90 days' },
  { value: '1y', label: 'Last year' },
]

export interface MetricPoint {
  t: string
  avg: number
  min: number
  max: number
}
export interface MetricSeries {
  device_id: string | null
  metric: string
  instance: string
  points: MetricPoint[]
}
export interface MetricsResult {
  resolution: 'raw' | '5m' | '1h'
  step_seconds: number
  series: MetricSeries[]
}

export interface MetricsQuery {
  deviceIds?: string[]
  siteId?: string
  metrics: string[]
  instances?: string[]
  range: MetricsRange
  /** Add up every selected series per metric (device and site totals). */
  sum?: boolean
  /** With sum: only physical ports, so a link aggregate is not counted twice. */
  physicalOnly?: boolean
}

/** Short ranges refresh every minute; long ones every five. */
function refreshFor(range: MetricsRange): number {
  return range === '1h' || range === '6h' || range === '24h' ? 60_000 : 300_000
}

export function useMetricsQuery(q: MetricsQuery | null) {
  const [result, setResult] = useState<MetricsResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const key = q ? JSON.stringify(q) : ''

  const refetch = useCallback(async () => {
    if (!key) return
    const query = JSON.parse(key) as MetricsQuery
    // Repeated keys (metric=a&metric=b) are what the API reads; axios's
    // default array format (metric[]=a) is not, so the params are built here.
    const params = new URLSearchParams()
    query.metrics.forEach((m) => params.append('metric', m))
    query.deviceIds?.forEach((d) => params.append('device_id', d))
    if (query.siteId) params.set('site_id', query.siteId)
    query.instances?.forEach((i) => params.append('instance', i))
    params.set('range', query.range)
    if (query.sum) params.set('agg', 'sum')
    if (query.physicalOnly) params.set('ports', 'physical')
    try {
      const { data } = await api.get<ApiResponse<MetricsResult>>('/network/metrics/query', { params })
      setResult(data.data)
      setError(null)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load the chart')
    } finally {
      setLoading(false)
    }
  }, [key])

  useEffect(() => {
    if (!key) return
    setLoading(true)
    void refetch()
    const range = (JSON.parse(key) as MetricsQuery).range
    const t = window.setInterval(() => void refetch(), refreshFor(range))
    return () => window.clearInterval(t)
  }, [key, refetch])

  return { result, loading, error }
}
