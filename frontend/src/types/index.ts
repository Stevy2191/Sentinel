// Types mirroring the Sentinel backend API payloads.

export type MonitorType = 'http' | 'tcp' | 'ping' | 'dns' | 'webhook'
export type MonitorStatus = 'online' | 'offline' | 'unknown'
export type CheckStatus = 'success' | 'failed' | 'timeout'

export interface Monitor {
  id: string
  name: string
  description: string
  type: MonitorType
  url: string
  method: string
  headers: Record<string, string> | null
  body: string
  interval_seconds: number
  timeout_seconds: number
  retries: number
  current_status: MonitorStatus
  last_check_at: string | null
  last_response_time_ms: number
  enabled: boolean
  tags: string[] | null
  created_at: string
  updated_at: string
}

// Fields accepted when creating/updating a monitor.
export interface MonitorInput {
  name: string
  description?: string
  type: MonitorType
  url: string
  method?: string
  headers?: Record<string, string>
  body?: string
  interval_seconds: number
  timeout_seconds: number
  retries?: number
  enabled?: boolean
  tags?: string[]
}

export interface Check {
  id: number
  monitor_id: string
  status: CheckStatus
  response_time_ms: number
  status_code: number
  error_message: string
  timestamp: string
}

export interface Incident {
  id: string
  monitor_id: string
  start_time: string
  end_time: string | null
  duration_seconds: number
  severity: string
  root_cause: string
  notes: string
}

export interface Pagination {
  page?: number
  limit: number
  offset?: number
  total: number
  pages?: number
}

// Standard API envelope: { success, data } or { success:false, error }.
export interface ApiResponse<T> {
  success: boolean
  data: T
  error?: string
}

export interface PaginatedMonitors {
  monitors: Monitor[]
  pagination: Pagination
}

export interface StatusPage {
  id: string
  slug: string
  name: string
  description: string
  logo_url: string
  theme_color: string
  published: boolean
  created_at: string
  updated_at: string
}
