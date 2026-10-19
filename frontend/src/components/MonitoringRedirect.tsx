import { Navigate, useLocation } from 'react-router-dom'
import { legacyMonitoringTarget, type KindParent } from '@/utils/monitoringList'

/** MonitoringRedirect sends an old list address (/uptime, /servers,
 *  /network/devices) to the Monitoring list on that kind, keeping its
 *  filters and hash, so old bookmarks and links keep working. */
export default function MonitoringRedirect({ show }: { show: KindParent }) {
  const location = useLocation()
  return <Navigate to={legacyMonitoringTarget(location, show)} replace />
}
