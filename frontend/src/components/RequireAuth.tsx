import { Navigate, useLocation } from 'react-router-dom'
import type { ReactNode } from 'react'
import { useAuthContext } from '@/context/AuthContext'

/** RequireAuth redirects to /login when there is no signed-in user, carrying
 *  the page that was asked for so sign-in can return to it (an alert email's
 *  link, a page whose session just expired). */
export default function RequireAuth({ children }: { children: ReactNode }) {
  const { isAuthenticated, authChecked } = useAuthContext()
  const location = useLocation()
  if (!authChecked) {
    // Still waiting on the initial /auth/me probe - render nothing rather
    // than redirect, so a logged-in user with a valid cookie isn't briefly
    // bounced to /login on page load/refresh.
    return null
  }
  if (!isAuthenticated) {
    return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />
  }
  return <>{children}</>
}
