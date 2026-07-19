// Canonical API types for the Sentinel backend.

// ---- Envelope & pagination -------------------------------------------------

export interface ApiResponseError {
  code?: string
  message: string
}

// The backend currently sends `error` as a plain string; this union tolerates
// both that and a future { code, message } object.
export interface ApiResponse<T> {
  success: boolean
  data: T
  error?: ApiResponseError | string
}

export interface PaginationParams {
  page?: number
  limit?: number
  offset?: number
}

export interface Pagination {
  page?: number
  limit: number
  offset?: number
  total: number
  pages?: number
}

// ---- Monitors --------------------------------------------------------------

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

export interface MonitorFilters extends PaginationParams {
  enabled?: boolean
  type?: MonitorType
  status?: MonitorStatus
}

export interface PaginatedMonitors {
  monitors: Monitor[]
  pagination: Pagination
}

// ---- Checks & incidents ----------------------------------------------------

export interface Check {
  id: number
  monitor_id: string
  status: CheckStatus
  response_time_ms: number
  status_code: number
  error_message: string
  timestamp: string
}

export interface PaginatedChecks {
  checks: Check[]
  pagination: Pagination
  range?: { start_time: string; end_time: string }
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

// ---- Reports ---------------------------------------------------------------

export interface UptimeReport {
  monitor_id: string
  monitor_name: string
  period: { start_time: string; end_time: string }
  metrics: {
    uptime_percentage: number
    downtime_percentage: number
    total_downtime_seconds: number
    incident_count: number
    total_checks: number
    failed_checks: number
    avg_response_time_ms: number
  }
  sla: { target: number; met: boolean }
}

export type TimelineGranularity = 'hourly' | 'daily'

export interface TimelineBucket {
  timestamp: string
  uptime_percent: number
  avg_response_time_ms: number
  checks_total: number
  checks_failed: number
}

export interface TimelineReport {
  monitor_id: string
  monitor_name: string
  granularity: TimelineGranularity
  period: { start: string; end: string }
  timeline: TimelineBucket[]
}

export interface SummaryMonitor {
  monitor_id: string
  monitor_name: string
  uptime_percent: number
  downtime_minutes: number
  status: MonitorStatus
}

export interface SummaryAggregate {
  avg_uptime: number
  best_uptime: number
  worst_uptime: number
  total_incidents: number
  total_downtime_minutes: number
}

export interface SummaryReport {
  period: { start: string; end: string }
  monitors: SummaryMonitor[]
  aggregate: SummaryAggregate
}

// ---- Status pages ----------------------------------------------------------

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

export interface StatusPageInput {
  slug: string
  name: string
  description?: string
  logo_url?: string
  theme_color?: string
  published?: boolean
}

export interface StatusPageMonitorView {
  id: string
  name: string
  group_name?: string
  status: MonitorStatus
  response_time_ms: number
  uptime_percent?: number
  position?: number
}

export interface StatusPageDetail {
  page: StatusPage
  monitors: StatusPageMonitorView[]
}

export interface AddMonitorToPageInput {
  monitor_id: string
  group_name?: string
  position?: number
}

// ---- Public status page ----------------------------------------------------

export interface PublicMonitor {
  id: string
  name: string
  group: string
  status: MonitorStatus
  last_check: string | null
  response_time_ms: number
  uptime: { last_7_days: number; last_30_days: number; last_90_days: number }
  recent_incidents: { start: string; end: string | null; duration_minutes: number }[]
}

export interface PublicSummary {
  total_monitors: number
  online: number
  offline: number
  last_updated: string
}

export interface PublicStatusData {
  page: {
    name: string
    description: string
    logo_url: string
    theme_color: string
    updated_at: string
  }
  monitors: PublicMonitor[]
  summary: PublicSummary
}

// ---- Notifications (endpoints to be created server-side) --------------------

export interface NotificationChannel {
  name: string
  enabled: boolean
  settings: Record<string, string>
}

export interface NotificationHistoryItem {
  id: string
  monitor_id: string
  channel: string
  status: string
  error_message?: string
  sent_at?: string
  created_at: string
}
