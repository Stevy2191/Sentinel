import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ThemeProvider } from '@/context/ThemeContext'
import { AuthProvider } from '@/context/AuthContext'
import RequireAuth from '@/components/RequireAuth'
import Layout from '@/components/Layout'
import Dashboard from '@/pages/Dashboard'
import Monitors from '@/pages/Monitors'
import MonitorDetail from '@/pages/MonitorDetail'
import Reports from '@/pages/Reports'
import StatusPages from '@/pages/StatusPages'
import Notifications from '@/pages/Notifications'
import Settings from '@/pages/Settings'
import PublicStatus from '@/pages/PublicStatus'
import Auth from '@/pages/Auth'

export default function App() {
  return (
    <ThemeProvider>
      <AuthProvider>
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
            </Route>
          </Routes>
        </BrowserRouter>
      </AuthProvider>
    </ThemeProvider>
  )
}
