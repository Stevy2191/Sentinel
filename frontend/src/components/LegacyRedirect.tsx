import { Navigate, useLocation } from 'react-router-dom'
import { movedPath } from '@/utils/navigation'

/** LegacyRedirect sends an address that moved (`from`) to its new home
 *  (`to`), keeping the rest of the path, the query and the hash, so old
 *  bookmarks and links keep working. */
export default function LegacyRedirect({ from, to }: { from: string; to: string }) {
  const location = useLocation()
  return <Navigate to={movedPath(location, from, to)} replace />
}
