import { useCallback, useEffect, useRef, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type { MetricProfile } from '@/hooks/useProfiles'

const LIVE_MS = 60_000

/** One row's live value: for a status metric, state is the state's name and
 *  ok says whether it is one of the metric's OK states; otherwise ok says the
 *  value is inside the rule (always true when the metric has none). problem
 *  is set while an incident is open for this exact row. no_reading marks a
 *  row with an open incident but no live sample (a removed fan): value is
 *  then meaningless and state absent. */
export interface HealthRow {
  instance: string
  label: string
  value: number
  state?: string
  ok: boolean
  problem: boolean
  no_reading?: boolean
}

/** One metric's live rows. rule describes its alert rule in words ("above
 *  90 % for 10 min", "not OK"); "" when it has none. A metric with no live
 *  rows in the live window and no open problem is omitted by the backend. */
export interface HealthMetric {
  key: string
  name: string
  kind: string
  units: string
  rule: string
  rows: HealthRow[]
}

export interface DeviceProfileRun {
  ran_at: string
  ok: boolean
  error: string
}

/** One profile's standing with a device: mode is the per-device override
 *  ("auto" means none), matched is what sysObjectID prefix matching alone
 *  would say, and applies accounts for the override too. */
export interface DeviceProfileView {
  profile: MetricProfile
  applies: boolean
  matched: boolean
  mode: 'auto' | 'attach' | 'detach'
  last_run: DeviceProfileRun | null
}

/** GET /devices/:id/health: every profile's standing with the device, plus
 *  the live rows of each applicable metric that has any. */
export interface DeviceHealth {
  profiles: DeviceProfileView[]
  metrics: HealthMetric[]
}

/**
 * A device's Health view, refreshed every 60 s (as useDevicePorts).
 *
 * Guards against a slow response landing after deviceId has already moved on
 * to another device: each call is tagged with a ticket, and a result is only
 * applied while its ticket is still the latest one issued.
 */
export function useDeviceHealth(deviceId: string | undefined) {
  const [health, setHealth] = useState<DeviceHealth | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const ticket = useRef(0)

  const refetch = useCallback(async () => {
    if (!deviceId) return
    const mine = ++ticket.current
    try {
      const { data } = await api.get<ApiResponse<DeviceHealth>>(`/devices/${deviceId}/health`)
      if (ticket.current !== mine) return
      setHealth(data.data)
      setError(null)
    } catch (err) {
      if (ticket.current !== mine) return
      setError((err as ApiError).message || 'Failed to load device health')
    } finally {
      if (ticket.current === mine) setLoading(false)
    }
  }, [deviceId])

  useEffect(() => {
    setLoading(true)
    void refetch()
    const t = window.setInterval(() => void refetch(), LIVE_MS)
    return () => window.clearInterval(t)
  }, [refetch])

  return { health, loading, error, refetch }
}

/** PUT /devices/:id/profiles/:profileId: "auto" clears any override,
 *  "attach"/"detach" set one. Used by the Edit details modal's profile
 *  fieldset, which saves each change immediately. */
export async function setDeviceProfile(deviceId: string, profileId: string, mode: 'auto' | 'attach' | 'detach'): Promise<void> {
  await api.put(`/devices/${deviceId}/profiles/${profileId}`, { mode })
}
