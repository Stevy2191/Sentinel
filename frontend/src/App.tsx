import { useEffect } from 'react'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ThemeProvider, useTheme, type ThemeMode } from '@/context/ThemeContext'
import { AuthProvider, useAuthContext } from '@/context/AuthContext'
import { applyThemeColors } from '@/utils/themeUtils'
import { PREF, setString } from '@/utils/preferences'
import RequireAuth from '@/components/RequireAuth'
import Layout from '@/components/Layout'
import Dashboard from '@/pages/Dashboard'
import Monitors from '@/pages/Monitors'
import MonitorDetail from '@/pages/MonitorDetail'
import Reports from '@/pages/Reports'
import StatusPages from '@/pages/StatusPages'
import Notifications from '@/pages/Notifications'
import Settings from '@/pages/Settings'
import SecuritySettings from '@/pages/SecuritySettings'
import AdminUsers from '@/pages/AdminUsers'
import PublicStatus from '@/pages/PublicStatus'
import Auth from '@/pages/Auth'

// ThemeSync applies the signed-in user's saved theme (colors + mode) whenever it
// loads or changes — this is what makes the theme follow the user across devices.
function ThemeSync() {
  const { currentUser } = useAuthContext()
  const { setMode } = useTheme()
  const theme = currentUser?.theme
  const primary = theme?.primary_color
  const accent = theme?.accent_color
  const mode = theme?.mode

  useEffect(() => {
    if (!primary || !accent) return
    applyThemeColors(primary, accent)
    setString(PREF.primaryColor, primary)
    setString(PREF.accentColor, accent)
    if (mode === 'light' || mode === 'dark' || mode === 'auto') setMode(mode as ThemeMode)
  }, [primary, accent, mode, setMode])

  return null
}

export default function App() {
  return (
    <ThemeProvider>
      <AuthProvider>
        <ThemeSync />
        <BrowserRouter>
          <Routes>
            {/* Public routes. */}
            <Route path="/login" element={<Auth mode="login" />} />
            <Route path="/register" element={<Auth mode="register" />} />
            <Route path="/public/status/:slug" element={<PublicStatus />} />

            {/* Admin app — requires authentication. */}
            <Route
              element={
                <RequireAuth>
                  <Layout />
                </RequireAuth>
              }
            >
              <Route path="/" element={<Dashboard />} />
              <Route path="/dashboard" element={<Dashboard />} />
              <Route path="/monitors" element={<Monitors />} />
              <Route path="/monitors/create" element={<MonitorDetail mode="create" />} />
              <Route path="/monitors/:id" element={<MonitorDetail mode="view" />} />
              <Route path="/monitors/:id/edit" element={<MonitorDetail mode="edit" />} />
              <Route path="/reports" element={<Reports />} />
              <Route path="/status-pages" element={<StatusPages mode="list" />} />
              <Route path="/status-pages/create" element={<StatusPages mode="create" />} />
              <Route path="/status-pages/:slug/detail" element={<StatusPages mode="detail" />} />
              <Route path="/status-pages/:slug/edit" element={<StatusPages mode="edit" />} />
              <Route path="/notifications" element={<Notifications />} />
              <Route path="/settings" element={<Settings />} />
              <Route path="/settings/security" element={<SecuritySettings />} />
              {/* Admin-only page; AdminUsers itself redirects non-admins to /dashboard. */}
              <Route path="/admin/users" element={<AdminUsers />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </AuthProvider>
    </ThemeProvider>
  )
}
