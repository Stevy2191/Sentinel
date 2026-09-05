import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import api from '@/services/api'

export interface UserTheme {
  primary_color: string
  accent_color: string
  mode: string
}

export interface CurrentUser {
  user_id: string
  username: string
  is_admin: boolean
  mfa_enabled: boolean
  last_login: string | null
  theme?: UserTheme
}

interface AuthContextValue {
  currentUser: CurrentUser | null
  isAuthenticated: boolean
  authChecked: boolean
  setCurrentUser: (user: CurrentUser | null) => void
  getCurrentUser: () => Promise<CurrentUser | null>
  /** Call after a successful login/MFA-verify/invitation-accept response
   *  (the backend has already set the auth cookie) to populate currentUser. */
  refreshAuth: () => Promise<CurrentUser | null>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [currentUser, setCurrentUser] = useState<CurrentUser | null>(null)
  // authChecked distinguishes "haven't checked yet" from "checked, not
  // logged in", so RequireAuth doesn't redirect to /login on the initial
  // render before the /auth/me probe has come back.
  const [authChecked, setAuthChecked] = useState(false)

  // Fetch (and refresh) the current user from /auth/me.
  const getCurrentUser = useCallback(async (): Promise<CurrentUser | null> => {
    try {
      const res = await api.get<{ data: CurrentUser }>('/auth/me')
      setCurrentUser(res.data.data)
      return res.data.data
    } catch {
      setCurrentUser(null)
      return null
    }
  }, [])

  const refreshAuth = getCurrentUser

  const logout = useCallback(async () => {
    try {
      await api.post('/auth/logout')
    } catch {
      // Even if the request fails, drop client-side state below.
    }
    setCurrentUser(null)
  }, [])

  // On mount: the browser will have sent the auth cookie automatically if
  // one exists, so ask the backend who (if anyone) it belongs to.
  useEffect(() => {
    let active = true
        getCurrentUser().finally(() => {
      if (active) setAuthChecked(true)
    })
    return () => {
      active = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const value = useMemo(
    () => ({
      currentUser,
      isAuthenticated: !!currentUser,
      authChecked,
      setCurrentUser,
      getCurrentUser,
      refreshAuth,
      logout,
    }),
    [currentUser, authChecked, getCurrentUser, refreshAuth, logout]
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuthContext(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuthContext must be used within an AuthProvider')
  }
  return ctx
}
