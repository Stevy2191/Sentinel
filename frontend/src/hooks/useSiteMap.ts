import { useCallback, useEffect, useRef, useState } from 'react'
import { api, apiBaseURL, type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type { DevicePorts } from '@/hooks/usePorts'
import type { MapSnapshot, MapTraffic } from '@/types/siteMap'

export type LiveState = { kind: 'connecting' } | { kind: 'live' } | { kind: 'reconnecting'; since: number } | { kind: 'stopped'; error: string }

export interface MapEndInput {
  device_id: string
  if_index?: number | null
}

function parse<T>(e: MessageEvent): T | null {
  try {
    return JSON.parse(e.data) as T
  } catch {
    return null
  }
}

const enc = encodeURIComponent

/** A closed stream is retried after this long, doubling up to the max. */
const RETRY_MIN_MS = 2000
const RETRY_MAX_MS = 60_000

/**
 * useSiteMap follows a site's map live: the whole map on connect, after each
 * edit and every few minutes, and the links' traffic about every 10 s, over
 * SSE (GET /network/sites/:id/map/stream, or the public link's stream).
 * EventSource reconnects by itself; when it gives up (the server was away,
 * the sign-in expired) this tries again with a growing pause, until the map
 * answers 404. Frames from a closed source are ignored, so a late frame
 * can't land on another site.
 */
export function useSiteMap(siteId: string | undefined, opts?: { publicToken?: string }) {
  const token = opts?.publicToken
  const [map, setMap] = useState<MapSnapshot | null>(null)
  const [traffic, setTraffic] = useState<MapTraffic | null>(null)
  const [live, setLive] = useState<LiveState>({ kind: 'connecting' })
  const [error, setError] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)
  const backoff = useRef(RETRY_MIN_MS)
  const shown = useRef<string | null>(null)

  useEffect(() => {
    const key = `${siteId ?? ''}|${token ?? ''}`
    if (shown.current !== key) {
      // A different map: start clean. A reconnect keeps the last one shown.
      shown.current = key
      setMap(null)
      setTraffic(null)
      setError(null)
      setLive({ kind: 'connecting' })
      backoff.current = RETRY_MIN_MS
    }
    if (!siteId && !token) return
    let open = true
    let disposed = false
    let retry = 0
    const url = token ? `${apiBaseURL}/public/maps/${enc(token)}/stream` : `${apiBaseURL}/network/sites/${enc(siteId!)}/map/stream`
    const probe = token ? `/public/maps/${enc(token)}` : `/network/sites/${enc(siteId!)}/map`
    const src = new EventSource(url, { withCredentials: !token })
    src.addEventListener('map', (e: MessageEvent) => {
      if (!open) return
      const m = parse<MapSnapshot>(e)
      if (m) {
        setMap(m)
        setError(null)
        setLive({ kind: 'live' })
        backoff.current = RETRY_MIN_MS
      }
    })
    src.addEventListener('traffic', (e: MessageEvent) => {
      if (!open) return
      const t = parse<MapTraffic>(e)
      if (t) {
        setTraffic(t)
        setLive({ kind: 'live' })
      }
    })
    src.onerror = () => {
      if (!open) return
      setLive((cur) => (cur.kind === 'reconnecting' ? cur : { kind: 'reconnecting', since: Date.now() }))
      if (src.readyState !== EventSource.CLOSED) return // the browser retries by itself
      // The browser gave up (a 502 while the server restarts, a 401 after
      // the sign-in expired, or a 404). Ask once why: a 404 means the map
      // or the link is gone for good; anything else, try again later, so a
      // wall display recovers by itself.
      open = false
      src.close()
      void api
        .get(probe)
        .then(() => true)
        .catch((err: ApiError) => err.status !== 404)
        .then((again) => {
          if (disposed || shown.current !== key) return
          if (!again) {
            setLive({ kind: 'stopped', error: 'Live updates stopped' })
            setError('This map is not available')
            return
          }
          const wait = backoff.current
          backoff.current = Math.min(RETRY_MAX_MS, wait * 2)
          retry = window.setTimeout(() => setAttempt((n) => n + 1), wait)
        })
    }
    return () => {
      open = false
      disposed = true
      window.clearTimeout(retry)
      src.close()
    }
  }, [siteId, token, attempt])

  const reload = useCallback(() => setAttempt((n) => n + 1), [])
  return { map, traffic, live, error, reload }
}

