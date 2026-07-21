// Sentinel service worker.
//
// Deliberately conservative for a live-monitoring app:
//  - App shell + hashed static assets are cached so the UI loads offline/fast.
//  - API and public-status requests are NEVER cached — serving stale uptime or
//    status data as if it were live would be misleading, so those pass straight
//    through (and fail offline, which is the correct behavior).
//  - Navigations are network-first so a new deploy is picked up immediately,
//    falling back to the cached shell only when offline.

const CACHE = 'sentinel-v1'
const APP_SHELL = ['/', '/index.html', '/manifest.json', '/favicon.svg', '/icon.svg']

self.addEventListener('install', (event) => {
  self.skipWaiting()
  event.waitUntil(caches.open(CACHE).then((cache) => cache.addAll(APP_SHELL).catch(() => {})))
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', (event) => {
  const req = event.request
  if (req.method !== 'GET') return

  const url = new URL(req.url)

  // Never cache live data.
  if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/public/')) return

  // Navigations: network-first, cached shell as offline fallback.
  if (req.mode === 'navigate') {
    event.respondWith(
      fetch(req).catch(() => caches.match('/index.html').then((r) => r || caches.match('/')))
    )
    return
  }

  // Static assets (Vite hashes filenames → safe to cache-first).
  event.respondWith(
    caches.match(req).then((cached) => {
      if (cached) return cached
      return fetch(req)
        .then((res) => {
          if (res && res.status === 200 && res.type === 'basic') {
            const clone = res.clone()
            caches.open(CACHE).then((cache) => cache.put(req, clone))
          }
          return res
        })
        .catch(() => cached)
    })
  )
})
