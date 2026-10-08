import { useCallback, useEffect, useRef, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export type CircuitKind = 'fiber' | 'cable' | 'dsl' | 'fixed_wireless' | 'cellular' | 'copper' | 'other'

export interface SiteNetwork {
  id: string
  site_id: string
  name: string
  cidr: string
  vlan: number | null
  gateway: string | null
  note: string | null
  created_at: string
  updated_at: string
}

/** The port a circuit is tied to, read live. Rates are null without a recent sample. */
export interface LivePort {
  interface_id: string
  device_id: string
  device_name: string
  if_index: number
  number: number
  label: string
  name: string
  alias: string
  stack_unit: number
  oper_status: string
  in_bps: number | null
  out_bps: number | null
}

export interface SiteCircuit {
  id: string
  site_id: string
  provider: string
  circuit_ref: string | null
  kind: CircuitKind
  download_mbps: number | null
  upload_mbps: number | null
  support_phone: string | null
  account_number: string | null
  notes: string | null
  interface_id: string | null
  /** null when not tied to a port, or its device has left the site. */
  port: LivePort | null
  created_at: string
  updated_at: string
}

export interface SiteProfile {
  notes: string | null
  networks: SiteNetwork[]
  circuits: SiteCircuit[]
}

export interface SiteNetworkInput {
  name: string
  cidr: string
  vlan: number | null
  gateway: string | null
  note: string | null
}

export interface SiteCircuitInput {
  provider: string
  kind: CircuitKind
  circuit_ref: string | null
  download_mbps: number | null
  upload_mbps: number | null
  support_phone: string | null
  account_number: string | null
  notes: string | null
  interface_id: string | null
}

const POLL_MS = 30_000

/** A site's notes, networks and circuits (with their ports live), refreshed
 *  every 30 seconds. The answer is kept with the site it belongs to and each
 *  load takes a number, so neither a slow load nor another site's answer can
 *  replace what this page shows. */
export function useSiteProfile(siteId: string | undefined) {
  const [answer, setAnswer] = useState<{ siteId: string; profile: SiteProfile | null; error: string | null } | null>(null)
  const latest = useRef(0)
  // Loads in progress; the poll skips a tick while one is running so a slow
  // server cannot supersede every answer before it lands.
  const inFlight = useRef(0)

  const refetch = useCallback(async () => {
    if (!siteId) return
    const mine = ++latest.current
    inFlight.current++
    try {
      const { data } = await api.get<ApiResponse<SiteProfile>>(`/sites/${siteId}/profile`)
      if (mine === latest.current) setAnswer({ siteId, profile: data.data, error: null })
    } catch (err) {
      if (mine === latest.current) {
        setAnswer((prev) => ({
          siteId,
          // A failed refresh keeps showing what this site had.
          profile: prev && prev.siteId === siteId ? prev.profile : null,
          error: (err as ApiError).message || 'Could not load the site profile',
        }))
      }
    } finally {
      inFlight.current--
    }
  }, [siteId])

  useEffect(() => {
    void refetch()
    const t = window.setInterval(() => {
      if (inFlight.current === 0) void refetch()
    }, POLL_MS)
    return () => window.clearInterval(t)
  }, [refetch])

  const current = answer && answer.siteId === siteId ? answer : null
  return {
    profile: current?.profile ?? null,
    loading: !current,
    error: current?.error ?? null,
    refetch,
  }
}

/** Changes to a site's profile. Each call rejects with the API's error. */
export function useSiteProfileActions(siteId: string) {
  const [busy, setBusy] = useState(false)
  const run = useCallback(async <T>(fn: () => Promise<T>): Promise<T> => {
    setBusy(true)
    try {
      return await fn()
    } finally {
      setBusy(false)
    }
  }, [])
  const base = `/sites/${siteId}`
  return {
    busy,
    saveNotes: (notes: string) => run(() => api.put(`${base}/notes`, { notes })),
    createNetwork: (input: SiteNetworkInput) => run(() => api.post(`${base}/networks`, input)),
    updateNetwork: (id: string, input: SiteNetworkInput) => run(() => api.put(`${base}/networks/${id}`, input)),
    deleteNetwork: (id: string) => run(() => api.delete(`${base}/networks/${id}`)),
    createCircuit: (input: SiteCircuitInput) => run(() => api.post(`${base}/circuits`, input)),
    updateCircuit: (id: string, input: SiteCircuitInput) => run(() => api.put(`${base}/circuits/${id}`, input)),
    deleteCircuit: (id: string) => run(() => api.delete(`${base}/circuits/${id}`)),
  }
}
