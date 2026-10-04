import { useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type { NetworkScopeType, ReportScopeData, ScopePreview } from '@/types/reports'

/** Long enough that ticking a run of checkboxes asks once, not per tick. */
const DEBOUNCE_MS = 400

export interface ScopePreviewState {
  /** The answer for the scope as it is now; null while asking or after an error. */
  data: ScopePreview | null
  /** The server's message: "A chosen port, device or site is not available", and the like. */
  error: string | null
  /** A complete scope is waiting for its answer (debounce or request). */
  loading: boolean
}

/**
 * Sizes a network scope and lists its metrics (POST /reports/scope-preview),
 * about 400 ms after it stops changing. Pass nulls while the scope is
 * incomplete: nothing is asked. Each answer is kept with the request it
 * answers, so a late answer for an earlier scope is never shown as the
 * current one.
 */
export function useScopePreview(scopeType: NetworkScopeType | null, scopeData: ReportScopeData | null): ScopePreviewState {
  // Keyed on the value, not the object, so a caller that rebuilds the scope
  // each render does not ask again.
  const key = scopeType && scopeData ? JSON.stringify({ scope_type: scopeType, scope_data: scopeData }) : ''
  const [answer, setAnswer] = useState<{ key: string; data: ScopePreview | null; error: string | null }>({
    key: '',
    data: null,
    error: null,
  })

  useEffect(() => {
    if (!key) return
    let cancelled = false
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      api
        .post<ApiResponse<ScopePreview>>('/reports/scope-preview', JSON.parse(key), { signal: controller.signal })
        .then(({ data }) => {
          if (!cancelled) setAnswer({ key, data: data.data, error: null })
        })
        .catch((err: ApiError) => {
          if (!cancelled) setAnswer({ key, data: null, error: err.message || 'Could not check this scope' })
        })
    }, DEBOUNCE_MS)
    return () => {
      cancelled = true
      controller.abort()
      window.clearTimeout(timer)
    }
  }, [key])

  const current = key !== '' && answer.key === key
  return {
    data: current ? answer.data : null,
    error: current ? answer.error : null,
    loading: key !== '' && !current,
  }
}
