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
  /** Which channels expiry warnings go to. null means every enabled channel. */
  notify_channels: string[] | null

  // Domain registration — a separate clock from the certificate. A lapsed
  // registration takes the whole domain down and is renewed at the registrar,
  // not by reissuing a certificate.
  /** The registrable domain the registration belongs to, e.g. example.com. */
  registrable_domain: string | null
  registrar: string | null
  domain_expiry_date: string | null
  domain_days_until_expiry: number | null
  domain_status: SSLStatus
  registration_checked_at: string | null
  /** Why the last registration lookup failed. Explains a domain_status of "unknown". */
  registration_error: string | null

  created_at: string
  updated_at: string
}

export interface SSLCertificateInput {
  domain: string
  expiry_notification_days?: number
  enabled?: boolean
  /** Omit for every channel; send [] for none. */
  notify_channels?: string[]
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
    async (
      id: string,
      patch: { expiry_notification_days?: number; enabled?: boolean; notify_channels?: string[] },
    ) => {
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
