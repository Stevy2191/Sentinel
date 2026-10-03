import { useEffect, useState } from 'react'
import axios from 'axios'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type { MetricsRange } from '@/hooks/useMetrics'
import type { WidgetResponse } from '@/types/dashboards'
import { isStale } from '@/utils/dashboards'

export type WidgetSource =
  | { kind: 'dashboard'; dashboardId: string; widgetId: string; override: MetricsRange | '' }
  | { kind: 'public'; token: string; widgetId: string }

const MAX_BACKOFF_MS = 5 * 60 * 1000

async function fetchWidget<T>(src: WidgetSource, signal: AbortSignal): Promise<WidgetResponse<T>> {
  if (src.kind === 'public') {
    // A bare axios call: the public page has no session and must not trigger
    // the app's auth handling. The timeout turns a hung request into a retry.
    const res = await axios.get<ApiResponse<WidgetResponse<T>>>(
      `/api/v1/public/dashboards/${src.token}/widgets/${src.widgetId}/data`,
      { signal, timeout: 60000 },
    )
    return res.data.data
  }
  const res = await api.get<ApiResponse<WidgetResponse<T>>>(
    `/dashboards/${src.dashboardId}/widgets/${src.widgetId}/data`,
    { params: src.override ? { range: src.override } : {}, signal },
  )
  return res.data.data
}

/** One widget's data, refreshed when the server says to. A failed request is
 *  retried with backoff (5 s doubling, capped at 5 minutes) and the last good
 *  data stays on screen, marked stale once it is three refresh periods old. */
export function useWidgetData<T>(source: WidgetSource | null) {
  const [response, setResponse] = useState<WidgetResponse<T> | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [lastSuccess, setLastSuccess] = useState<number | null>(null)
  const [now, setNow] = useState(() => Date.now())
  // The effect keys on the source's value, not its identity, so a parent that
  // rebuilds the object each render does not refetch.
  const key = source ? JSON.stringify(source) : ''

  useEffect(() => {
    if (!key) return
    const src = JSON.parse(key) as WidgetSource
    let cancelled = false
    let timer: number | undefined
    let failures = 0
    const controller = new AbortController()
    const load = async () => {
      try {
        const resp = await fetchWidget<T>(src, controller.signal)
        if (cancelled) return
        failures = 0
        setResponse(resp)
        setError(null)
        setLastSuccess(Date.now())
        if (resp.refresh_seconds > 0) timer = window.setTimeout(() => void load(), resp.refresh_seconds * 1000)
      } catch (err) {
        if (cancelled) return
        failures++
        const msg = axios.isAxiosError(err)
          ? ((err.response?.data as { error?: string } | undefined)?.error ?? err.message)
          : (err as ApiError).message
        setError(msg || 'Could not load this widget')
        timer = window.setTimeout(() => void load(), Math.min(MAX_BACKOFF_MS, 5000 * 2 ** Math.min(failures - 1, 6)))
      }
    }
    void load()
    return () => {
      cancelled = true
      controller.abort()
      window.clearTimeout(timer)
    }
  }, [key])

  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 15000)
    return () => window.clearInterval(t)
  }, [])

  return { response, error, lastSuccess, stale: isStale(lastSuccess, response?.refresh_seconds ?? 0, now) }
}
