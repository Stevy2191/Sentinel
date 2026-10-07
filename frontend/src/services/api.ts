import axios, { AxiosError, type AxiosInstance } from 'axios'
import type { ApiResponseError } from '@/types'

// Base URL resolution (Vite exposes VITE_* and REACT_APP_* via envPrefix):
// - VITE_API_URL, else REACT_APP_API_URL if set.
// - Otherwise '/api/v1', which the Vite dev server proxies to the backend so
//   the browser makes same-origin requests (avoids CORS).
// Exported for clients axios cannot serve, such as the EventSource that
// follows a network tool run.
export const apiBaseURL: string =
  import.meta.env.VITE_API_URL || import.meta.env.REACT_APP_API_URL || '/api/v1'

export const api: AxiosInstance = axios.create({
  baseURL: apiBaseURL,
  headers: { 'Content-Type': 'application/json' },
  timeout: 120000,
  // The auth token lives in an httpOnly cookie set by the backend, scoped to
  // /api. withCredentials is what makes axios send it.
  withCredentials: true,
})

// A normalized API error surfaced to the UI.
export interface ApiError {
  status: number
  message: string
  code?: string
  /** The whole error body, for endpoints that add fields to it (a dashboard
   *  save adds widget_index and field; a conflict adds current_version). */
  details?: Record<string, unknown>
}

// No request interceptor for auth: nothing writes 'sentinel:token' to
// localStorage any more, so reading it back only ever produced null. The
// cookie above is the SPA's credential. The backend still accepts
// "Authorization: Bearer" for scripts and non-browser clients.

/** Extract a human-readable message from the backend error payload, which may
 *  be a plain string or a { code, message } object. */
export function extractError(
  payload: ApiResponseError | string | undefined,
  fallback: string
): { message: string; code?: string } {
  if (!payload) return { message: fallback }
  if (typeof payload === 'string') return { message: payload }
  return { message: payload.message || fallback, code: payload.code }
}

// Told when the API refuses a request for want of a session: the 24-hour
// session ran out, or the user signed out in another tab. AuthContext clears
// the signed-in user, so the route guard sends the page to /login instead of
// leaving it polling and failing. The /auth/* endpoints are left out: a wrong
// password or MFA code answers 401 there too, which is not an expired session.
const unauthorizedListeners = new Set<() => void>()

/** onUnauthorized registers listener and returns a function that removes it. */
export function onUnauthorized(listener: () => void): () => void {
  unauthorizedListeners.add(listener)
  return () => {
    unauthorizedListeners.delete(listener)
  }
}

// Response interceptor: normalize errors into ApiError.
api.interceptors.response.use(
  (response) => response,
  (error: AxiosError<{ error?: ApiResponseError | string }>) => {
    const status = error.response?.status ?? 0
    if (status === 401 && !(error.config?.url ?? '').startsWith('/auth/')) {
      unauthorizedListeners.forEach((listener) => listener())
    }
    const { message, code } = extractError(
      error.response?.data?.error,
      error.message || 'An unexpected error occurred'
    )
    const details = error.response?.data as unknown as Record<string, unknown> | undefined
    const apiError: ApiError = { status, message, code, details }
    return Promise.reject(apiError)
  }
)

export default api
