import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export interface ScanResult {
  host: string
  port: number
  name: string
  descr: string
  object_id: string
  vendor: string
  credential_id: string
  credential_name: string
  already_added: boolean
}

export interface ScanJob {
  id: string
  site_id: string
  cidr: string
  total: number
  done: number
  running: boolean
  results: ScanResult[]
  started_at: string
  finished_at: string | null
}

/** Starts a scan and polls it every second until it finishes. */
export function useSiteScan(siteId: string) {
  const [job, setJob] = useState<ScanJob | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const start = useCallback(
    async (cidr: string, credentialIds: string[]) => {
      setBusy(true)
      setError(null)
      try {
        const { data } = await api.post<ApiResponse<ScanJob>>(`/sites/${siteId}/scans`, {
          cidr,
          credential_ids: credentialIds,
        })
        setJob(data.data)
      } catch (err) {
        setError((err as ApiError).message || 'Could not start the scan')
      } finally {
        setBusy(false)
      }
    },
    [siteId]
  )

  useEffect(() => {
    if (!job?.running) return
    const t = window.setInterval(async () => {
      try {
        const { data } = await api.get<ApiResponse<ScanJob>>(`/sites/${siteId}/scans/${job.id}`)
        setJob(data.data)
      } catch (err) {
        setError((err as ApiError).message || 'Lost track of the scan')
        setJob((j) => (j ? { ...j, running: false } : j))
      }
    }, 1000)
    return () => window.clearInterval(t)
  }, [job?.id, job?.running, siteId])

  const addDevices = useCallback(
    async (rows: ScanResult[]) => {
      const { data } = await api.post<ApiResponse<{ added: unknown[]; failed: { host: string; error: string }[] }>>(
        `/sites/${siteId}/devices`,
        { devices: rows.map((r) => ({ host: r.host, port: r.port, credential_id: r.credential_id, name: r.name })) }
      )
      return data.data
    },
    [siteId]
  )

  return { job, start, busy, error, addDevices }
}
