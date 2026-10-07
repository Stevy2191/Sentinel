import { useCallback, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useAuthContext } from '@/context/AuthContext'

// Shown when a sign-in request gets no answer at all. With a self-signed
// certificate this usually means the certificate rotated and the browser no
// longer accepts it; reloading lets the browser show its warning.
const UNREACHABLE =
  "Can't reach Sentinel. Reload the page; if your browser shows a certificate warning, accept it and try again."

/** returnPath is where to go after signing in: the page RequireAuth sent the
 *  user here from, when it is a path inside Sentinel, else the dashboard. */
function returnPath(state: unknown): string {
  const from = (state as { from?: unknown } | null)?.from
  if (typeof from === 'string' && from.startsWith('/') && !from.startsWith('//') && !from.startsWith('/login')) {
    return from
  }
  return '/dashboard'
}

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
  const location = useLocation()
  const afterSignIn = returnPath(location.state)
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
        navigate(afterSignIn)
        return { success: true }
      } catch {
        setError(UNREACHABLE)
        return { success: false }
      } finally {
        setLoading(false)
      }
    },
    [navigate, refreshAuth, afterSignIn]
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
        setError(UNREACHABLE)
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
        navigate(afterSignIn)
        return { success: true }
      } catch {
        setError(UNREACHABLE)
        return { success: false }
      } finally {
        setLoading(false)
      }
    },
    [navigate, refreshAuth, afterSignIn]
  )

  return { login, register, verifyMFA, loading, error, setError }
}
