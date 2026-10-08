import { Navigate, useLocation } from 'react-router-dom'
import { legacyMonitoringTarget, type Section } from '@/utils/monitoringView'

/** MonitoringRedirect sends an old list address (/uptime, /servers,
 *  /network/devices) to the Monitoring page on that section, keeping its
 *  query and hash, so old bookmarks and links keep working. */
export default function MonitoringRedirect({ show }: { show: Section }) {
  const location = useLocation()
  return <Navigate to={legacyMonitoringTarget(location, show)} replace />
}
