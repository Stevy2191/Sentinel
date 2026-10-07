// Sentinel no longer uses a service worker; this one only removes itself.
//
// The old worker answered a failed page load with its cached copy of the
// app. That hid real failures: when the self-signed certificate rotates
// (every 8 hours), the browser refuses the new one until the user accepts it
// again, but the worker served the cached login page instead of letting the
// browser show its certificate warning, so sign-in failed with "Network
// error". Sentinel is a live monitoring app with nothing useful to show
// offline, so the worker is gone.
//
// Browsers that already registered the old worker fetch this file on their
// next visit, see it changed, and install it: it deletes every cache it
// made, unregisters itself, and reloads open tabs so they run without it.
// Nothing registers a worker any more (see src/index.tsx), so new visitors
// never get one.

self.addEventListener('install', () => {
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      const keys = await caches.keys()
      await Promise.all(keys.map((k) => caches.delete(k)))
      await self.registration.unregister()
      const tabs = await self.clients.matchAll({ type: 'window' })
      for (const tab of tabs) tab.navigate(tab.url)
    })()
  )
})
