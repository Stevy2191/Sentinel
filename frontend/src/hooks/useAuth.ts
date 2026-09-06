import { useCallback, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuthContext } from '@/context/AuthContext'

interface LoginResult {
  success: boolean
  mfaRequired?: boolean
  mfaToken?: string
}

interface ApiEnvelope {
  data?: {
    token?: string
    mfa_required?: boolean
    mfa_token?: string
  }
  error?: { message?: string }
}

/**
 * useAuth performs the auth network flows. Auth endpoints are public, so it uses
 * fetch (via the dev proxy / same origin). On success it stores the token in the
 * AuthContext (which persists to localStorage) and redirects.
 */
export function useAuth() {
  const navigate = useNavigate()
  const { refreshAuth } = useAuthContext()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const login = useCallback(
    async (username: string, password: string): Promise<LoginResult> => {
      setLoading(true)
      setError(null)
      try {
        const res = await fetch('/api/v1/auth/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username, password }),
        })
        const data: ApiEnvelope = await res.json()
        if (!res.ok) {
          setError(data.error?.message || 'Login failed')
          return { success: false }
        }
        if (data.data?.mfa_required) {
          return { success: false, mfaRequired: true, mfaToken: data.data.mfa_token }
        }
	// The backend already set the auth cookie on this response; pull the
        // user record so AuthContext flips isAuthenticated to true.
        await refreshAuth()
        navigate('/dashboard')
        return { success: true }
      } catch {
        setError('Network error. Please try again.')
        return { success: false }
      } finally {
        setLoading(false)
      }
    },
    [navigate, refreshAuth]
  )

  const register = useCallback(
    async (username: string, password: string, confirmPassword: string): Promise<{ success: boolean }> => {
      setLoading(true)
      setError(null)
      if (password !== confirmPassword) {
        setError('Passwords do not match')
        setLoading(false)
        return { success: false }
      }
      try {
        const res = await fetch('/api/v1/auth/register', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username, password, password_confirm: confirmPassword }),
        })
        const data: ApiEnvelope = await res.json()
        if (!res.ok) {
          setError(data.error?.message || 'Registration failed')
          return { success: false }
        }
        return { success: true }
      } catch {
        setError('Network error. Please try again.')
        return { success: false }
      } finally {
        setLoading(false)
      }
    },
    []
  )

  const verifyMFA = useCallback(
    async (mfaToken: string, totpCode?: string, backupCode?: string): Promise<{ success: boolean }> => {
      setLoading(true)
      setError(null)
      try {
        // The backend checks the same field against both TOTP and backup codes.
        const res = await fetch('/api/v1/auth/mfa/verify', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ mfa_token: mfaToken, totp_code: totpCode || backupCode }),
        })
        const data: ApiEnvelope = await res.json()
        if (!res.ok) {
          setError(data.error?.message || 'Verification failed')
          return { success: false }
        }
	await refreshAuth()
        navigate('/dashboard')
        return { success: true }
      } catch {
        setError('Network error. Please try again.')
        return { success: false }
      } finally {
        setLoading(false)
      }
    },
    [navigate, refreshAuth]
  )

  return { login, register, verifyMFA, loading, error, setError }
}
