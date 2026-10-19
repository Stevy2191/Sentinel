import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import { useDebounced } from '@/hooks/useDebounced'

/** What an endpoint is, for its icon. A hint only: the maker is always
 *  shown beside it. */
export type EndpointKind = 'camera' | 'phone' | 'printer' | 'ap' | 'device'
/** Where it is: on an edge port, through an AP, behind a link port (an
 *  unmanaged switch), or only in a router's ARP table. */
export type EndpointHow = 'wired' | 'wireless' | 'behind' | 'router'

export interface BehindGroup {
  device_id: string
  if_index: number
  port_label: string
  count: number
  endpoints?: Endpoint[]
}

export interface Endpoint {
  mac: string
  maker: string
  kind: EndpointKind
  ips: string[]
  name: string
  /** Who named it: the user, reverse DNS, or the device itself over LLDP. */
  name_source: 'user' | 'dns' | 'lldp' | ''
  how: EndpointHow
  via: string
  device_id: string | null
  device_name: string
  if_index: number | null
  port_label: string
  vlan: number
  site_id: string
  first_seen: string
  last_seen: string
  behind?: BehindGroup
}

export interface APGroup {
  if_index: number
  port_label: string
  name: string
  mac: string
  ap_device_id: string | null
  clients: Endpoint[]
}

export interface ConnectedView {
  endpoints: Endpoint[]
  aps: APGroup[]
  behind: BehindGroup[]
}

export interface SightingView {
  device_id: string
  device_name: string
  if_index: number
  port_label: string
  vlan: number
  first_seen: string
  last_seen: string
}

/** "Core-SW port 14", "via KitchenAP, Core-SW port 47", "behind Core-SW
 *  port 12" or "seen by Quantum-Gate, port unknown". */
export function locationLine(e: Endpoint): string {
  const port = `${e.device_name} port ${e.port_label}`
  switch (e.how) {
    case 'wireless':
      return `via ${e.via || 'an access point'}, ${port}`
    case 'behind':
      return `behind ${port}`
    case 'router':
      return `seen by ${e.device_name}, port unknown`
    default:
      return port
  }
}

/** The name, else the first IP, else the MAC. */
export function endpointTitle(e: Endpoint): string {
  return e.name || e.ips[0] || e.mac
}

/** Where a result opens: its switch (or router) with the record highlighted. */
export function endpointHref(e: Endpoint): string | null {
  return e.device_id ? `/network/devices/${e.device_id}?endpoint=${encodeURIComponent(e.mac)}` : null
}

/** Whether the last poll of its switch missed it: more than two L2 polls
 *  (5 min each) since it was seen. */
export function endpointStale(e: Endpoint, now = Date.now()): boolean {
  return now - new Date(e.last_seen).getTime() > 11 * 60_000
}

/** Find a device: up to 50 results for q, debounced 250 ms. */
export function useEndpointSearch(q: string) {
  const query = useDebounced(q.trim(), 250)
  const [results, setResults] = useState<Endpoint[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!query) {
      setResults([])
      setLoading(false)
      setError(null)
      return
    }
    let cancelled = false
    setLoading(true)
    api
      .get<ApiResponse<Endpoint[]>>('/network/endpoints/search', { params: { q: query } })
      .then(({ data }) => {
        if (cancelled) return
        setResults(data.data ?? [])
        setError(null)
      })
      .catch((err: ApiError) => {
        if (cancelled) return
        setResults([])
        setError(err.message || 'Search failed')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [query])

  return { results, loading, error, query }
}

/** A switch's (or router's) connected devices, refreshed every 60 s. */
export function useConnected(deviceId: string | undefined) {
  const [view, setView] = useState<ConnectedView | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    if (!deviceId) return
    try {
      const { data } = await api.get<ApiResponse<ConnectedView>>(`/devices/${deviceId}/endpoints`)
      setView(data.data)
      setError(null)
    } catch (err) {
      setError((err as ApiError).message || 'Could not load connected devices')
    } finally {
      setLoading(false)
    }
  }, [deviceId])

  useEffect(() => {
    setLoading(true)
    setView(null)
    void refetch()
    const t = window.setInterval(() => void refetch(), 60_000)
    return () => window.clearInterval(t)
  }, [refetch])

  return { view, loading, error, refetch }
}

/** One device's full record and its last 20 sightings. */
export function useEndpoint(mac: string | null) {
  const [endpoint, setEndpoint] = useState<Endpoint | null>(null)
  const [sightings, setSightings] = useState<SightingView[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    if (!mac) return
    setLoading(true)
    try {
      const { data } = await api.get<ApiResponse<{ endpoint: Endpoint; sightings: SightingView[] }>>(
        `/network/endpoints/${encodeURIComponent(mac)}`
      )
      setEndpoint(data.data.endpoint)
      setSightings(data.data.sightings ?? [])
      setError(null)
    } catch (err) {
      const e = err as ApiError
      setError(e.status === 404 ? 'Sentinel no longer sees this device.' : e.message || 'Could not load this device')
    } finally {
      setLoading(false)
    }
  }, [mac])

  useEffect(() => {
    setEndpoint(null)
    setSightings([])
    void refetch()
  }, [refetch])

  return { endpoint, sightings, loading, error, refetch }
}

/** Sets (or, with an empty name, clears) the user's name for a device. */
export async function setEndpointName(mac: string, name: string): Promise<string> {
  const { data } = await api.put<ApiResponse<{ mac: string; name: string }>>(
    `/network/endpoints/${encodeURIComponent(mac)}/name`,
    { name }
  )
  return data.data.name
}
