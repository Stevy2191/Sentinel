import { useCallback, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse, DiscoveryResult } from '@/types'

export interface ScanSubnetOptions {
  /** Per-host timeout override, in milliseconds. Omit to use the backend default. */
  timeoutMs?: number
}

/** useScanSubnet returns a scan() action that sweeps a CIDR for live hosts. */
export function useScanSubnet() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)
  const [result, setResult] = useState<DiscoveryResult | null>(null)

  const scan = useCallback(async (cidr: string, options?: ScanSubnetOptions) => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.post<ApiResponse<DiscoveryResult>>('/discovery/scan', {
        cidr,
        timeout_ms: options?.timeoutMs ?? 0,
      })
      setResult(data.data)
      return data.data
    } catch (err) {
      setError(err as ApiError)
      throw err
    } finally {
      setLoading(false)
    }
  }, [])

  return { scan, result, loading, error }
}
