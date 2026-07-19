import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type {
  AddMonitorToPageInput,
  ApiResponse,
  StatusPage,
  StatusPageDetail,
  StatusPageInput,
  StatusPageMonitorView,
} from '@/types'

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

/** useStatusPages fetches all status pages. */
export function useStatusPages() {
  const [pages, setPages] = useState<StatusPage[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<ApiError | null>(null)

  const refetch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<{ pages: StatusPage[] }>>('/status-pages')
      setPages(data.data.pages ?? [])
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { pages, loading, error, refetch }
}

/** useStatusPage fetches a page and its associated monitors by slug. */
export function useStatusPage(slug: string | undefined) {
  const [page, setPage] = useState<StatusPage | null>(null)
  const [monitors, setMonitors] = useState<StatusPageMonitorView[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const refetch = useCallback(async () => {
    if (!slug) return
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<StatusPageDetail>>(`/status-pages/${slug}`)
      setPage(data.data.page)
      setMonitors(data.data.monitors ?? [])
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [slug])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { page, monitors, loading, error, refetch }
}

/** useCreateStatusPage returns a create() action. */
export function useCreateStatusPage() {
  const { loading, error, wrap } = useMutationState()
  const create = useCallback(
    (input: StatusPageInput) =>
      wrap(async () => {
        const { data } = await api.post<ApiResponse<StatusPage>>('/status-pages', input)
        return data.data
      }),
    [wrap]
  )
  return { create, loading, error }
}

/** useUpdateStatusPage returns an update() action. */
export function useUpdateStatusPage(slug?: string) {
  const { loading, error, wrap } = useMutationState()
  const update = useCallback(
    (input: Partial<StatusPageInput>, overrideSlug?: string) =>
      wrap(async () => {
        const target = overrideSlug ?? slug
        if (!target) throw { status: 0, message: 'slug is required' } as ApiError
        const { data } = await api.put<ApiResponse<StatusPage>>(`/status-pages/${target}`, input)
        return data.data
      }),
    [wrap, slug]
  )
  return { update, loading, error }
}

/** useDeleteStatusPage returns a delete() action. */
export function useDeleteStatusPage(slug?: string) {
  const { loading, error, wrap } = useMutationState()
  const del = useCallback(
    (overrideSlug?: string) =>
      wrap(async () => {
        const target = overrideSlug ?? slug
        if (!target) throw { status: 0, message: 'slug is required' } as ApiError
        await api.delete(`/status-pages/${target}`)
      }),
    [wrap, slug]
  )
  return { delete: del, loading, error }
}

/** useAddMonitorToPage returns an add() action. */
export function useAddMonitorToPage(slug?: string) {
  const { loading, error, wrap } = useMutationState()
  const add = useCallback(
    (input: AddMonitorToPageInput, overrideSlug?: string) =>
      wrap(async () => {
        const target = overrideSlug ?? slug
        if (!target) throw { status: 0, message: 'slug is required' } as ApiError
        await api.post(`/status-pages/${target}/monitors`, input)
      }),
    [wrap, slug]
  )
  return { add, loading, error }
}

/** useRemoveMonitorFromPage returns a remove() action. */
export function useRemoveMonitorFromPage(slug?: string, monitorId?: string) {
  const { loading, error, wrap } = useMutationState()
  const remove = useCallback(
    (overrideSlug?: string, overrideMonitorId?: string) =>
      wrap(async () => {
        const target = overrideSlug ?? slug
        const mid = overrideMonitorId ?? monitorId
        if (!target || !mid) {
          throw { status: 0, message: 'slug and monitorId are required' } as ApiError
        }
        await api.delete(`/status-pages/${target}/monitors/${mid}`)
      }),
    [wrap, slug, monitorId]
  )
  return { remove, loading, error }
}
