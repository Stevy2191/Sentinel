import { useEffect, useState } from 'react'
import api from '@/services/api'

/**
 * The version of the Sentinel server actually running, not a build-time
 * constant baked into the frontend bundle — the two can drift the moment the
 * backend is redeployed without a matching frontend rebuild.
 */
export function useSystemVersion() {
  const [version, setVersion] = useState<string | null>(null)

  useEffect(() => {
    let active = true
    api
      .get<{ data: { version: string } }>('/system/version')
      .then((res) => active && setVersion(res.data.data.version))
      .catch(() => active && setVersion(null))
    return () => {
      active = false
    }
  }, [])

  return version
}
