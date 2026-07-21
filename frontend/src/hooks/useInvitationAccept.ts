import { useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export interface InvitationDetails {
  email: string
  role: 'admin' | 'user'
  expires_at: string
  expired: boolean
  accepted: boolean
}

export interface AcceptedAccount {
  token: string
  user_id: string
  username: string
  is_admin: boolean
  role: 'admin' | 'user'
}

/** Load public invitation details for a token. */
export function useInvitationDetails(token: string | undefined) {
  const [invitation, setInvitation] = useState<InvitationDetails | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!token) {
      setError('Missing invitation token')
      setLoading(false)
      return
    }
    let active = true
    setLoading(true)
    setError(null)
    api
      .get<ApiResponse<InvitationDetails>>(`/invitations/${token}`)
      .then((res) => active && setInvitation(res.data.data))
      .catch((err: ApiError) => active && setError(err.message || 'This invitation link is invalid'))
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [token])

  return { invitation, loading, error }
}

/** Accept an invitation, creating the account and returning a login token. */
export function useAcceptInvitation() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const accept = async (token: string, username: string, password: string): Promise<AcceptedAccount> => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.post<ApiResponse<AcceptedAccount>>(`/invitations/${token}/accept`, {
        username,
        password,
      })
      return data.data
    } catch (err) {
      setError((err as ApiError).message || 'Failed to accept invitation')
      throw err
    } finally {
      setLoading(false)
    }
  }

  return { accept, loading, error }
}
