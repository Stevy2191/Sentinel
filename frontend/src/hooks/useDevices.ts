import { useCallback, useEffect, useMemo, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export type DeviceStatus = 'pending' | 'up' | 'down' | 'paused' | 'error'

export type DeviceType = 'switch' | 'router' | 'access_point' | 'nvr' | 'other'

export const DEVICE_TYPE_LABEL: Record<DeviceType, string> = {
  switch: 'Switch',
  router: 'Router',
  access_point: 'Access point',
  nvr: 'NVR',
  other: 'Other',
}

export interface Device {
  id: string
  site_id: string
  credential_id: string
  name: string
  host: string
  port: number
  enabled: boolean
  poll_interval: number
  timeout_ms: number
  retries: number
  notify_channels: string[] | null
  status: DeviceStatus
  status_detail: string
  consecutive_failures: number
  last_polled_at: string | null
  last_seen_at: string | null
  last_inventory_at: string | null
  sys_name: string
  sys_descr: string
  sys_object_id: string
  sys_location: string
  sys_contact: string
  sys_uptime_seconds: number | null
  vendor: string
  model: string
  serial: string
  vendor_override: string | null
  model_override: string | null
  location_override: string | null
  device_type: DeviceType | null
  device_type_detected: DeviceType
  faceplate_rows: number | null
  faceplate_sfp_ports: number[] | null
  last_stats_at: string | null
  last_stats_duration_ms: number | null
  /** The override when set, else what SNMP reported. */
  effective_vendor?: string
  effective_model?: string
  effective_location?: string
  effective_type?: DeviceType
  created_at: string
  updated_at: string
  site_name?: string
  credential_name?: string
  availability_30d?: number | null
}

export interface DeviceInput {
  site_id: string
  credential_id: string
  name: string
  host: string
  port: number
  enabled: boolean
  poll_interval: number
  timeout_ms: number
  retries: number
  notify_channels: string[] | null
}

export type PortRole = 'access' | 'uplink' | 'wan'

export interface DeviceInterface {
  id: string
  if_index: number
  name: string
  descr: string
  alias: string
  if_type: number
  speed_bps: number
  mac: string
  admin_status: string
  oper_status: string
  last_change_seconds: number
  present: boolean
  connector_present: boolean | null
  role: PortRole
  collect: boolean | null
  collect_default: boolean
  important: boolean
  util_threshold_pct: number | null
  error_threshold_per_min: number | null
  down_grace_seconds: number | null
  usual_speed_bps: number | null
  conditions: string[]
  conditions_since: Record<string, string>
  oper_changed_at: string | null
}

export interface TestResult {
  ok: boolean
  error?: string
  vendor?: string
  system?: { name: string; descr: string; object_id: string; location: string; contact: string; uptime_seconds: number }
}

const REFRESH_MS = 30_000

/** Devices in the sites the user can see, refreshed every 30 s. */
export function useDevices(filter: { siteId?: string; status?: DeviceStatus } = {}) {
  const [devices, setDevices] = useState<Device[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const { siteId, status } = filter

  const refetch = useCallback(async () => {
    try {
      const { data } = await api.get<ApiResponse<Device[]>>('/devices', {
        params: { ...(siteId ? { site_id: siteId } : {}), ...(status ? { status } : {}) },
      })
      setDevices(data.data ?? [])
      setError(null)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load devices')
    } finally {
      setLoading(false)
    }
  }, [siteId, status])

  useEffect(() => {
    void refetch()
    const t = window.setInterval(() => void refetch(), REFRESH_MS)
    return () => window.clearInterval(t)
  }, [refetch])

  return { devices, loading, error, refetch }
}

/** One device. notFound covers "missing" and "not in a site you can see". */
export function useDevice(id: string | undefined) {
  const [device, setDevice] = useState<Device | null>(null)
  const [loading, setLoading] = useState(true)
  const [notFound, setNotFound] = useState(false)

  const refetch = useCallback(async () => {
    if (!id) return
    try {
      const { data } = await api.get<ApiResponse<Device>>(`/devices/${id}`)
      setDevice(data.data)
      setNotFound(false)
    } catch (err) {
      const e = err as ApiError
      if (e.status === 404 || e.status === 400) {
        setDevice(null)
        setNotFound(true)
      }
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => {
    setLoading(true)
    setDevice(null)
    setNotFound(false)
    void refetch()
    const t = window.setInterval(() => void refetch(), REFRESH_MS)
    return () => window.clearInterval(t)
  }, [refetch])

  return { device, loading, notFound, refetch }
}

export function useDeviceInterfaces(id: string | undefined, includeAbsent: boolean) {
  const [interfaces, setInterfaces] = useState<DeviceInterface[]>([])
  const [loading, setLoading] = useState(true)

  const refetch = useCallback(async () => {
    if (!id) return
    try {
      const { data } = await api.get<ApiResponse<DeviceInterface[]>>(`/devices/${id}/interfaces`, {
        params: includeAbsent ? { include_absent: 'true' } : {},
      })
      setInterfaces(data.data ?? [])
    } finally {
      setLoading(false)
    }
  }, [id, includeAbsent])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { interfaces, loading, refetch }
}

export function useDeviceActions() {
  const [busy, setBusy] = useState(false)
  const run = useCallback(async <T,>(fn: () => Promise<T>): Promise<T> => {
    setBusy(true)
    try {
      return await fn()
    } finally {
      setBusy(false)
    }
  }, [])

  const create = useCallback(
    (input: DeviceInput) => run(async () => (await api.post<ApiResponse<Device>>('/devices', input)).data.data),
    [run]
  )
  const update = useCallback(
    (id: string, input: DeviceInput) =>
      run(async () => (await api.put<ApiResponse<Device>>(`/devices/${id}`, input)).data.data),
    [run]
  )
  const remove = useCallback((id: string) => run(async () => void (await api.delete(`/devices/${id}`))), [run])
  const refresh = useCallback(
    (id: string) => run(async () => void (await api.post(`/devices/${id}/refresh`))),
    [run]
  )
  const test = useCallback(
    (input: { site_id: string; credential_id: string; host: string; port: number }) =>
      run(async () => (await api.post<ApiResponse<TestResult>>('/devices/test', input)).data.data),
    [run]
  )

  return { create, update, remove, refresh, test, busy }
}

/** The Overview card: device count, with what needs attention. */
export function useDeviceSummary(): { value: string; subtitle: string; hasDevices: boolean } {
  const { devices, loading, error } = useDevices()
  return useMemo(() => {
    if (loading) return { value: '—', subtitle: 'loading', hasDevices: false }
    if (error || devices.length === 0) return { value: '0', subtitle: '', hasDevices: false }
    const down = devices.filter((d) => d.status === 'down').length
    const erroring = devices.filter((d) => d.status === 'error').length
    let subtitle = 'all up'
    if (down > 0) subtitle = `${down} down`
    else if (erroring > 0) subtitle = `${erroring} need attention`
    else if (devices.some((d) => d.status === 'pending')) subtitle = 'first polls pending'
    return { value: String(devices.length), subtitle, hasDevices: true }
  }, [devices, loading, error])
}

/** "1 Gb/s", "10 Gb/s", "100 Mb/s". */
export function formatSpeed(bps: number): string {
  if (!bps) return '—'
  if (bps >= 1e9) return `${+(bps / 1e9).toFixed(1)} Gb/s`
  if (bps >= 1e6) return `${+(bps / 1e6).toFixed(1)} Mb/s`
  return `${+(bps / 1e3).toFixed(1)} kb/s`
}
