import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type { Device, DeviceInterface, DeviceType } from '@/hooks/useDevices'

export type PortCondition = 'link_down' | 'errors' | 'flapping' | 'slow_link' | 'saturated'

export interface PortView extends DeviceInterface {
  number: number
  collected: boolean
  physical: boolean
  in_bps: number | null
  out_bps: number | null
  in_util_pct: number | null
  out_util_pct: number | null
  errors_per_min: number | null
}

export interface FacePort {
  if_index: number
  number: number
}
export interface FaceBlock {
  sfp: boolean
  top: FacePort[]
  bottom: FacePort[]
}
export interface Faceplate {
  rows: number
  blocks: FaceBlock[]
}

export interface PortDefaults {
  error_threshold_per_min: number
  util_threshold_pct: number
  down_grace_seconds: number
}

export interface DevicePorts {
  ports: PortView[]
  faceplate: Faceplate
  defaults: PortDefaults
}

export interface PortIncident {
  id: string
  start_time: string
  condition: PortCondition | null
  root_cause: string
}

export interface PortDetail extends PortView {
  defaults: PortDefaults
  open_incidents: PortIncident[]
}

export interface PortEvent {
  id: number
  device_id: string
  interface_id: string
  if_index: number
  kind: string
  started_at: string
  ended_at: string | null
  detail: Record<string, unknown>
  device_name: string
  port_name: string
  port_alias: string
  port_number: number
}

export interface PortRef {
  device_id: string
  device_name: string
  if_index: number
  number: number
  name: string
  alias: string
  oper_status: string
  important: boolean
  conditions: PortCondition[]
  in_bps: number | null
  out_bps: number | null
  util_pct: number | null
}

export interface SitePortSummary {
  busiest: PortRef[]
  problems: PortRef[]
}

/** PATCH body: absent = unchanged, null = back to the default. */
export interface PortPatch {
  important?: boolean
  collect?: boolean | null
  util_threshold_pct?: number | null
  error_threshold_per_min?: number | null
  down_grace_seconds?: number | null
}

/** PATCH body: absent = unchanged, null or '' = back to what SNMP reports. */
export interface DeviceDetailsPatch {
  vendor_override?: string | null
  model_override?: string | null
  location_override?: string | null
  device_type?: DeviceType | null
  faceplate_rows?: number | null
  faceplate_sfp_ports?: number[] | null
}

const LIVE_MS = 60_000

/** GET url every refreshMs (0 = once). notFound covers 404, which the API
 *  also answers for things in a site the caller cannot see. */
function useResource<T>(url: string | null, params: Record<string, string | number> | undefined, refreshMs: number) {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [notFound, setNotFound] = useState(false)
  const paramsKey = JSON.stringify(params ?? {})

  const refetch = useCallback(async () => {
    if (!url) return
    try {
      const { data: res } = await api.get<ApiResponse<T>>(url, { params: JSON.parse(paramsKey) })
      setData(res.data)
      setError(null)
      setNotFound(false)
    } catch (err) {
      const e = err as ApiError
      if (e.status === 404) {
        setNotFound(true)
        setData(null)
      }
      setError(e.message || 'Failed to load')
    } finally {
      setLoading(false)
    }
  }, [url, paramsKey])

  useEffect(() => {
    setLoading(true)
    void refetch()
    if (!refreshMs) return
    const t = window.setInterval(() => void refetch(), refreshMs)
    return () => window.clearInterval(t)
  }, [refetch, refreshMs])

  return { data, loading, error, notFound, refetch }
}

/** A device's ports, faceplate and default thresholds, every minute. */
export function useDevicePorts(deviceId: string | undefined) {
  return useResource<DevicePorts>(deviceId ? `/devices/${deviceId}/ports` : null, undefined, LIVE_MS)
}

export function usePort(deviceId: string | undefined, ifIndex: string | undefined) {
  return useResource<PortDetail>(deviceId && ifIndex ? `/devices/${deviceId}/ports/${ifIndex}` : null, undefined, LIVE_MS)
}

/** Port events for a device (optionally one port) or a site, newest first. */
export function usePortEvents(scope: { deviceId?: string; siteId?: string; ifIndex?: number }, limit = 20) {
  const url = scope.deviceId ? `/devices/${scope.deviceId}/events` : scope.siteId ? `/sites/${scope.siteId}/port-events` : null
  const params: Record<string, number> = { limit }
  if (scope.ifIndex) params.if_index = scope.ifIndex
  const r = useResource<{ events: PortEvent[]; total: number }>(url, params, LIVE_MS)
  return { events: r.data?.events ?? [], total: r.data?.total ?? 0, loading: r.loading, refetch: r.refetch }
}

export function useSitePortSummary(siteId: string | undefined) {
  return useResource<SitePortSummary>(siteId ? `/sites/${siteId}/ports/summary` : null, undefined, LIVE_MS)
}

export function usePortActions() {
  const [busy, setBusy] = useState(false)
  const run = useCallback(async <T,>(fn: () => Promise<T>): Promise<T> => {
    setBusy(true)
    try {
      return await fn()
    } finally {
      setBusy(false)
    }
  }, [])
  const updatePort = useCallback(
    (deviceId: string, ifIndex: number, patch: PortPatch) =>
      run(async () => (await api.patch<ApiResponse<DeviceInterface>>(`/devices/${deviceId}/ports/${ifIndex}`, patch)).data.data),
    [run]
  )
  const updateDetails = useCallback(
    (deviceId: string, patch: DeviceDetailsPatch) =>
      run(async () => (await api.patch<ApiResponse<Device>>(`/devices/${deviceId}/details`, patch)).data.data),
    [run]
  )
  return { updatePort, updateDetails, busy }
}
