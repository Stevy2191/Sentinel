import { Navigate, useSearchParams } from 'react-router-dom'
import { useAuthContext } from '@/context/AuthContext'
import { legacySettingsTarget } from '@/utils/settingsNav'

/** SettingsIndex answers /settings and any unknown /settings/* address: the
 *  old ?tab= value if there is one, otherwise the user's settings home. */
export default function SettingsIndex() {
  const { currentUser } = useAuthContext()
  const [searchParams] = useSearchParams()
  return <Navigate to={legacySettingsTarget(searchParams.get('tab'), currentUser?.is_admin ?? false)} replace />
}
