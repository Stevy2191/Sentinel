import { Suspense, lazy } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { ThemeProvider } from '@/context/ThemeContext'
import { AuthProvider } from '@/context/AuthContext'
import { AppConfigProvider } from '@/context/AppConfigContext'
import RequireAuth from '@/components/RequireAuth'
import Layout from '@/components/Layout'
import LegacyRedirect from '@/components/LegacyRedirect'
import MonitoringRedirect from '@/components/MonitoringRedirect'
import Auth from '@/pages/Auth'

// Route components load on demand. The app shipped as one ~950 KB chunk, so a
// visitor downloaded every page - the report builder, the wizard, admin users -
// before the login form could render. Splitting per route means a page's code
// arrives when it is first visited.
//
// Auth, Layout and RequireAuth stay eager: they are on the first-paint path for
// every visit, so deferring them would only add a spinner before the login form.
const Overview = lazy(() => import('@/pages/Overview'))
const Dashboards = lazy(() => import('@/pages/dashboards/Dashboards'))
const DashboardPage = lazy(() => import('@/pages/dashboards/DashboardPage'))
const Monitoring = lazy(() => import('@/pages/Monitoring'))
const Incidents = lazy(() => import('@/pages/Incidents'))
const IncidentDetail = lazy(() => import('@/pages/IncidentDetail'))
const MonitorDetail = lazy(() => import('@/pages/MonitorDetail'))
const MonitorWizard = lazy(() => import('@/pages/MonitorWizard'))
const BulkUpload = lazy(() => import('@/pages/BulkUpload'))
const NetworkDiscovery = lazy(() => import('@/pages/NetworkDiscovery'))
const Reports = lazy(() => import('@/pages/Reports'))
const SSL = lazy(() => import('@/pages/SSL'))
const ServerDetail = lazy(() => import('@/pages/ServerDetail'))
const Sites = lazy(() => import('@/pages/network/Sites'))
const SiteDetail = lazy(() => import('@/pages/network/SiteDetail'))
const DeviceDetail = lazy(() => import('@/pages/network/DeviceDetail'))
const PortDetail = lazy(() => import('@/pages/network/PortDetail'))
const Credentials = lazy(() => import('@/pages/network/Credentials'))
const NetworkSettings = lazy(() => import('@/pages/network/NetworkSettings'))
const MibLibrary = lazy(() => import('@/pages/network/MibLibrary'))
const MibBrowser = lazy(() => import('@/pages/network/MibBrowser'))
const Profiles = lazy(() => import('@/pages/network/Profiles'))
const ProfileDetail = lazy(() => import('@/pages/network/ProfileDetail'))
const NetworkTools = lazy(() => import('@/pages/NetworkTools'))
const ToolRunDetail = lazy(() => import('@/pages/ToolRunDetail'))
const SavedReports = lazy(() => import('@/pages/SavedReports'))
const SavedReportDetail = lazy(() => import('@/pages/SavedReportDetail'))
const PublicDashboardPage = lazy(() => import('@/pages/dashboards/PublicDashboard'))
const PublicReport = lazy(() => import('@/pages/PublicReport'))
const StatusPages = lazy(() => import('@/pages/StatusPages'))
const Notifications = lazy(() => import('@/pages/Notifications'))
const SettingsLayout = lazy(() => import('@/pages/settings/SettingsLayout'))
const SettingsIndex = lazy(() => import('@/pages/settings/SettingsIndex'))
const GeneralSettings = lazy(() => import('@/pages/settings/GeneralSettings'))
const BackupsSettings = lazy(() => import('@/pages/settings/BackupsSettings'))
const AboutSettings = lazy(() => import('@/pages/settings/AboutSettings'))
const NetworkToolsSection = lazy(() => import('@/pages/settings/NetworkToolsSection'))
const NotificationSettings = lazy(() => import('@/pages/NotificationSettings'))
const Profile = lazy(() => import('@/pages/Profile'))
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

