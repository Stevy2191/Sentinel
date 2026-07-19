import axios, { AxiosError, type AxiosInstance } from 'axios'

// Base URL resolution:
// - REACT_APP_API_URL if set (exposed via Vite's envPrefix).
// - Otherwise '/api/v1', which the Vite dev server proxies to the backend
//   (see vite.config.ts) so the browser makes same-origin requests.
const baseURL = import.meta.env.REACT_APP_API_URL || '/api/v1'

export const api: AxiosInstance = axios.create({
  baseURL,
  headers: { 'Content-Type': 'application/json' },
  timeout: 15000,
})

// Request interceptor: attach an auth token if present (placeholder for later).
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('sentinel-token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// A normalized API error surfaced to the UI.
export interface ApiError {
  status: number
  message: string
}

// Response interceptor: normalize errors into ApiError.
api.interceptors.response.use(
  (response) => response,
  (error: AxiosError<{ error?: string }>) => {
    const status = error.response?.status ?? 0
    const message =
      error.response?.data?.error || error.message || 'An unexpected error occurred'
    return Promise.reject({ status, message } satisfies ApiError)
  }
)

export default api
