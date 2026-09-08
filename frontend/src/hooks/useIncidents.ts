import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { Check } from '@/types'

export type IncidentStatus = 'ongoing' | 'resolved'
export type IncidentType = 'down' | 'timeout' | 'error'

export interface Incident {
  id: string
  monitor_id: string
  monitor_name: string
  monitor_url: string
  monitor_type: string
  incident_type: IncidentType
  status: IncidentStatus
  start_time: string
  end_time: string | null
  /** Seconds. Measured to now while the incident is still open. */
  duration_seconds: number
  severity: string
  root_cause: string
  notes: string
  resolution_notes: string
  created_at: string
}

export interface IncidentFilters {
  page: number
  limit: number
  status: 'all' | IncidentStatus
  search: string
  sort: 'started' | 'duration'
  order: 'asc' | 'desc'
  monitorId?: string
}

export const DEFAULT_FILTERS: IncidentFilters = {
  page: 1,
  limit: 50,
  status: 'all',
  search: '',
  sort: 'started',
  order: 'desc',
}

/** "2h 15m", "45s", "3d 4h" — the two largest units that carry information. */
export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '—'
  if (seconds < 60) return `${Math.floor(seconds)}s`
  const m = Math.floor(seconds / 60) % 60
  const h = Math.floor(seconds / 3600) % 24
  const d = Math.floor(seconds / 86400)
  if (d > 0) return h > 0 ? `${d}d ${h}h` : `${d}d`
  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`
  return `${m}m`
}

/**
 * Paginated incidents across every monitor.
 *
 * Filtering and paging happen server-side: the table is capped at 50 rows a
 * page but the history behind it is not, so filtering in the browser would
 * only ever filter the page you happen to be looking at.
 */
export function useIncidents(filters: IncidentFilters) {
  const [incidents, setIncidents] = useState<Incident[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const { page, limit, status, search, sort, order, monitorId } = filters

  const refetch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await api.get<{ data: { total: number; incidents: Incident[] } }>('/incidents', {
        params: {
          page,
          limit,
          status,
          sort,
          order,
          ...(search.trim() ? { search: search.trim() } : {}),
          ...(monitorId ? { monitor_id: monitorId } : {}),
        },
      })
      setIncidents(res.data.data.incidents ?? [])
      setTotal(res.data.data.total ?? 0)
    } catch (err) {
      setError((err as ApiError).message || 'Could not load incidents')
      setIncidents([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }, [page, limit, status, search, sort, order, monitorId])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { incidents, total, loading, error, refetch }
}

export interface IncidentDetail {
  incident: Incident
  checks: Check[]
  total_checks: number
}

/** One incident with the checks recorded while it was open. */
export function useIncidentDetail(id: string | null) {
  const [detail, setDetail] = useState<IncidentDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Bumped to force a reload after an edit, without closing and reopening.
  const [nonce, setNonce] = useState(0)

  const reload = useCallback(() => setNonce((n) => n + 1), [])

  useEffect(() => {
    if (!id) {
      setDetail(null)
      return
    }
    let active = true
    setLoading(true)
    setError(null)
    api
      .get<{ data: IncidentDetail }>(`/incidents/${id}`)
      .then((res) => active && setDetail(res.data.data))
      .catch((err) => active && setError((err as ApiError).message || 'Could not load the incident'))
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [id, nonce])

  return { detail, loading, error, reload }
}

/**
 * Save an operator's notes on an incident.
 *
 * Only the two human-authored fields. root_cause is deliberately not editable
 * from here: it holds the failing check's own error message, captured when the
 * incident opened, and letting a note overwrite it would destroy the one piece
 * of evidence about what actually happened.
 */
export function useUpdateIncident() {
  const [saving, setSaving] = useState(false)

  const save = useCallback(
    async (id: string, patch: { notes?: string; resolution_notes?: string }) => {
      setSaving(true)
      try {
        await api.patch(`/incidents/${id}`, patch)
      } finally {
        setSaving(false)
      }
    },
    []
  )

  return { save, saving }
}

/** The retention window, in days, plus the bounds the API will accept. */
export function useIncidentRetention() {
  const [days, setDays] = useState<number | null>(null)
  const [bounds, setBounds] = useState({ min: 7, max: 365 })
  const [loading, setLoading] = useState(true)

  const refetch = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.get<{ data: { days: number; min: number; max: number } }>(
        '/settings/incident-retention-days'
      )
      setDays(res.data.data.days)
      setBounds({ min: res.data.data.min, max: res.data.data.max })
    } catch {
      setDays(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  const save = useCallback(async (next: number) => {
    await api.patch('/settings/incident-retention-days', { days: next })
    setDays(next)
  }, [])

  return { days, bounds, loading, save, refetch }
}
