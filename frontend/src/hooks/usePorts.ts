import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type { Device, DeviceInterface, DeviceType, PortRole } from '@/hooks/useDevices'
import type { MetricsRange } from '@/hooks/useMetrics'

export type PortCondition = 'link_down' | 'errors' | 'flapping' | 'slow_link' | 'saturated'

export interface PortView extends DeviceInterface {
  number: number
  /** How the port is named: "12", or "1/4" (slot/port) on a Cisco-style
   *  switch/slot/port device. slot is its network module, 0 for the main ports. */
  label: string
  slot: number
  collected: boolean
  physical: boolean
  in_bps: number | null
  out_bps: number | null
  in_util_pct: number | null
  out_util_pct: number | null
  errors_per_min: number | null
  neighbor_device_name: string
  // Structured, not a preformatted string: format with portTitle, the same
  // helper the device/port picker uses, so the picker and the saved text
  // read exactly the same way. neighbor_port_number is null when the far
  // port isn't known (no neighbor, or its if_index didn't resolve).
  neighbor_port_number: number | null
  neighbor_port_unit: number
  neighbor_port_alias: string
  neighbor_port_label: string
}

export interface FacePort {
  if_index: number
  number: number
}
export interface FaceBlock {
  /** "Module N" for a network module block; absent for the main ports. */
  label?: string
  sfp: boolean
  top: FacePort[]
  bottom: FacePort[]
}
export interface Faceplate {
  rows: number
  blocks: FaceBlock[]
}
/** One stack member's faceplate; label is "Switch N" when the device has
 *  more than one member, else ''. */
export interface UnitFaceplate extends Faceplate {
  unit: number
  label: string
}

export interface PortDefaults {
  error_threshold_per_min: number
  util_threshold_pct: number
  down_grace_seconds: number
}

export interface DevicePorts {
  ports: PortView[]
  faceplates: UnitFaceplate[]
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
  port_label: string
  stack_unit: number
}

export interface PortRef {
  device_id: string
  device_name: string
  if_index: number
  number: number
  label: string
  name: string
  alias: string
  oper_status: string
  important: boolean
  conditions: PortCondition[]
  in_bps: number | null
  out_bps: number | null
  util_pct: number | null
  stack_unit: number
}

export interface SitePortSummary {
  busiest: PortRef[]
  problems: PortRef[]
}

export interface NorthSouthPoint {
  t: string
  in_bps: number
  out_bps: number
}
export interface EastWestPoint {
  t: string
  bps: number
}

/** GET /sites/:id/traffic: the site's internet (north-south) and internal
 *  (east-west) traffic. wan_configured is false until a port is marked WAN,
 *  and both series are empty until then. */
export interface SiteTraffic {
  resolution: 'raw' | '5m' | '1h'
  step_seconds: number
  wan_configured: boolean
  north_south: NorthSouthPoint[]
  east_west: EastWestPoint[]
}

/** PATCH body: absent = unchanged, null = back to the default. role has no
 *  "back to the default": absent is the only way to leave it unchanged. */
export interface PortPatch {
  important?: boolean
  collect?: boolean | null
  util_threshold_pct?: number | null
  error_threshold_per_min?: number | null
  down_grace_seconds?: number | null
  role?: PortRole
  neighbor_device_id?: string | null
  neighbor_if_index?: number | null
}

/** PATCH body: absent = unchanged, null or '' = back to what SNMP reports. */
export interface DeviceDetailsPatch {
  vendor_override?: string | null
  model_override?: string | null
  location_override?: string | null
  device_type?: DeviceType | null
  faceplate_rows?: number | null
  faceplate_sfp_ports?: number[] | null
  faceplate_port_style?: FaceplatePortStyle | null
  ups_low_battery_pct?: number | null
  ups_high_load_pct?: number | null
}

/** How the faceplate draws the main ports; null on the device is Auto. */
export type FaceplatePortStyle = 'sfp' | 'rj45'

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

/** GET /devices/:id/ups: latest UPS-MIB readings (metric key -> value,
 *  absent when not reported recently), open UPS conditions, thresholds. */
export interface UPSStatus {
  readings: Partial<Record<UPSMetric, number>>
  conditions: string[]
  low_battery_pct: number
  high_load_pct: number
  default_low_battery_pct: number
  default_high_load_pct: number
}

export type UPSMetric =
  | 'ups_charge_pct'
  | 'ups_runtime_min'
  | 'ups_load_pct'
  | 'ups_input_v'
  | 'ups_output_v'
  | 'ups_battery_temp_c'
  | 'ups_on_battery'
  | 'ups_battery_status'

export function useUPSStatus(deviceId: string | undefined, enabled: boolean) {
  return useResource<UPSStatus>(deviceId && enabled ? `/devices/${deviceId}/ups` : null, undefined, LIVE_MS)
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

/** A site's north-south and east-west traffic for one range. Short ranges
 *  refresh every minute; long ones every five, same as useMetricsQuery. */
export function useSiteTraffic(siteId: string | undefined, range: MetricsRange) {
  const refreshMs = range === '1h' || range === '6h' || range === '24h' ? 60_000 : 300_000
  return useResource<SiteTraffic>(siteId ? `/sites/${siteId}/traffic` : null, { range }, refreshMs)
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
