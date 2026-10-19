import { useCallback, useEffect, useMemo } from 'react'
import { useLocation, useNavigate, useNavigationType } from 'react-router-dom'

// In-app navigation history, so a page's back link returns to the page the
// user actually came from (with that page's name), not a fixed parent: the
// Monitoring list -> a switch -> back lands on the Monitoring list, filters
// and scroll position as they were left. Each history entry (React Router's
// location.key) remembers the entry before it, the name its page gave
// itself, and where it was scrolled. Kept in sessionStorage so a reload
// keeps the trail.

interface Entry {
  prev?: string
  label?: string
  scrollY?: number
  path?: string
}

interface Trail {
  entries: Record<string, Entry>
  order: string[]
  last?: string
}

const KEY = 'sentinel:nav-trail'
const MAX = 200

function load(): Trail {
  try {
    const raw = sessionStorage.getItem(KEY)
    if (raw) return JSON.parse(raw) as Trail
  } catch {
    // private window or blocked storage: start a fresh trail
  }
  return { entries: {}, order: [] }
}

const trail: Trail = load()

function save() {
  while (trail.order.length > MAX) {
    const old = trail.order.shift()
    if (old) delete trail.entries[old]
  }
  try {
    sessionStorage.setItem(KEY, JSON.stringify(trail))
  } catch {
    // storage full or blocked: the trail still works for this page's life
  }
}

function entry(key: string): Entry {
  if (!trail.entries[key]) {
    trail.entries[key] = {}
    trail.order.push(key)
  }
  return trail.entries[key]
}

/** Records each navigation: a push remembers the page it came from, a
 *  replace keeps the replaced page's predecessor, a back/forward (pop) keeps
 *  what it had and restores that page's scroll position. Mount once inside
 *  the router. */
export function NavTracker() {
  const location = useLocation()
  const type = useNavigationType()
  // The scroll position is saved as the user scrolls: by the time a
  // navigation's effect runs, the new (often shorter) page has already
  // pulled the window back to the top. The browser's own restoring would
  // fight ours, so it is switched off.
  useEffect(() => {
    if ('scrollRestoration' in window.history) window.history.scrollRestoration = 'manual'
    let pending = 0
    const onScroll = () => {
      if (pending) return
      pending = window.setTimeout(() => {
        pending = 0
        if (trail.last) {
          entry(trail.last).scrollY = window.scrollY
          save()
        }
      }, 150)
    }
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      window.removeEventListener('scroll', onScroll)
      window.clearTimeout(pending)
    }
  }, [])
  useEffect(() => {
    const key = location.key
    const last = trail.last
    const lastPath = last ? trail.entries[last]?.path : undefined
    const e = entry(key)
    e.path = location.pathname
    if (type === 'PUSH' && last && last !== key) e.prev = last
    if (type === 'REPLACE' && last && last !== key) e.prev = trail.entries[last]?.prev
    trail.last = key
    save()
    // Opening another page starts at its top; a filter change on the same
    // page (Monitoring's search updates the address) keeps the scroll.
    if (type !== 'POP' && lastPath !== undefined && lastPath !== location.pathname) window.scrollTo(0, 0)
    if (type === 'POP' && e.scrollY != null) {
      // Lists load after the page mounts: retry until there's room to scroll.
      const y = e.scrollY
      const tries = [0, 100, 300, 700, 1500]
      const timers = tries.map((ms) =>
        window.setTimeout(() => {
          if (Math.abs(window.scrollY - y) > 2) window.scrollTo(0, y)
        }, ms)
      )
      return () => timers.forEach((t) => window.clearTimeout(t))
    }
  }, [location.key, location.pathname, type])
  return null
}

/** Names the current page for the back link of whatever page is opened from
 *  it ("Monitoring", the site's name, the device's name). */
export function usePageLabel(label: string | undefined) {
  const location = useLocation()
  useEffect(() => {
    if (!label) return
    entry(location.key).label = label
    save()
  }, [location.key, label])
}

/** Where this page's back link goes: the page the user came from, when
 *  they came from one inside the app, else the fallback (a device's site, a
 *  monitor's Monitoring list). */
export function useBack(fallback: { to: string; label: string }) {
  const location = useLocation()
  const navigate = useNavigate()
  const type = useNavigationType()
  // The page renders before NavTracker's effect records this navigation, so
  // on a fresh entry work out its predecessor the same way the tracker will:
  // a push came from the page last seen, a replace inherits that page's.
  const known = trail.entries[location.key]
  const prev =
    known?.prev ??
    (known || !trail.last || trail.last === location.key
      ? undefined
      : type === 'PUSH'
        ? trail.last
        : type === 'REPLACE'
          ? trail.entries[trail.last]?.prev
          : undefined)
  const prevEntry = prev ? trail.entries[prev] : undefined
  const back = useMemo(
    () => (prevEntry ? { label: prevEntry.label ?? 'Back', inApp: true } : { label: fallback.label, inApp: false }),
    [prevEntry, fallback.label]
  )
  const go = useCallback(() => {
    if (back.inApp) navigate(-1)
    else navigate(fallback.to)
  }, [back.inApp, navigate, fallback.to])
  return { label: back.label, go, href: back.inApp ? undefined : fallback.to }
}
