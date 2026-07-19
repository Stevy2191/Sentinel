import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type {
  ApiResponse,
  Check,
  Monitor,
  MonitorInput,
  PaginatedMonitors,
} from '@/types'

// A tiny module-level cache so repeated mounts reuse the last fetched list.
let monitorsCache: Monitor[] | null = null

interface UseMonitorsResult {
  monitors: Monitor[]
  loading: boolean
  error: ApiError | null
  refetch: () => Promise<void>
}

/** useMonitors fetches the monitor list (with a simple in-memory cache). */
export function useMonitors(): UseMonitorsResult {
  const [monitors, setMonitors] = useState<Monitor[]>(monitorsCache ?? [])
  const [loading, setLoading] = useState(monitorsCache === null)
  const [error, setError] = useState<ApiError | null>(null)

  const refetch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<PaginatedMonitors>>('/monitors', {
        params: { limit: 500 },
      })
      monitorsCache = data.data.monitors
      setMonitors(data.data.monitors)
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { monitors, loading, error, refetch }
}

/** useMonitor fetches a single monitor by id. */
export function useMonitor(id: string | undefined) {
  const [monitor, setMonitor] = useState<Monitor | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  useEffect(() => {
    if (!id) return
    let active = true
    setLoading(true)
    api
      .get<ApiResponse<Monitor>>(`/monitors/${id}`)
      .then(({ data }) => active && setMonitor(data.data))
      .catch((err) => active && setError(err as ApiError))
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [id])

  return { monitor, loading, error }
}

function invalidate() {
  monitorsCache = null
}

/** useCreateMonitor returns a create() action with loading/error state. */
export function useCreateMonitor() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const create = useCallback(async (input: MonitorInput): Promise<Monitor> => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.post<ApiResponse<Monitor>>('/monitors', input)
      invalidate()
      return data.data
    } catch (err) {
      setError(err as ApiError)
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  return { create, loading, error }
}

/** useUpdateMonitor returns an update() action. */
export function useUpdateMonitor() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const update = useCallback(
    async (id: string, input: Partial<MonitorInput>): Promise<Monitor> => {
      setLoading(true)
      setError(null)
      try {
        const { data } = await api.put<ApiResponse<Monitor>>(`/monitors/${id}`, input)
        invalidate()
        return data.data
      } catch (err) {
        setError(err as ApiError)
        throw err
      } finally {
        setLoading(false)
      }
    },
    []
  )

  return { update, loading, error }
}

/** useDeleteMonitor returns a remove() action. */
export function useDeleteMonitor() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const remove = useCallback(async (id: string): Promise<void> => {
    setLoading(true)
    setError(null)
    try {
      await api.delete(`/monitors/${id}`)
      invalidate()
    } catch (err) {
      setError(err as ApiError)
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  return { remove, loading, error }
}

/** useTestMonitor runs an immediate check and returns the resulting Check. */
export function useTestMonitor() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const test = useCallback(async (id: string): Promise<Check> => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.post<ApiResponse<Check>>(`/monitors/${id}/test`)
      return data.data
    } catch (err) {
      setError(err as ApiError)
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  return { test, loading, error }
}
