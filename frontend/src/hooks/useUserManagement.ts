import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export type Role = 'admin' | 'user'

export interface ManagedUser {
  id: string
  username: string
  email: string
  role: Role
  is_admin: boolean
  created_at: string
  last_login?: string | null
}

export interface PendingInvitation {
  id: string
  email: string
  role: Role
  invited_by_user_id: string
  expires_at: string
  created_at: string
  token: string
}

export interface CreatedInvitation {
  id: string
  email: string
  role: Role
  token: string
  expires_at: string
  email_sent?: boolean
  email_warning?: string
}

/** All users (any authenticated user may read; the admin page uses the role). */
export function useUsers() {
  const [users, setUsers] = useState<ManagedUser[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<ManagedUser[]>>('/users')
      setUsers(data.data ?? [])
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load users')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { users, loading, error, refetch }
}

/** Pending (non-accepted, non-expired) invitations. */
export function usePendingInvitations() {
  const [invitations, setInvitations] = useState<PendingInvitation[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<PendingInvitation[]>>('/invitations/pending')
      setInvitations(data.data ?? [])
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load invitations')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { invitations, loading, error, refetch }
}

// A generic mutation hook wrapper: exposes a call fn + loading state; throws on
// error so callers can toast the message.
function useMutation<Args extends unknown[], R>(fn: (...args: Args) => Promise<R>) {
  const [loading, setLoading] = useState(false)
  const call = useCallback(
    async (...args: Args): Promise<R> => {
      setLoading(true)
      try {
        return await fn(...args)
      } finally {
        setLoading(false)
      }
    },
    // fn is stable (module-level api calls); intentionally not in deps.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    []
  )
  return { call, loading }
}

export function useCreateUser() {
  const { call, loading } = useMutation(
    async (username: string, email: string, password: string, role: Role) => {
      const { data } = await api.post<ApiResponse<ManagedUser>>('/users', {
        username,
        email,
        password,
        role,
      })
      return data.data
    }
  )
  return { create: call, loading }
}

export function useCreateUserAutoPassword() {
  const { call, loading } = useMutation(async (username: string, email: string, role: Role) => {
    const { data } = await api.post<ApiResponse<{ user: ManagedUser; temporary_password: string }>>(
      '/users/auto-password',
      { username, email, role }
    )
    return data.data
  })
  return { create: call, loading }
}

export function useResetPassword() {
  const { call, loading } = useMutation(async (userId: string, newPassword: string) => {
    await api.post(`/users/${userId}/reset-password`, { new_password: newPassword })
  })
  return { reset: call, loading }
}

export function useResetPasswordAuto() {
  const { call, loading } = useMutation(async (userId: string) => {
    const { data } = await api.post<ApiResponse<{ temporary_password: string }>>(
      `/users/${userId}/reset-password-auto`
    )
    return data.data.temporary_password
  })
  return { reset: call, loading }
}

export function useChangeUserRole() {
  const { call, loading } = useMutation(async (userId: string, role: Role) => {
    await api.patch(`/users/${userId}/role`, { role })
  })
  return { change: call, loading }
}

export function useDeleteUser() {
  const { call, loading } = useMutation(async (userId: string) => {
    await api.delete(`/users/${userId}`)
  })
  return { remove: call, loading }
}

export function useInviteUser() {
  const { call, loading } = useMutation(async (email: string, role: Role, sendEmail: boolean) => {
    const { data } = await api.post<ApiResponse<CreatedInvitation>>('/invitations', {
      email,
      role,
      send_email: sendEmail,
    })
    return data.data
  })
  return { invite: call, loading }
}

export function useResendInvitation() {
  const { call, loading } = useMutation(async (invitationId: string) => {
    await api.post(`/invitations/resend-email/${invitationId}`)
  })
  return { resend: call, loading }
}

export function useCancelInvitation() {
  const { call, loading } = useMutation(async (invitationId: string) => {
    await api.delete(`/invitations/cancel/${invitationId}`)
  })
  return { cancel: call, loading }
}

/** Build the shareable invitation link for a token. */
export function invitationLink(token: string): string {
  return `${window.location.origin}/invitation/${token}`
}
