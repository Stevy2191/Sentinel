import { useCallback, useEffect, useMemo, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type { MetricsRange } from '@/hooks/useMetrics'
import type {
  Dashboard, DashboardDetail, DashboardShare, DeviceMetric, PublicLink, PublicLinkInfo, WidgetResponse, WidgetType,
} from '@/types/dashboards'

export interface SaveWidget {
  id?: string
  type: WidgetType
  title: string
  config: Record<string, unknown>
  x: number
  y: number
  w: number
  h: number
}

export interface SaveDashboardInput {
  version: number
  name: string
  description: string
  widgets: SaveWidget[]
}

export interface CreateDashboardInput {
  name: string
  description?: string
  site_id?: string | null
  starter?: boolean
}

/** Dashboards the signed-in user can see, optionally one site's. A late
 *  response for an earlier siteId is dropped. */
export function useDashboards(siteId?: string) {
  const [dashboards, setDashboards] = useState<Dashboard[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [version, setVersion] = useState(0)
  const refetch = useCallback(() => setVersion((v) => v + 1), [])

  useEffect(() => {
    let cancelled = false
    api
      .get<ApiResponse<Dashboard[]>>('/dashboards', { params: siteId ? { site_id: siteId } : {} })
      .then(({ data }) => {
        if (cancelled) return
        setDashboards(data.data ?? [])
        setError(null)
      })
      .catch((err: ApiError) => {
        if (!cancelled) setError(err.message || 'Failed to load dashboards')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [siteId, version])

  return { dashboards, loading, error, refetch }
}

/** One dashboard. notFound covers "missing" and "not yours": the API answers
 *  404 for both on purpose. */
export function useDashboard(id: string | undefined) {
  const [dashboard, setDashboard] = useState<DashboardDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [notFound, setNotFound] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [version, setVersion] = useState(0)
  const refetch = useCallback(() => setVersion((v) => v + 1), [])

  useEffect(() => {
    if (!id) return
    let cancelled = false
    api
      .get<ApiResponse<DashboardDetail>>(`/dashboards/${id}`)
      .then(({ data }) => {
        if (cancelled) return
        setDashboard(data.data)
        setNotFound(false)
        setError(null)
      })
      .catch((err: ApiError) => {
        if (cancelled) return
        if (err.status === 404 || err.status === 400) setNotFound(true)
        else setError(err.message || 'Failed to load the dashboard')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [id, version])

  return { dashboard, setDashboard, loading, notFound, error, refetch }
}

/** Dashboard mutations and the admin/editor calls that are not lists. */
export function useDashboardActions() {
  return useMemo(
    () => ({
      create: async (input: CreateDashboardInput) =>
        (await api.post<ApiResponse<DashboardDetail>>('/dashboards', input)).data.data,
      save: async (id: string, input: SaveDashboardInput) =>
        (await api.put<ApiResponse<DashboardDetail>>(`/dashboards/${id}`, input)).data.data,
      remove: async (id: string) => {
        await api.delete(`/dashboards/${id}`)
      },
      listShares: async (id: string) =>
        (await api.get<ApiResponse<DashboardShare[]>>(`/dashboards/${id}/shares`)).data.data ?? [],
      share: async (id: string, userId: string, permission: 'readonly' | 'editable') => {
        await api.put(`/dashboards/${id}/shares/${userId}`, { permission })
      },
      unshare: async (id: string, userId: string) => {
        await api.delete(`/dashboards/${id}/shares/${userId}`)
      },
      getPublicLink: async (id: string) =>
        (await api.get<ApiResponse<PublicLinkInfo>>(`/dashboards/${id}/public-link`)).data.data,
      createPublicLink: async (id: string) =>
        (await api.post<ApiResponse<PublicLink>>(`/dashboards/${id}/public-link`)).data.data,
      revokePublicLink: async (id: string) => {
        await api.delete(`/dashboards/${id}/public-link`)
      },
      preview: async (id: string, body: { type: WidgetType; config: Record<string, unknown>; range?: MetricsRange | '' }) =>
        (await api.post<ApiResponse<WidgetResponse>>(`/dashboards/${id}/widgets/preview`, body)).data.data,
    }),
    [],
  )
}

/** The metrics and instances a device has, for the editor's pickers. */
export function useDeviceMetrics(deviceId: string | undefined) {
  const [metrics, setMetrics] = useState<DeviceMetric[]>([])
  const [loading, setLoading] = useState(false)
  useEffect(() => {
    if (!deviceId) {
      setMetrics([])
      return
    }
    let cancelled = false
    setLoading(true)
    api
      .get<ApiResponse<DeviceMetric[]>>(`/devices/${deviceId}/metrics`)
      .then(({ data }) => {
        if (!cancelled) setMetrics(data.data ?? [])
      })
      .catch(() => {
        if (!cancelled) setMetrics([])
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [deviceId])
  return { metrics, loading }
}