export default function App() {
  return (
    <ThemeProvider>
      <AppConfigProvider>
        <AuthProvider>
          <BrowserRouter>
          <Suspense fallback={<RouteFallback />}>
            <Routes>
            {/* Public routes. */}
            <Route path="/login" element={<Auth mode="login" />} />
            <Route path="/register" element={<Auth mode="register" />} />
            <Route path="/invitation/:token" element={<InvitationAccept />} />
            <Route path="/public/status/:slug" element={<PublicStatus />} />
            <Route path="/public/dashboards/:token" element={<PublicDashboardPage />} />
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
              <Route path="/" element={<Overview />} />
              <Route path="/dashboards" element={<Dashboards />} />
              <Route path="/dashboards/:id" element={<DashboardPage mode="view" />} />
              <Route path="/dashboards/:id/edit" element={<DashboardPage mode="edit" />} />
              {/* Kept: bookmarks and in-app links still point at /dashboard. */}
              <Route path="/dashboard" element={<Navigate to="/" replace />} />
              <Route path="/uptime" element={<MonitoringRedirect show="uptime" />} />
              <Route path="/monitoring" element={<Monitoring />} />
              <Route path="/incidents" element={<Incidents />} />
                <Route path="/incidents/:id" element={<IncidentDetail />} />
              <Route path="/monitors" element={<MonitoringRedirect show="uptime" />} />
              <Route path="/ssl" element={<SSL />} />
              <Route path="/servers" element={<MonitoringRedirect show="servers" />} />
              <Route path="/servers/:agentID" element={<ServerDetail />} />
              {/* The old list addresses open the Monitoring page; they are kept so existing
                  links and bookmarks still land somewhere useful. */}
              <Route path="/server-monitoring" element={<MonitoringRedirect show="servers" />} />
              {/* Network Monitoring. /network has no page of its own until a
                  network overview exists, so it opens the site list. */}
              <Route path="/network" element={<Navigate to="/network/sites" replace />} />
              <Route path="/network/sites" element={<Sites />} />
              <Route path="/network/sites/:id" element={<SiteDetail />} />
              <Route path="/network/devices" element={<MonitoringRedirect show="devices" />} />
              <Route path="/network/devices/:id" element={<DeviceDetail />} />
              <Route path="/network/devices/:id/ports/:ifIndex" element={<PortDetail />} />
              {/* Network's setup pages moved into Settings; the old addresses
                  redirect so bookmarks and links still land. */}
              <Route path="/network/credentials" element={<LegacyRedirect from="/network/credentials" to="/settings/network/credentials" />} />
              <Route path="/network/settings" element={<LegacyRedirect from="/network/settings" to="/settings/network/polling" />} />
              <Route path="/network/mibs" element={<LegacyRedirect from="/network/mibs" to="/settings/network/mibs" />} />
              <Route path="/network/mibs/browse" element={<MibBrowser />} />
              <Route path="/network/profiles/*" element={<LegacyRedirect from="/network/profiles" to="/settings/network/profiles" />} />
              {/* Network tools. The page itself tells anyone without the
                  grant that it is not for them; the API refuses them too. */}
              <Route path="/tools" element={<NetworkTools />} />
              <Route path="/tools/runs/:id" element={<ToolRunDetail />} />
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
              <Route path="/notifications" element={<LegacyRedirect from="/notifications" to="/settings/notifications/history" />} />
              {/* One Settings area: the layout holds the section menu, each
                  section is a child route at its own address. */}
              <Route path="/settings" element={<SettingsLayout />}>
                <Route index element={<SettingsIndex />} />
                <Route path="general" element={<GeneralSettings />} />
                <Route path="notifications" element={<NotificationSettings />} />
                <Route path="notifications/history" element={<Notifications />} />
                <Route path="users" element={<AdminUsers />} />
                <Route path="network/credentials" element={<Credentials />} />
                <Route path="network/profiles" element={<Profiles />} />
                <Route path="network/profiles/:id" element={<ProfileDetail />} />
                <Route path="network/mibs" element={<MibLibrary />} />
                <Route path="network/polling" element={<NetworkSettings />} />
                <Route path="backups" element={<BackupsSettings />} />
                <Route path="network-tools" element={<NetworkToolsSection />} />
                <Route path="about" element={<AboutSettings />} />
                {/* The profile page used to be Settings → Security. */}
                <Route path="security" element={<Navigate to="/profile" replace />} />
                <Route path="*" element={<SettingsIndex />} />
              </Route>
              <Route path="/profile" element={<Profile />} />
              {/* Users moved into Settings → Users & access. */}
              <Route path="/admin/users" element={<LegacyRedirect from="/admin/users" to="/settings/users" />} />
            </Route>
            </Routes>
          </Suspense>
          </BrowserRouter>
        </AuthProvider>
      </AppConfigProvider>
    </ThemeProvider>
  )
}
