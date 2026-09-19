import { useEffect, useState } from 'react'
import api from '@/services/api'

/** The Sentinel server's own resource usage. */
export interface SystemResources {
  /** null until two CPU samples exist — utilisation is a rate, not a reading. */
  cpu_percent: number | null
  memory_percent: number
  memory_used_mb: number
  memory_total_mb: number
  disk_percent: number
  disk_used_gb: number
  disk_total_gb: number
}

/**
 * Polls the machine Sentinel itself runs on.
 *
 * Distinct from agent metrics, which describe the hosts being watched. This
 * needs no agent and works on a fresh install, because the server is reading
 * its own /proc rather than waiting for something to report in.
 */
export function useSystemResources(pollMs = 15000) {
  const [resources, setResources] = useState<SystemResources | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let active = true
    const load = () =>
      api
        .get<{ data: SystemResources }>('/system/resources')
        .then((res) => active && setResources(res.data.data))
        .catch(() => active && setResources(null))
        .finally(() => active && setLoading(false))

    void load()
    if (pollMs <= 0)
      return () => {
        active = false
      }
    const t = window.setInterval(() => void load(), pollMs)
    return () => {
      active = false
      window.clearInterval(t)
    }
  }, [pollMs])

  return { resources, loading }
}
