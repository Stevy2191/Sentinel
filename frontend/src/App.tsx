import { Suspense, lazy, useEffect } from 'react'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ThemeProvider } from '@/context/ThemeContext'
import { AuthProvider, useAuthContext } from '@/context/AuthContext'
import { applyThemeColors } from '@/utils/themeUtils'
import { PREF, setString } from '@/utils/preferences'
import RequireAuth from '@/components/RequireAuth'
import Layout from '@/components/Layout'
import Auth from '@/pages/Auth'

// Route components load on demand. The app shipped as one ~950 KB chunk, so a
// visitor downloaded every page - the report builder, the wizard, admin users -
// before the login form could render. Splitting per route means a page's code
// arrives when it is first visited.
//
// Auth, Layout and RequireAuth stay eager: they are on the first-paint path for
// every visit, so deferring them would only add a spinner before the login form.
const Dashboard = lazy(() => import('@/pages/Dashboard'))
const Monitors = lazy(() => import('@/pages/Monitors'))
const MonitorDetail = lazy(() => import('@/pages/MonitorDetail'))
const MonitorWizard = lazy(() => import('@/pages/MonitorWizard'))
const BulkUpload = lazy(() => import('@/pages/BulkUpload'))
const NetworkDiscovery = lazy(() => import('@/pages/NetworkDiscovery'))
const Reports = lazy(() => import('@/pages/Reports'))
const SSL = lazy(() => import('@/pages/SSL'))
const ServerMonitoring = lazy(() => import('@/pages/ServerMonitoring'))
const SavedReports = lazy(() => import('@/pages/SavedReports'))
const SavedReportDetail = lazy(() => import('@/pages/SavedReportDetail'))
const PublicReport = lazy(() => import('@/pages/PublicReport'))
const StatusPages = lazy(() => import('@/pages/StatusPages'))
const Notifications = lazy(() => import('@/pages/Notifications'))
const Settings = lazy(() => import('@/pages/Settings'))
const SecuritySettings = lazy(() => import('@/pages/SecuritySettings'))
const AdminUsers = lazy(() => import('@/pages/AdminUsers'))
const PublicStatus = lazy(() => import('@/pages/PublicStatus'))
const InvitationAccept = lazy(() => import('@/pages/InvitationAccept'))

/** Shown while a route's chunk is fetched. */
function RouteFallback() {
  return (
    <div className="flex min-h-64 items-center justify-center p-8 text-sm text-slate-400">
      Loading\u2026
    </div>
  )
}

// ThemeSync applies the signed-in user's saved brand colours whenever they load
// or change, so they follow the user across devices. Light/dark is not part of
// this any more: the app is dark-only.
function ThemeSync() {
  const { currentUser } = useAuthContext()
  const theme = currentUser?.theme
  const primary = theme?.primary_color
  const accent = theme?.accent_color

  useEffect(() => {
    if (!primary || !accent) return
    applyThemeColors(primary, accent)
    setString(PREF.primaryColor, primary)
    setString(PREF.accentColor, accent)
  }, [primary, accent])

  return null
}

export default function App() {
  return (
    <ThemeProvider>
      <AuthProvider>
        <ThemeSync />
        <BrowserRouter>
          <Suspense fallback={<RouteFallback />}>
            <Routes>
            {/* Public routes. */}
            <Route path="/login" element={<Auth mode="login" />} />
            <Route path="/register" element={<Auth mode="register" />} />
            <Route path="/invitation/:token" element={<InvitationAccept />} />
            <Route path="/public/status/:slug" element={<PublicStatus />} />
            {/* Shared reports are reachable by token without signing in. */}
            <Route path="/reports/share/:token" element={<PublicReport />} />

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
              <Route path="/ssl" element={<SSL />} />
              <Route path="/server-monitoring" element={<ServerMonitoring />} />
              <Route path="/monitors/create" element={<MonitorDetail mode="create" />} />
              <Route path="/monitors/new/wizard" element={<MonitorWizard />} />
              <Route path="/monitors/bulk" element={<BulkUpload />} />
              <Route path="/monitors/discover" element={<NetworkDiscovery />} />
              <Route path="/monitors/:id" element={<MonitorDetail mode="view" />} />
              <Route path="/monitors/:id/edit" element={<MonitorDetail mode="edit" />} />
              <Route path="/reports" element={<SavedReports mode="list" />} />
              <Route path="/reports/new" element={<SavedReports mode="create" />} />
              {/* The pre-existing live analytics view, moved off /reports so the
                  saved-report hub can own that path. Declared before the ":id"
                  route for clarity; React Router ranks the static segment higher
                  either way. */}
              <Route path="/reports/analytics" element={<Reports />} />
              <Route path="/reports/:id" element={<SavedReportDetail />} />
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
          </Suspense>
        </BrowserRouter>
      </AuthProvider>
    </ThemeProvider>
  )
}