function apiError(err: unknown, fallback: string): Error {
  return new Error((err as ApiError).message || fallback)
}

/** The map's edits. Nothing is refetched: the stream brings the new map. */
export function useSiteMapActions(siteId: string) {
  const [busy, setBusy] = useState(false)
  const base = `/network/sites/${enc(siteId)}/map`
  const run = useCallback(async (f: () => Promise<unknown>, fallback: string) => {
    setBusy(true)
    try {
      await f()
    } catch (err) {
      throw apiError(err, fallback)
    } finally {
      setBusy(false)
    }
  }, [])
  return {
    busy,
    setPositions: (positions: { device_id: string; x: number; y: number }[]) =>
      run(() => api.put(`${base}/positions`, { positions }), 'Could not save the position'),
    setDevice: (id: string, p: { hidden?: boolean; added?: boolean }) =>
      run(() => api.put(`${base}/devices/${enc(id)}`, p), 'Could not change the device'),
    addLink: (a: MapEndInput, b: MapEndInput) => run(() => api.post(`${base}/links`, { a, b }), 'Could not add the link'),
    updateLink: (id: string, a: MapEndInput, b: MapEndInput) =>
      run(() => api.put(`${base}/links/${enc(id)}`, { a, b }), 'Could not change the link'),
    deleteLink: (id: string) => run(() => api.delete(`${base}/links/${enc(id)}`), 'Could not delete the link'),
    setLinkHidden: (key: string, hidden: boolean) =>
      run(() => api.put(`${base}/link-state`, { key, hidden }), hidden ? 'Could not hide the link' : 'Could not show the link'),
    keep: (key: string) => run(() => api.post(`${base}/link-state/keep`, { key }), 'Could not keep the link'),
    reset: () => run(() => api.post(`${base}/reset`), 'Could not reset the layout'),
  }
}

/** The map's public link (admins). */
export function useMapPublicLink(siteId: string) {
  const [link, setLink] = useState<{ token: string } | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const base = `/network/sites/${enc(siteId)}/map/public-link`
  useEffect(() => {
    let live = true
    setLoading(true)
    api
      .get<ApiResponse<{ token: string } | null>>(base)
      .then((r) => live && setLink(r.data.data ?? null))
      .catch((err) => live && setError((err as ApiError).message || 'Could not load the public link'))
      .finally(() => live && setLoading(false))
    return () => {
      live = false
    }
  }, [base])
  return {
    link,
    loading,
    error,
    create: async () => {
      try {
        const r = await api.post<ApiResponse<{ token: string }>>(base)
        setLink(r.data.data)
      } catch (err) {
        throw apiError(err, 'Could not create the public link')
      }
    },
    revoke: async () => {
      try {
        await api.delete(base)
        setLink(null)
      } catch (err) {
        throw apiError(err, 'Could not turn the public link off')
      }
    },
  }
}

const PORTS_MS = 60_000

/** One map device's ports, for its realistic drawing: the device page's own
 *  data, or the public map's trimmed copy, every minute; nothing while
 *  enabled is false (the simple view). */
export function useMapDevicePorts(deviceId: string, publicToken?: string, enabled = true): DevicePorts | null {
  const [data, setData] = useState<DevicePorts | null>(null)
  const url = publicToken ? `/public/maps/${enc(publicToken)}/devices/${enc(deviceId)}/ports` : `/devices/${enc(deviceId)}/ports`
  const current = useRef(url)
  current.current = url
  useEffect(() => {
    if (!enabled) {
      setData(null)
      return
    }
    let live = true
    const load = () =>
      api
        .get<ApiResponse<DevicePorts>>(url)
        .then((r) => live && current.current === url && setData(r.data.data))
        .catch(() => {
          // A drawing without its ports falls back to a card.
          if (live) setData(null)
        })
    void load()
    const t = window.setInterval(() => void load(), PORTS_MS)
    return () => {
      live = false
      window.clearInterval(t)
    }
  }, [url, enabled])
  return data
}
