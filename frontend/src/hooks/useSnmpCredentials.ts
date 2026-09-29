import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export type SnmpVersion = '1' | '2c' | '3'
export const AUTH_PROTOCOLS = ['none', 'MD5', 'SHA', 'SHA224', 'SHA256', 'SHA384', 'SHA512'] as const
export const PRIV_PROTOCOLS = ['none', 'DES', 'AES', 'AES192', 'AES256'] as const

/** A profile as the API shows it: secrets are never sent, only whether set. */
export interface CredentialView {
  id: string
  name: string
  site_id: string | null
  site_name: string
  version: SnmpVersion
  username: string
  auth_protocol: string
  priv_protocol: string
  has_community: boolean
  has_auth_password: boolean
  has_priv_password: boolean
  used_by: number
  created_at: string
  updated_at: string
}

/** Secrets left empty on update keep the stored value. */
export interface CredentialInput {
  name: string
  site_id: string | null
  version: SnmpVersion
  community?: string
  username?: string
  auth_protocol?: string
  auth_password?: string
  priv_protocol?: string
  priv_password?: string
}

export interface CredentialOption {
  id: string
  name: string
  version: SnmpVersion
  site_id: string | null
}

/** Every profile (admin only; the API enforces it). */
export function useSnmpCredentials() {
  const [credentials, setCredentials] = useState<CredentialView[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<CredentialView[]>>('/snmp-credentials')
      setCredentials(data.data ?? [])
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load credential profiles')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  const create = useCallback(
    async (input: CredentialInput) => {
      const { data } = await api.post<ApiResponse<CredentialView>>('/snmp-credentials', input)
      await refetch()
      return data.data
    },
    [refetch]
  )
  const update = useCallback(
    async (id: string, input: CredentialInput) => {
      const { data } = await api.put<ApiResponse<CredentialView>>(`/snmp-credentials/${id}`, input)
      await refetch()
      return data.data
    },
    [refetch]
  )
  const remove = useCallback(
    async (id: string) => {
      await api.delete(`/snmp-credentials/${id}`)
      await refetch()
    },
    [refetch]
  )

  return { credentials, loading, error, refetch, create, update, remove }
}

/** Profiles usable in a site, by name (editors of that site). */
export function useSiteCredentialOptions(siteId: string | undefined, enabled: boolean) {
  const [options, setOptions] = useState<CredentialOption[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!siteId || !enabled) {
      setOptions([])
      return
    }
    let active = true
    setLoading(true)
    api
      .get<ApiResponse<CredentialOption[]>>(`/sites/${siteId}/credentials`)
      .then((res) => active && setOptions(res.data.data ?? []))
      .catch(() => active && setOptions([]))
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [siteId, enabled])

  return { options, loading }
}
