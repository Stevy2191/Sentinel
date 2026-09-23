// Types for the saved-report builder: definitions, generations, schedules, and
// the templates that shape them. These mirror the backend models in
// internal/models/report.go and report_schedule.go.

export type ReportScopeType = 'monitors' | 'tags' | 'groups' | 'types'
export type ScheduleType = 'daily' | 'weekly' | 'monthly' | 'quarterly' | 'custom'

/** The two fixed report types. No template system, no section picker. */
export type ReportType = 'uptime' | 'incident'

export const REPORT_TYPE_LABEL: Record<ReportType, string> = {
  uptime: 'Uptime Report',
  incident: 'Incident Report',
}

export interface IncidentSummary {
  id: string
  start_time: string
  end_time?: string | null
  /** Overlap with the report window, in minutes - not the incident's full length. */
  duration_minutes: number
  severity: string
  status: string
  root_cause: string
  resolution_notes: string
}

export interface ReportMetrics {
  monitor_id: string
  monitor_name: string
  uptime: number
  downtime_minutes: number
  incident_count: number
  sla_target?: number | null
  sla_met: boolean
  incidents: IncidentSummary[]
}

export interface ReportGeneration {
  id: string
  generated_at: string
  file_size?: number | null
  download_url: string
}

export interface SavedReport {
  id: string
  name: string
  report_type: ReportType
  scope_type: ReportScopeType
  time_range_days: number
  /** The window in words — "August 2026", "Q2 2026", "Last 7 days". */
  period_label?: string
  period_kind?: ReportPeriodKind
  period_unit?: string
  created_at: string
  updated_at: string
  last_generated?: string | null
  generations: ReportGeneration[]
}

export interface ReportSchedule {
  id: string
  report_id: string
  send_hour?: number
  send_minute?: number
  day_of_week?: number | null
  day_of_month?: number | null
  /** What the runner evaluates, so the UI need not reimplement the mapping. */
  cron_expression_resolved?: string
  schedule_type: ScheduleType
  cron_expression?: string | null
  email_recipients: string[]
  send_as_attachment: boolean
  include_in_email: { include_link: boolean; include_summary: boolean }
  last_run_at?: string | null
  next_run_at?: string | null
  is_active: boolean
  created_at: string
  updated_at: string
}

/** Scope payload; exactly one field is set, matching scope_type. */
export interface ReportScopeData {
  monitor_ids?: string[]
  tags?: string[]
  group_ids?: string[]
}

export interface CreateReportPayload {
  name: string
  report_type: ReportType
  scope_type: ReportScopeType
  scope_data: ReportScopeData
  /** Required only for a rolling period, which is the default on the server. */
  time_range_days?: number
  period_kind?: ReportPeriodKind
  period_unit?: 'week' | 'month' | 'quarter' | 'year'
  period_offset?: number
  period_start?: string
  period_end?: string
  custom_title?: string
  custom_description?: string
}

export interface CreateSchedulePayload {
  schedule_type: ScheduleType
  cron_expression?: string
  /** Local time of day, read in the instance's report timezone. */
  send_hour?: number
  send_minute?: number
  /** Weekly only, 0 = Sunday. */
  day_of_week?: number
  /** Monthly and quarterly only, 1-28. */
  day_of_month?: number
  email_recipients: string[]
  send_as_attachment: boolean
  /**
   * Replaced wholesale by the backend when present, so always send both fields
   * — a partial object silently resets the one that is omitted.
   */
  include_in_email: { include_link: boolean; include_summary: boolean }
  /** Omitted on create (defaults to active); sent when editing. */
  is_active?: boolean
}

export type ReportJobStatus = 'queued' | 'running' | 'succeeded' | 'failed'

/**
 * Response from POST /reports/generate, which returns 202. The report exists
 * immediately; its PDF is rendered by a worker, so the caller polls job_url.
 */
export interface GenerateReportResult {
  id: string
  job_id: string
  status: ReportJobStatus
  job_url: string
}

/** Response from GET /reports/jobs/:job_id. */
export interface ReportJob {
  id: string
  report_id: string
  status: ReportJobStatus
  generation_id?: string | null
  download_url?: string | null
  error?: string | null
  attempts: number
  created_at: string
  finished_at?: string | null
}

/** Response from POST /reports/:id/share. */
export interface ShareReportResult {
  share_token: string
  share_link: string
  /** Null means the link never expires. */
  expires_at?: string | null
}

/** An existing share link, from GET /reports/:id/shares. */
export interface ShareLink {
  id: string
  share_token: string
  share_link: string
  expires_at?: string | null
  expired: boolean
  created_at: string
}

/** How a report's window is worked out. */
export type ReportPeriodKind = 'rolling' | 'calendar' | 'custom'

export interface ReportPeriod {
  period_kind: ReportPeriodKind
  /** Rolling only. */
  time_range_days?: number
  /** Calendar only. */
  period_unit?: 'week' | 'month' | 'quarter' | 'year'
  /** Calendar only: 0 is the period in progress, 1 the one before it. */
  period_offset?: number
  /** Custom only, RFC3339. */
  period_start?: string
  period_end?: string
}
