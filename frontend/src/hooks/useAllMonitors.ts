import { useCallback, useEffect, useRef, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse, Monitor, PaginatedMonitors } from '@/types'
import { fetchAllPages } from '@/utils/monitorPages'

// The API's largest page.
const PAGE_SIZE = 500

/** useAllMonitors is every monitor the user can see, paged through in full
 *  and refreshed every pollMs. loadedAt changes on each successful load, so
 *  rows can refresh what they fetch themselves. */
export function useAllMonitors(pollMs = 30_000) {
  const [monitors, setMonitors] = useState<Monitor[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [loadedAt, setLoadedAt] = useState(0)
  // Each load takes a number; only the newest may write, so a slow earlier
  // load cannot overwrite a newer one.
  const latest = useRef(0)

  const refetch = useCallback(async () => {
    const mine = ++latest.current
    try {
      const all = await fetchAllPages(async (page) => {
        const { data } = await api.get<ApiResponse<PaginatedMonitors>>('/monitors', {
          params: { page, limit: PAGE_SIZE },
        })
        return data.data
      })
      if (mine !== latest.current) return
      setMonitors(all)
      setError(null)
      setLoadedAt(Date.now())
    } catch (err) {
      if (mine !== latest.current) return
      setError((err as ApiError).message || 'Failed to load monitors')
    } finally {
      if (mine === latest.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
    const t = window.setInterval(() => void refetch(), pollMs)
    return () => window.clearInterval(t)
  }, [refetch, pollMs])

  return { monitors, loading, error, loadedAt, refetch }
}
