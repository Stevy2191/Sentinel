import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type {
  ApiResponse,
  SummaryReport,
  TimelineGranularity,
  TimelineReport,
  UptimeReport,
} from '@/types'

// Shape returned by the real backend endpoint GET /monitors/:id/report.
interface BackendReport {
  monitor: { id: string; name: string; url: string; current_status: string }
  range: { start_time: string; end_time: string }
  uptime: {
    uptime_percentage: number
    downtime_percentage: number
    total_downtime_seconds: number
    incident_count: number
    ongoing_incident: boolean
    current_downtime_minutes: number
  }
  checks: {
    total: number
    success: number
    failed: number
    timeout: number
    avg_response_time_ms: number
  }
}

/**
 * useUptimeReport loads an uptime/SLA report for one monitor. It calls the real
 * backend endpoint GET /monitors/:id/report and derives the SLA verdict from
 * slaTarget on the client.
 */
export function useUptimeReport(
  monitorId: string | undefined,
  startTime?: string,
  endTime?: string,
  slaTarget = 99.9
) {
  const [report, setReport] = useState<UptimeReport | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const load = useCallback(async () => {
    if (!monitorId) return
    setLoading(true)
    setError(null)
    try {
      const params: Record<string, unknown> = {}
      if (startTime) params.start_time = startTime
      if (endTime) params.end_time = endTime
      const { data } = await api.get<ApiResponse<BackendReport>>(
        `/monitors/${monitorId}/report`,
        { params }
      )
      const r = data.data
      setReport({
        monitor_id: r.monitor.id,
        monitor_name: r.monitor.name,
        period: r.range,
        metrics: {
          uptime_percentage: r.uptime.uptime_percentage,
          downtime_percentage: r.uptime.downtime_percentage,
          total_downtime_seconds: r.uptime.total_downtime_seconds,
          incident_count: r.uptime.incident_count,
          total_checks: r.checks.total,
          failed_checks: r.checks.failed + r.checks.timeout,
          avg_response_time_ms: r.checks.avg_response_time_ms,
          // Default to false/0 for resilience if an older backend omits them.
          ongoing_incident: r.uptime.ongoing_incident ?? false,
          current_downtime_minutes: r.uptime.current_downtime_minutes ?? 0,
        },
        sla: {
          target: slaTarget,
          met: r.uptime.uptime_percentage >= slaTarget,
        },
      })
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [monitorId, startTime, endTime, slaTarget])

  useEffect(() => {
    void load()
  }, [load])

  return { report, loading, error }
}

/**
 * useTimeline loads a bucketed timeline.
 * NOTE: the backend endpoint GET /reports/timeline does not exist yet; this hook
 * targets the intended URL and will error until that endpoint is implemented.
 */
export function useTimeline(
  monitorId: string | undefined,
  startTime: string,
  endTime: string,
  granularity: TimelineGranularity = 'hourly'
) {
  const [timeline, setTimeline] = useState<TimelineReport | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const load = useCallback(async () => {
    if (!monitorId) return
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<TimelineReport>>('/reports/timeline', {
        params: { monitor_id: monitorId, start: startTime, end: endTime, granularity },
      })
      setTimeline(data.data)
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [monitorId, startTime, endTime, granularity])

  useEffect(() => {
    void load()
  }, [load])

  return { timeline, loading, error }
}

/**
 * useSummaryReport loads a multi-monitor summary.
 * NOTE: the backend endpoint GET /reports/summary does not exist yet.
 */
export function useSummaryReport(
  startTime: string,
  endTime: string,
  monitorIds?: string[]
) {
  const [report, setReport] = useState<SummaryReport | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const idsKey = (monitorIds ?? []).join(',')

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const params: Record<string, unknown> = { start: startTime, end: endTime }
      if (idsKey) params.monitor_ids = idsKey
      const { data } = await api.get<ApiResponse<SummaryReport>>('/reports/summary', {
        params,
      })
      setReport(data.data)
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [startTime, endTime, idsKey])

  useEffect(() => {
    void load()
  }, [load])

  return { report, loading, error }
}
