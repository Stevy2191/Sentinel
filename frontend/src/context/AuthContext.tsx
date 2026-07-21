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

export const TOKEN_KEY = 'sentinel:token'

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
  token: string | null
  currentUser: CurrentUser | null
  isAuthenticated: boolean
  setToken: (token: string | null) => void
  setCurrentUser: (user: CurrentUser | null) => void
  getCurrentUser: () => Promise<CurrentUser | null>
  logout: () => void
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setTokenState] = useState<string | null>(() => localStorage.getItem(TOKEN_KEY))
  const [currentUser, setCurrentUser] = useState<CurrentUser | null>(null)

  const setToken = useCallback((next: string | null) => {
    setTokenState(next)
    if (next) {
      localStorage.setItem(TOKEN_KEY, next)
    } else {
      localStorage.removeItem(TOKEN_KEY)
    }
  }, [])

  const logout = useCallback(() => {
    setToken(null)
    setCurrentUser(null)
  }, [setToken])

  // Fetch (and refresh) the current user from /auth/me.
  const getCurrentUser = useCallback(async (): Promise<CurrentUser | null> => {
    try {
      const res = await api.get<{ data: CurrentUser }>('/auth/me')
      setCurrentUser(res.data.data)
      return res.data.data
    } catch {
      return null
    }
  }, [])

  // Load the current user whenever we have a token. If it's invalid, clear it.
  useEffect(() => {
    if (!token) {
      setCurrentUser(null)
      return
    }
    let active = true
    api
      .get<{ data: CurrentUser }>('/auth/me')
      .then((res) => active && setCurrentUser(res.data.data))
      .catch(() => {
        if (active) {
          setTokenState(null)
          localStorage.removeItem(TOKEN_KEY)
          setCurrentUser(null)
        }
      })
    return () => {
      active = false
    }
  }, [token])

  const value = useMemo(
    () => ({
      token,
      currentUser,
      isAuthenticated: !!token,
      setToken,
      setCurrentUser,
      getCurrentUser,
      logout,
    }),
    [token, currentUser, setToken, getCurrentUser, logout]
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
