import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ThemeProvider } from '@/context/ThemeContext'
import Layout from '@/components/Layout'
import Dashboard from '@/pages/Dashboard'
import Monitors from '@/pages/Monitors'
import MonitorDetail from '@/pages/MonitorDetail'
import Reports from '@/pages/Reports'
import StatusPages from '@/pages/StatusPages'
import Notifications from '@/pages/Notifications'
import Settings from '@/pages/Settings'
import PublicStatus from '@/pages/PublicStatus'

export default function App() {
  return (
    <ThemeProvider>
      <BrowserRouter>
        <Routes>
          {/* Public status page — standalone, no admin layout. */}
          <Route path="/public/status/:slug" element={<PublicStatus />} />

          {/* Admin app. */}
          <Route element={<Layout />}>
            <Route path="/" element={<Dashboard />} />
            <Route path="/monitors" element={<Monitors />} />
            <Route path="/monitors/create" element={<MonitorDetail mode="create" />} />
            <Route path="/monitors/:id" element={<MonitorDetail mode="view" />} />
            <Route path="/monitors/:id/edit" element={<MonitorDetail mode="edit" />} />
            <Route path="/reports" element={<Reports />} />
            <Route path="/status-pages" element={<StatusPages />} />
            <Route path="/notifications" element={<Notifications />} />
            <Route path="/settings" element={<Settings />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </ThemeProvider>
  )
}
