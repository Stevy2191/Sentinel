import { useCallback, useEffect, useMemo, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export type SitePermission = 'readonly' | 'editable'
export type SiteAccess = 'none' | 'readonly' | 'editable' | 'admin'

export interface Site {
  id: string
  name: string
  description: string | null
  address: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

/** GET /sites/:id adds what the caller may do, so the page knows which
 *  controls to show. */
export interface SiteDetail extends Site {
  access: SiteAccess
}

export interface SiteInput {
  name: string
  description: string | null
  address: string | null
}

export interface SiteShare {
  id: string
  site_id: string
  shared_with_user_id: string
  username: string
  email: string
  permission: SitePermission
  created_at: string
}

/** Sites the signed-in user can see: all of them for an admin. */
export function useSites() {
  const [sites, setSites] = useState<Site[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<Site[]>>('/sites')
      setSites(data.data ?? [])
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load sites')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { sites, loading, error, refetch }
}

/** One site. notFound covers both "missing" and "not shared with you": the
 *  API answers 404 for both on purpose. */
export function useSite(id: string | undefined) {
  const [site, setSite] = useState<SiteDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [notFound, setNotFound] = useState(false)

  const refetch = useCallback(async () => {
    if (!id) return
    try {
      const { data } = await api.get<ApiResponse<SiteDetail>>(`/sites/${id}`)
      setSite(data.data)
      setNotFound(false)
    } catch (err) {
      const e = err as ApiError
      if (e.status === 404 || e.status === 400) setNotFound(true)
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { site, loading, notFound, refetch }
}

/** Create, edit and delete (admin only; the API enforces it). */
export function useSiteActions() {
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
    (input: SiteInput) =>
      run(async () => (await api.post<ApiResponse<Site>>('/sites', input)).data.data),
    [run]
  )
  const update = useCallback(
    (id: string, input: SiteInput) =>
      run(async () => (await api.put<ApiResponse<Site>>(`/sites/${id}`, input)).data.data),
    [run]
  )
  const remove = useCallback(
    (id: string) =>
      run(async () => {
        await api.delete(`/sites/${id}`)
      }),
    [run]
  )

  return { create, update, remove, busy }
}

/** Who a site is shared with (admin only). enabled=false skips the request
 *  for non-admins, who would only get a 403. */
export function useSiteShares(siteId: string | undefined, enabled: boolean) {
  const [shares, setShares] = useState<SiteShare[]>([])
  const [loading, setLoading] = useState(false)

  const refetch = useCallback(async () => {
    if (!siteId || !enabled) return
    setLoading(true)
    try {
      const { data } = await api.get<ApiResponse<SiteShare[]>>(`/sites/${siteId}/shares`)
      setShares(data.data ?? [])
    } finally {
      setLoading(false)
    }
  }, [siteId, enabled])

  useEffect(() => {
    void refetch()
  }, [refetch])

  const share = useCallback(
    async (userId: string, permission: SitePermission) => {
      await api.post(`/sites/${siteId}/shares`, { user_id: userId, permission })
      await refetch()
    },
    [siteId, refetch]
  )
  const unshare = useCallback(
    async (userId: string) => {
      await api.delete(`/sites/${siteId}/shares/${userId}`)
      await refetch()
    },
    [siteId, refetch]
  )

  return { shares, loading, refetch, share, unshare }
}

/** The Overview card: how many sites the user can see. */
export function useSiteSummary(): { value: string; subtitle: string } {
  const { sites, loading, error } = useSites()
  return useMemo(() => {
    if (loading) return { value: '—', subtitle: 'loading' }
    if (error) return { value: '—', subtitle: 'unavailable' }
    // A bare zero says nothing about why; the card says what to do instead.
    if (sites.length === 0) return { value: '0', subtitle: 'add a site' }
    return { value: String(sites.length), subtitle: sites.length === 1 ? 'site' : 'sites' }
  }, [sites, loading, error])
}
