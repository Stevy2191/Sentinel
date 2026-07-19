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

/** useNotificationHistory lists recently sent notifications. */
export function useNotificationHistory() {
  const [history, setHistory] = useState<NotificationHistoryItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<ApiError | null>(null)

  const refetch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<{ history: NotificationHistoryItem[] }>>(
        '/notifications/history'
      )
      setHistory(data.data.history ?? [])
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { history, loading, error, refetch }
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
