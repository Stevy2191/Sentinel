import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse, NotificationChannel, NotificationHistoryItem } from '@/types'

// NOTE: The endpoints used here do not exist on the backend yet. These hooks are
// skeletons targeting the intended URLs and will error until the corresponding
// endpoints are implemented server-side.

/** useNotificationChannels lists configured notification channels. */
export function useNotificationChannels() {
  const [channels, setChannels] = useState<NotificationChannel[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<ApiError | null>(null)

  const refetch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<{ channels: NotificationChannel[] }>>(
        '/notifications/channels'
      )
      setChannels(data.data.channels ?? [])
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { channels, loading, error, refetch }
}

export interface NotificationHistoryFilters {
  limit?: number
  offset?: number
  status?: string
  start?: string
  end?: string
}

interface HistoryResponse {
  notifications: NotificationHistoryItem[]
  pagination: { limit: number; offset: number; total: number }
}

/** useNotificationHistory lists notifications, filtered and paginated. */
export function useNotificationHistory(filters?: NotificationHistoryFilters) {
  const [history, setHistory] = useState<NotificationHistoryItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<ApiError | null>(null)
  const key = JSON.stringify(filters ?? {})

  const refetch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const params: Record<string, unknown> = {}
      if (filters?.limit) params.limit = filters.limit
      if (filters?.offset) params.offset = filters.offset
      if (filters?.status) params.status = filters.status
      if (filters?.start) params.start = filters.start
      if (filters?.end) params.end = filters.end
      const { data } = await api.get<ApiResponse<HistoryResponse>>('/notifications/history', {
        params,
      })
      setHistory(data.data.notifications ?? [])
      setTotal(data.data.pagination?.total ?? 0)
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { history, total, loading, error, refetch }
}

/** useSendTestNotification sends a test notification through one channel. */
export function useSendTestNotification(channel: string) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const send = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      await api.post(`/notifications/test/${channel}`)
    } catch (err) {
      setError(err as ApiError)
      throw err
    } finally {
      setLoading(false)
    }
  }, [channel])

  return { send, loading, error }
}
