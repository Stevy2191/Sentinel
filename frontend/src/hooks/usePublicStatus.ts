import { useCallback, useEffect, useState } from 'react'
import axios from 'axios'
import { extractError, type ApiError } from '@/services/api'
import type { ApiResponse, PublicStatusData } from '@/types'

/**
 * usePublicStatusPage fetches a public status page. The JSON endpoint lives at
 * /api/v1/public/status/:slug (no authentication required) — deliberately under
 * /api/ so it does not collide with the SPA route of the same human-facing name
 * (/public/status/:slug), which the web server serves as the app. Uses a bare
 * axios call so no auth token/interceptors are involved.
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
      const res = await axios.get<ApiResponse<PublicStatusData>>(`/api/v1/public/status/${slug}`)
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
