import { Navigate } from 'react-router-dom'
import type { ReactNode } from 'react'
import { useAuthContext } from '@/context/AuthContext'

/** RequireAuth redirects to /login when there is no auth token. */
export default function RequireAuth({ children }: { children: ReactNode }) {
  const { isAuthenticated, authChecked } = useAuthContext()
  if (!authChecked) {
    // Still waiting on the initial /auth/me probe - render nothing rather
    // than redirect, so a logged-in user with a valid cookie isn't briefly
    // bounced to /login on page load/refresh.
    return null
  }
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}
