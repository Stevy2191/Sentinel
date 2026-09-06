import { useCallback, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type {
  CreateReportPayload,
  CreateSchedulePayload,
  GenerateReportResult,
  ReportJob,
  ReportSchedule,
  ReportTemplate,
  SavedReport,
  ShareLink,
  ShareReportResult,
} from '@/types/reports'

// Hooks for the saved-report builder. This is a separate file from
// useReports.ts, which serves the live analytics page and is unrelated.
//
// All calls go through the shared axios client so the Authorization header is
// attached and errors arrive as a normalized ApiError. The client's baseURL is
// already /api/v1, so paths here are relative to that.

/** useSavedReports lists, generates, deletes, and shares saved report definitions. */
export function useSavedReports() {
  const [reports, setReports] = useState<SavedReport[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const listReports = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<SavedReport[]>>('/reports')
      setReports(data.data ?? [])
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [])

  // Returns as soon as the report is saved and its render is queued. The PDF is
  // not ready yet - callers that need it should await waitForReportJob.
  const createReport = useCallback(
    async (payload: CreateReportPayload) => {
      const { data } = await api.post<ApiResponse<GenerateReportResult>>(
        '/reports/generate',
        payload
      )
      await listReports()
      return data.data
    },
    [listReports]
  )

  const deleteReport = useCallback(
    async (reportId: string) => {
      await api.delete(`/reports/${reportId}`)
      await listReports()
    },
    [listReports]
  )

  // expiresInDays: omit for the backend default (30 days); 0 means never.
  const shareReport = useCallback(async (reportId: string, expiresInDays?: number) => {
    const { data } = await api.post<ApiResponse<ShareReportResult>>(
      `/reports/${reportId}/share`,
      expiresInDays === undefined ? {} : { expires_in_days: expiresInDays }
    )
    return data.data
  }, [])

  return { reports, loading, error, listReports, createReport, deleteReport, shareReport }
}

/** useReportJobs lists a report's render attempts, including failures. */
export function useReportJobs(reportId: string | undefined) {
  const [jobs, setJobs] = useState<ReportJob[]>([])
  const [loading, setLoading] = useState(false)

  const listJobs = useCallback(async () => {
    if (!reportId) return
    setLoading(true)
    try {
      const { data } = await api.get<ApiResponse<ReportJob[]>>(`/reports/${reportId}/jobs`)
      setJobs(data.data ?? [])
    } finally {
      setLoading(false)
    }
  }, [reportId])

  return { jobs, loading, listJobs }
}

/** getReportJob fetches the current state of a queued render. */
export async function getReportJob(jobID: string): Promise<ReportJob> {
  const { data } = await api.get<ApiResponse<ReportJob>>(`/reports/jobs/${jobID}`)
  return data.data
}

/**
 * waitForReportJob polls a render job until it reaches a terminal state.
 *
 * Rendering is off the request path now, so "the report was created" and "its
 * PDF exists" are separate events. onProgress reports each observed state so a
 * caller can say "queued" versus "rendering" rather than a blank spinner.
 *
 * Rejects on a failed job, on timeout, or if a poll itself errors - a silent
 * give-up would leave the UI claiming success for a report that has no file.
 */
export async function waitForReportJob(
  jobID: string,
  options: { timeoutMs?: number; intervalMs?: number; onProgress?: (job: ReportJob) => void } = {}
): Promise<ReportJob> {
  const timeoutMs = options.timeoutMs ?? 120_000
  const intervalMs = options.intervalMs ?? 1_000
  const deadline = Date.now() + timeoutMs

  for (;;) {
    const job = await getReportJob(jobID)
    options.onProgress?.(job)

    if (job.status === 'succeeded') return job
    if (job.status === 'failed') {
      throw new Error(job.error || 'The report could not be generated')
    }
    if (Date.now() >= deadline) {
      throw new Error('Timed out waiting for the report to render; it may still finish in the background')
    }
    await new Promise((resolve) => setTimeout(resolve, intervalMs))
  }
}

/** useShareLinks lists and revokes a report's public share links. */
export function useShareLinks(reportId: string | undefined) {
  const [links, setLinks] = useState<ShareLink[]>([])
  const [loading, setLoading] = useState(false)

  const listLinks = useCallback(async () => {
    if (!reportId) return
    setLoading(true)
    try {
      const { data } = await api.get<ApiResponse<ShareLink[]>>(`/reports/${reportId}/shares`)
      setLinks(data.data ?? [])
    } finally {
      setLoading(false)
    }
  }, [reportId])

  const revokeLink = useCallback(
    async (shareId: string) => {
      if (!reportId) return
      await api.delete(`/reports/${reportId}/shares/${shareId}`)
      await listLinks()
    },
    [reportId, listLinks]
  )

  return { links, loading, listLinks, revokeLink }
}

/** useReportTemplates loads the templates the wizard offers. */
export function useReportTemplates() {
  const [templates, setTemplates] = useState<ReportTemplate[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const listTemplates = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<ReportTemplate[]>>('/report-templates')
      setTemplates(data.data ?? [])
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [])

  return { templates, loading, error, listTemplates }
}

/** useReportSchedules manages one report's delivery schedules. */
export function useReportSchedules(reportId: string | undefined) {
  const [schedules, setSchedules] = useState<ReportSchedule[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const listSchedules = useCallback(async () => {
    if (!reportId) return
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<ReportSchedule[]>>(
        `/reports/${reportId}/schedules`
      )
      setSchedules(data.data ?? [])
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [reportId])

  const createSchedule = useCallback(
    async (payload: CreateSchedulePayload) => {
      if (!reportId) throw new Error('reportId is required')
      const { data } = await api.post<ApiResponse<ReportSchedule>>(
        `/reports/${reportId}/schedules`,
        payload
      )
      await listSchedules()
      return data.data
    },
    [reportId, listSchedules]
  )

  const updateSchedule = useCallback(
    async (scheduleId: string, payload: CreateSchedulePayload) => {
      const { data } = await api.patch<ApiResponse<ReportSchedule>>(
        `/reports/schedules/${scheduleId}`,
        payload
      )
      await listSchedules()
      return data.data
    },
    [listSchedules]
  )

  const deleteSchedule = useCallback(
    async (scheduleId: string) => {
      await api.delete(`/reports/schedules/${scheduleId}`)
      await listSchedules()
    },
    [listSchedules]
  )

  const runScheduleNow = useCallback(
    async (scheduleId: string) => {
      // Synchronous on the backend: this resolves only once the report has
      // actually been generated and delivered, so a rejection is a real failure.
      const { data } = await api.post<ApiResponse<{ message: string; recipients: number }>>(
        `/reports/schedules/${scheduleId}/run`
      )
      await listSchedules()
      return data.data
    },
    [listSchedules]
  )

  return {
    schedules,
    loading,
    error,
    listSchedules,
    createSchedule,
    updateSchedule,
    deleteSchedule,
    runScheduleNow,
  }
}

/** useMonitorTags loads the distinct tags available for tag-scoped reports. */
export function useMonitorTags() {
  const [tags, setTags] = useState<string[]>([])
  const [loading, setLoading] = useState(false)

  const listTags = useCallback(async () => {
    setLoading(true)
    try {
      const { data } = await api.get<ApiResponse<string[]>>('/monitor-tags')
      setTags(data.data ?? [])
    } finally {
      setLoading(false)
    }
  }, [])

  return { tags, loading, listTags }
}

/** usePublicReport loads a shared report by token. No authentication required. */
export function usePublicReport(token: string | undefined) {
  const [report, setReport] = useState<SavedReport | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<ApiError | null>(null)

  const load = useCallback(async () => {
    if (!token) return
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<SavedReport>>(
        `/public/reports/share/${token}`
      )
      setReport(data.data)
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [token])

  return { report, loading, error, load }
}

/**
 * downloadReportPDF fetches a generation and saves it to disk.
 *
 * Auth is no longer the reason this exists. It originally was: the JWT lived in
 * localStorage and only an axios interceptor could attach it, so a browser
 * navigation arrived unauthenticated. The token now lives in an httpOnly cookie
 * scoped to /api, which a same-origin navigation sends automatically - a plain
 * <a href> would authenticate fine today.
 *
 * What it still buys is failure handling. Every error on this route is a JSON
 * body, and a navigation renders that raw in a tab:
 *
 *   session expired      {"error":{"code":401,"message":"..."}}
 *   file pruned on disk  {"error":"report file is no longer available"}
 *   stale generation id  {"error":"report generation not found"}
 *
 * All three are reachable in normal use - the session token lasts 24h, and
 * report files live on a mounted volume that can be replaced. Fetching first
 * lets the caller catch these and show a toast, and lets the filename come
 * from the report's name rather than the server's timestamped one.
 *
 * The tradeoff is that the PDF is buffered in memory. Reports are a few KB
 * today; if they ever grow large, streaming via a link becomes the better
 * choice and this can go.
 */
export async function downloadReportPDF(downloadURL: string, filename: string): Promise<void> {
  // download_url comes back absolute (/api/v1/...) while the client's baseURL is
  // already /api/v1; strip the prefix so it is not doubled.
  const path = downloadURL.replace(/^\/api\/v1/, '')
  const response = await api.get<Blob>(path, { responseType: 'blob' })

  const blobURL = URL.createObjectURL(response.data)
  const link = document.createElement('a')
  link.href = blobURL
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  // Revoke on the next tick; revoking synchronously can cancel the download.
  setTimeout(() => URL.revokeObjectURL(blobURL), 1000)
}

/** formatFileSize renders bytes adaptively - report PDFs are usually KB, not MB. */
export function formatFileSize(bytes?: number | null): string {
  if (bytes == null) return ''
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`
}
