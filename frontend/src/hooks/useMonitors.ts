import { useCallback, useEffect, useRef, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type {
  ApiResponse,
  Check,
  Monitor,
  MonitorFilters,
  MonitorInput,
  PaginatedMonitors,
} from '@/types'

// ---- Queries ---------------------------------------------------------------

export interface UseMonitorsResult {
  monitors: Monitor[]
  loading: boolean
  error: ApiError | null
  refetch: () => Promise<void>
}

/** useMonitors fetches the monitor list with optional filters and pagination. */
export function useMonitors(filters?: MonitorFilters): UseMonitorsResult {
  const [monitors, setMonitors] = useState<Monitor[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<ApiError | null>(null)

  const params: Record<string, unknown> = {
    page: filters?.page ?? 1,
    limit: filters?.limit ?? 50,
  }
  if (filters?.enabled !== undefined) params.enabled = filters.enabled
  if (filters?.type) params.type = filters.type
  if (filters?.status) params.status = filters.status
  const paramsKey = JSON.stringify(params)

  const refetch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<PaginatedMonitors>>('/monitors', {
        params,
      })
      setMonitors(data.data.monitors)
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [paramsKey])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { monitors, loading, error, refetch }
}

/** useMonitor fetches a single monitor, refetching every 30 seconds. */
export function useMonitor(id: string | undefined) {
  const [monitor, setMonitor] = useState<Monitor | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)
  const activeRef = useRef(true)

  const fetchOnce = useCallback(async () => {
    if (!id) return
    try {
      const { data } = await api.get<ApiResponse<Monitor>>(`/monitors/${id}`)
      if (activeRef.current) setMonitor(data.data)
    } catch (err) {
      if (activeRef.current) setError(err as ApiError)
    } finally {
      if (activeRef.current) setLoading(false)
    }
  }, [id])

  useEffect(() => {
    activeRef.current = true
    if (!id) return
    setLoading(true)
    void fetchOnce()
    const timer = window.setInterval(() => void fetchOnce(), 30_000)
    return () => {
      activeRef.current = false
      window.clearInterval(timer)
    }
  }, [id, fetchOnce])

  return { monitor, loading, error }
}

// ---- Mutations -------------------------------------------------------------

// Small helper to build a mutation hook with loading/error state.
function useMutationState() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)
  const wrap = useCallback(async <R>(fn: () => Promise<R>): Promise<R> => {
    setLoading(true)
    setError(null)
    try {
      return await fn()
    } catch (err) {
      setError(err as ApiError)
      throw err
    } finally {
      setLoading(false)
    }
  }, [])
  return { loading, error, wrap }
}

/** useCreateMonitor returns a create() action. */
export function useCreateMonitor() {
  const { loading, error, wrap } = useMutationState()
  const create = useCallback(
    (input: MonitorInput) =>
      wrap(async () => {
        const { data } = await api.post<ApiResponse<Monitor>>('/monitors', input)
        return data.data
      }),
    [wrap]
  )
  return { create, loading, error }
}

/** useUpdateMonitor returns an update() action. `id` may be provided here or per call. */
export function useUpdateMonitor(id?: string) {
  const { loading, error, wrap } = useMutationState()
  const update = useCallback(
    (input: Partial<MonitorInput>, overrideId?: string) =>
      wrap(async () => {
        const targetId = overrideId ?? id
        if (!targetId) throw { status: 0, message: 'monitor id is required' } as ApiError
        const { data } = await api.put<ApiResponse<Monitor>>(`/monitors/${targetId}`, input)
        return data.data
      }),
    [wrap, id]
  )
  return { update, loading, error }
}

/** useDeleteMonitor returns a delete() action. */
export function useDeleteMonitor(id?: string) {
  const { loading, error, wrap } = useMutationState()
  const del = useCallback(
    (overrideId?: string) =>
      wrap(async () => {
        const targetId = overrideId ?? id
        if (!targetId) throw { status: 0, message: 'monitor id is required' } as ApiError
        await api.delete(`/monitors/${targetId}`)
      }),
    [wrap, id]
  )
  return { delete: del, loading, error }
}

/** usePauseMonitor pauses a monitor (backend route is POST, not PATCH). */
export function usePauseMonitor(id?: string) {
  const { loading, error, wrap } = useMutationState()
  const pause = useCallback(
    (overrideId?: string) =>
      wrap(async () => {
        const targetId = overrideId ?? id
        if (!targetId) throw { status: 0, message: 'monitor id is required' } as ApiError
        await api.post(`/monitors/${targetId}/pause`)
      }),
    [wrap, id]
  )
  return { pause, loading, error }
}

/** useResumeMonitor resumes a monitor (backend route is POST, not PATCH). */
export function useResumeMonitor(id?: string) {
  const { loading, error, wrap } = useMutationState()
  const resume = useCallback(
    (overrideId?: string) =>
      wrap(async () => {
        const targetId = overrideId ?? id
        if (!targetId) throw { status: 0, message: 'monitor id is required' } as ApiError
        await api.post(`/monitors/${targetId}/resume`)
      }),
    [wrap, id]
  )
  return { resume, loading, error }
}

/** useTestMonitor runs an immediate check and exposes the resulting Check. */
export function useTestMonitor(id?: string) {
  const { loading, error, wrap } = useMutationState()
  const [result, setResult] = useState<Check | null>(null)
  const test = useCallback(
    (overrideId?: string) =>
      wrap(async () => {
        const targetId = overrideId ?? id
        if (!targetId) throw { status: 0, message: 'monitor id is required' } as ApiError
        const { data } = await api.post<ApiResponse<Check>>(`/monitors/${targetId}/test`)
        setResult(data.data)
        return data.data
      }),
    [wrap, id]
  )
  return { test, result, loading, error }
}
