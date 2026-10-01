import { useCallback, useEffect, useRef, useState } from 'react'
import type { Device } from '@/hooks/useDevices'

export type RefreshPhase = 'idle' | 'running' | 'done' | 'failed' | 'slow'

/** How long to wait for the poller before saying it hasn't got there yet. */
const PATIENCE_MS = 20_000
const CHECK_EVERY_MS = 2_000

/**
 * Follows a "Refresh now" through to its result. The refresh request only
 * queues the device: the poller picks it up within a few seconds, re-polls it
 * and re-reads its inventory. This watches the device for that poll and that
 * inventory landing, so the page can say "Refreshed" or why it didn't.
 */
export function useRefreshWatch(device: Device | null, refetch: () => Promise<void>) {
  const [state, setState] = useState<{ phase: RefreshPhase; message?: string }>({ phase: 'idle' })
  const baseline = useRef<{ polled: string | null; inventory: string | null; started: number } | null>(null)

  /** Call right after the refresh request succeeds. */
  const start = useCallback(() => {
    if (!device) return
    baseline.current = { polled: device.last_polled_at, inventory: device.last_inventory_at, started: Date.now() }
    setState({ phase: 'running' })
  }, [device])

  // While running, re-read the device every couple of seconds.
  useEffect(() => {
    if (state.phase !== 'running') return
    const t = window.setInterval(() => {
      void refetch()
      if (baseline.current && Date.now() - baseline.current.started > PATIENCE_MS) setState({ phase: 'slow' })
    }, CHECK_EVERY_MS)
    return () => window.clearInterval(t)
  }, [state.phase, refetch])

  // Decide from what the device now says.
  useEffect(() => {
    const b = baseline.current
    if (state.phase !== 'running' || !device || !b) return
    const polled = device.last_polled_at != null && device.last_polled_at !== b.polled
    if (!polled) return
    if (device.status !== 'up') {
      setState({ phase: 'failed', message: device.status_detail || `The device is ${device.status}.` })
      return
    }
    const inventoried = device.last_inventory_at != null && device.last_inventory_at !== b.inventory
    if (!inventoried) return
    if (device.status_detail.startsWith('inventory:')) {
      setState({ phase: 'failed', message: device.status_detail })
    } else {
      setState({ phase: 'done' })
    }
  }, [device, state.phase])

  // "Refreshed" fades after a few seconds.
  useEffect(() => {
    if (state.phase !== 'done') return
    const t = window.setTimeout(() => setState({ phase: 'idle' }), 6_000)
    return () => window.clearTimeout(t)
  }, [state.phase])

  return { phase: state.phase, message: state.message, start }
}
