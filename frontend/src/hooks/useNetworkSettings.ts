import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export interface NetworkSettings {
  metrics_raw_retention_days: number
  port_error_threshold_per_min: number
  port_util_threshold_pct: number
  port_down_grace_seconds: number
  ups_low_battery_pct: number
  ups_high_load_pct: number
}

export function useNetworkSettings() {
  const [settings, setSettings] = useState<NetworkSettings | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const refetch = useCallback(async () => {
    try {
      const { data } = await api.get<ApiResponse<NetworkSettings>>('/network/settings')
      setSettings(data.data)
      setError(null)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load network settings')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  const save = useCallback(async (patch: Partial<NetworkSettings>) => {
    setSaving(true)
    try {
      const { data } = await api.patch<ApiResponse<NetworkSettings>>('/network/settings', patch)
      setSettings(data.data)
      return data.data
    } finally {
      setSaving(false)
    }
  }, [])

  return { settings, loading, error, saving, save }
}
