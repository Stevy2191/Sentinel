import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'

export type SSLStatus = 'unknown' | 'valid' | 'expiring_soon' | 'expired'

export interface SSLCertificate {
  id: string
  domain: string
  issuer: string | null
  expiry_date: string | null
  days_until_expiry: number | null
  status: SSLStatus
  check_interval: number
  expiry_notification_days: number
  last_checked: string | null
  /** Why the last read failed. Explains a status of "unknown". */
  last_error: string | null
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface SSLCertificateInput {
  domain: string
  expiry_notification_days?: number
  enabled?: boolean
}

const BASE = '/ssl-certificates'

function message(err: unknown, fallback: string): string {
  return (err as ApiError)?.message || fallback
}

/** List every watched domain, with a refetch for after a change. */
export function useSSLCertificates() {
  const [certificates, setCertificates] = useState<SSLCertificate[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await api.get<{ data: SSLCertificate[] }>(BASE)
      setCertificates(res.data.data ?? [])
    } catch (err) {
      setError(message(err, 'Could not load certificates'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { certificates, loading, error, refetch }
}

/**
 * Certificate actions. The create call reads the certificate server-side before
 * it answers, so the returned row already carries a real issuer and expiry
 * rather than "unknown".
 */
export function useSSLCertificateActions() {
  const [busy, setBusy] = useState(false)

  const create = useCallback(async (input: SSLCertificateInput): Promise<SSLCertificate> => {
    setBusy(true)
    try {
      const res = await api.post<{ data: SSLCertificate }>(BASE, input)
      return res.data.data
    } finally {
      setBusy(false)
    }
  }, [])

  const update = useCallback(
    async (id: string, patch: { expiry_notification_days?: number; enabled?: boolean }) => {
      setBusy(true)
      try {
        const res = await api.patch<{ data: SSLCertificate }>(`${BASE}/${id}`, patch)
        return res.data.data
      } finally {
        setBusy(false)
      }
    },
    []
  )

  const remove = useCallback(async (id: string): Promise<void> => {
    setBusy(true)
    try {
      await api.delete(`${BASE}/${id}`)
    } finally {
      setBusy(false)
    }
  }, [])

  const checkNow = useCallback(async (id: string) => {
    setBusy(true)
    try {
      const res = await api.post<{ data: { certificate: SSLCertificate; check_error: string | null } }>(
        `${BASE}/${id}/check-now`
      )
      return res.data.data
    } finally {
      setBusy(false)
    }
  }, [])

  const checkAll = useCallback(async () => {
    setBusy(true)
    try {
      const res = await api.post<{ data: { checked: number; failed: number; message: string } }>(
        `${BASE}/check-all`
      )
      return res.data.data
    } finally {
      setBusy(false)
    }
  }, [])

  return { create, update, remove, checkNow, checkAll, busy }
}
