import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export type HttpMethod = 'get' | 'post' | 'put' | 'patch' | 'delete'

export interface UseApiOptions {
  /** Run the request automatically on mount. Defaults to true for GET. */
  auto?: boolean
  /** Query params. */
  params?: Record<string, unknown>
}

export interface UseApiResult<T> {
  data: T | null
  loading: boolean
  error: ApiError | null
  refetch: () => Promise<T>
}

/**
 * Generic hook for calling any endpoint. Unwraps the standard { success, data }
 * envelope and exposes loading/error state plus a refetch action.
 */
export function useApi<T>(
  method: HttpMethod,
  endpoint: string,
  body?: unknown,
  options?: UseApiOptions
): UseApiResult<T> {
  const auto = options?.auto ?? method === 'get'
  const paramsKey = JSON.stringify(options?.params ?? null)
  const bodyKey = JSON.stringify(body ?? null)

  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState<boolean>(auto)
  const [error, setError] = useState<ApiError | null>(null)

  const refetch = useCallback(async (): Promise<T> => {
    setLoading(true)
    setError(null)
    try {
      const res = await api.request<ApiResponse<T>>({
        method,
        url: endpoint,
        data: body,
        params: options?.params,
      })
      setData(res.data.data)
      return res.data.data
    } catch (err) {
      setError(err as ApiError)
      throw err
    } finally {
      setLoading(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [method, endpoint, bodyKey, paramsKey])

  useEffect(() => {
    if (auto) void refetch().catch(() => undefined)
  }, [auto, refetch])

  return { data, loading, error, refetch }
}

export default useApi
