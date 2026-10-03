import { useCallback, useEffect, useState } from 'react'

const RELOAD_EVERY_MS = 6 * 60 * 60 * 1000
const RELOAD_RETRY_MS = 60 * 1000
const HEALTH_TIMEOUT_MS = 10 * 1000

interface WakeLockLike {
  release(): Promise<void>
}
type NavigatorWithWakeLock = Navigator & { wakeLock?: { request(type: 'screen'): Promise<WakeLockLike> } }

/** What an unattended screen needs. While enabled it keeps the screen awake
 *  (where the browser supports it) and reloads the page every 6 hours, to
 *  pick up new versions of Sentinel and free memory. It reloads only once
 *  Sentinel answers its health check, so a TV never lands on the browser's
 *  error page while the server is down. */
export function useDisplayMode(enabled: boolean) {
  const [fullscreen, setFullscreen] = useState(() => document.fullscreenElement != null)

  useEffect(() => {
    const onChange = () => setFullscreen(document.fullscreenElement != null)
    document.addEventListener('fullscreenchange', onChange)
    return () => document.removeEventListener('fullscreenchange', onChange)
  }, [])

  useEffect(() => {
    if (!enabled) return
    let lock: WakeLockLike | null = null
    let stopped = false
    const request = async () => {
      try {
        const next = (await (navigator as NavigatorWithWakeLock).wakeLock?.request('screen')) ?? null
        if (stopped) {
          // Disabled or unmounted while the request was in flight.
          void next?.release().catch(() => undefined)
          return
        }
        lock = next
      } catch {
        lock = null // refused (battery saver, unsupported): the page still works
      }
    }
    void request()
    // A wake lock is dropped whenever the tab is hidden; take it again on return.
    const onVisible = () => {
      if (document.visibilityState === 'visible') void request()
    }
    document.addEventListener('visibilitychange', onVisible)
    let reload: number | undefined
    // The health check in flight and its timeout. An AbortController and a
    // timer, not AbortSignal.timeout, which older TV browsers lack.
    let check: AbortController | null = null
    let checkTimer: number | undefined
    const reloadWhenUp = async () => {
      const ac = new AbortController()
      check = ac
      checkTimer = window.setTimeout(() => ac.abort(), HEALTH_TIMEOUT_MS)
      try {
        const res = await fetch('/api/health', { cache: 'no-store', signal: ac.signal })
        if (stopped) return
        if (res.ok) {
          window.location.reload()
          return
        }
      } catch {
        // Unreachable, or no answer in time (aborted): keep showing the last
        // data and try again shortly.
      } finally {
        window.clearTimeout(checkTimer)
        check = null
      }
      if (!stopped) reload = window.setTimeout(() => void reloadWhenUp(), RELOAD_RETRY_MS)
    }
    reload = window.setTimeout(() => void reloadWhenUp(), RELOAD_EVERY_MS)
    return () => {
      stopped = true
      document.removeEventListener('visibilitychange', onVisible)
      window.clearTimeout(reload)
      window.clearTimeout(checkTimer)
      check?.abort()
      void lock?.release().catch(() => undefined)
    }
  }, [enabled])

  const enter = useCallback(async () => {
    try {
      await document.documentElement.requestFullscreen?.()
    } catch {
      // The browser refused (no user gesture, or a kiosk policy): the display
      // shell still covers the app, just not the browser's own chrome.
    }
  }, [])

  const exit = useCallback(async () => {
    try {
      if (document.fullscreenElement) await document.exitFullscreen()
    } catch {
      // Already left, or unsupported: nothing to undo.
    }
  }, [])

  return { fullscreen, enter, exit }
}
