import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export type SharePermission = 'readonly' | 'editable'

// MonitorShare mirrors the enriched share row from GET /monitors/:id/shares.
export interface MonitorShare {
  id: string
  monitor_id: string
  shared_with_user_id: string
  username: string
  email: string
  permission: SharePermission
  shared_by_user_id: string
  created_at: string
  updated_at: string
}

/** List everyone a monitor is shared with (owner-only endpoint). */
export function useMonitorShares(monitorId: string | null) {
  const [shares, setShares] = useState<MonitorShare[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    if (!monitorId) return
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<MonitorShare[]>>(`/monitors/${monitorId}/shares`)
      setShares(data.data ?? [])
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load shares')
    } finally {
      setLoading(false)
    }
  }, [monitorId])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { shares, loading, error, refetch }
}

/** Grant a user access to a monitor. */
export function useShareMonitor() {
  const [loading, setLoading] = useState(false)
  const share = useCallback(
    async (monitorId: string, userId: string, permission: SharePermission) => {
      setLoading(true)
      try {
        const { data } = await api.post<ApiResponse<unknown>>(`/monitors/${monitorId}/share`, {
          user_id: userId,
          permission,
        })
        return data.data
      } finally {
        setLoading(false)
      }
    },
    []
  )
  return { share, loading }
}

/** Change an existing share's permission. */
export function useUpdateMonitorShare() {
  const [loading, setLoading] = useState(false)
  const update = useCallback(
    async (monitorId: string, userId: string, permission: SharePermission) => {
      setLoading(true)
      try {
        await api.patch(`/monitors/${monitorId}/share/${userId}`, { permission })
      } finally {
        setLoading(false)
      }
    },
    []
  )
  return { update, loading }
}

/** Revoke a user's access. */
export function useRevokeMonitorShare() {
  const [loading, setLoading] = useState(false)
  const revoke = useCallback(async (monitorId: string, userId: string) => {
    setLoading(true)
    try {
      await api.delete(`/monitors/${monitorId}/share/${userId}`)
    } finally {
      setLoading(false)
    }
  }, [])
  return { revoke, loading }
}
