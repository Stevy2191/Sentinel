import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse, Check, PaginatedChecks } from '@/types'

export interface UseMonitorChecksResult {
  checks: Check[]
  total: number
  loading: boolean
  error: ApiError | null
  refetch: () => Promise<void>
}

/**
 * useMonitorChecks fetches a monitor's check history. If start/end are omitted
 * the backend defaults to the last 24 hours.
 */
export function useMonitorChecks(
  monitorId: string | undefined,
  startTime?: string,
  endTime?: string,
  limit = 100
): UseMonitorChecksResult {
  const [checks, setChecks] = useState<Check[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const refetch = useCallback(async () => {
    if (!monitorId) return
    setLoading(true)
    setError(null)
    try {
      const params: Record<string, unknown> = { limit }
      if (startTime) params.start_time = startTime
      if (endTime) params.end_time = endTime
      const { data } = await api.get<ApiResponse<PaginatedChecks>>(
        `/monitors/${monitorId}/checks`,
        { params }
      )
      setChecks(data.data.checks)
      setTotal(data.data.pagination.total)
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [monitorId, startTime, endTime, limit])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { checks, total, loading, error, refetch }
}
