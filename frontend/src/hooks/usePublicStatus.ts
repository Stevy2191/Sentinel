import { useCallback, useEffect, useState } from 'react'
import axios from 'axios'
import { extractError, type ApiError } from '@/services/api'
import type { ApiResponse, PublicStatusData } from '@/types'

/**
 * usePublicStatusPage fetches a public status page. This endpoint lives outside
 * /api/v1 (at /public/status/:slug) and requires no authentication, so it uses a
 * bare axios call (proxied by the dev server in development).
 */
export function usePublicStatusPage(slug: string | undefined) {
  const [data, setData] = useState<PublicStatusData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<ApiError | null>(null)

  const refetch = useCallback(async () => {
    if (!slug) return
    setLoading(true)
    setError(null)
    try {
      const res = await axios.get<ApiResponse<PublicStatusData>>(`/public/status/${slug}`)
      setData(res.data.data)
    } catch (err) {
      const status =
        axios.isAxiosError(err) && err.response ? err.response.status : 0
      const payload =
        axios.isAxiosError(err) && err.response
          ? (err.response.data as { error?: string })?.error
          : undefined
      setError({ status, ...extractError(payload, 'Status page not found') })
    } finally {
      setLoading(false)
    }
  }, [slug])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return {
    page: data?.page ?? null,
    monitors: data?.monitors ?? [],
    summary: data?.summary ?? null,
    loading,
    error,
    refetch,
  }
}
