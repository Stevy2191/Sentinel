import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse, MaintenanceStatus } from '@/types'

function useMutationState() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)
  const wrap = useCallback(async <R>(fn: () => Promise<R>): Promise<R> => {
    setLoading(true)
    setError(null)
    try {
      return await fn()
    } catch (err) {
      setError(err as ApiError)
      throw err
    } finally {
      setLoading(false)
    }
  }, [])
  return { loading, error, wrap }
}

/** useEnableMaintenanceMode → POST /monitors/:id/maintenance */
export function useEnableMaintenanceMode() {
  const { loading, error, wrap } = useMutationState()
  const enable = useCallback(
    (monitorId: string, startTime: string, endTime: string) =>
      wrap(() => api.post(`/monitors/${monitorId}/maintenance`, { start_time: startTime, end_time: endTime })),
    [wrap]
  )
  return { enable, loading, error }
}

/** useUpdateMaintenanceWindow → PATCH /monitors/:id/maintenance */
export function useUpdateMaintenanceWindow() {
  const { loading, error, wrap } = useMutationState()
  const update = useCallback(
    (monitorId: string, startTime: string, endTime: string) =>
      wrap(() => api.patch(`/monitors/${monitorId}/maintenance`, { start_time: startTime, end_time: endTime })),
    [wrap]
  )
  return { update, loading, error }
}

/** useDisableMaintenanceMode → DELETE /monitors/:id/maintenance */
export function useDisableMaintenanceMode() {
  const { loading, error, wrap } = useMutationState()
  const disable = useCallback(
    (monitorId: string) => wrap(() => api.delete(`/monitors/${monitorId}/maintenance`)),
    [wrap]
  )
  return { disable, loading, error }
}

/** useGetMaintenanceStatus → GET /monitors/:id/maintenance (auto-refreshes countdown every 10s). */
export function useGetMaintenanceStatus(monitorId: string | undefined) {
  const [status, setStatus] = useState<MaintenanceStatus | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const refetch = useCallback(async () => {
    if (!monitorId) return
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<MaintenanceStatus>>(`/monitors/${monitorId}/maintenance`)
      setStatus(data.data)
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [monitorId])

  useEffect(() => {
    void refetch()
    // Refresh the countdown periodically while mounted.
    const t = window.setInterval(() => void refetch(), 10_000)
    return () => window.clearInterval(t)
  }, [refetch])

  return { status, loading, error, refetch }
}
