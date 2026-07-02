// Whatomate service worker — Web Push only.
// Deliberately has NO fetch handler: the SPA (and its server-injected
// <base href>/__BASE_PATH__) must keep loading straight from the network,
// so this worker never caches or intercepts requests.

self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()))

self.addEventListener('push', (event) => {
  let data = {}
  try {
    data = event.data.json()
  } catch {
    return
  }

  event.waitUntil((async () => {
    // Dedup with the in-page alerts (websocket.ts): when a window is visible
    // AND focused the page already plays a sound / shows a toast, so skip the
    // OS notification. Backgrounded tabs still fire `new Notification` from
    // the page with the same tag as below, so the browser collapses the two
    // into one.
    const wins = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    if (wins.some((w) => w.visibilityState === 'visible' && w.focused)) return

    await self.registration.showNotification(data.title || 'Whatomate', {
      body: data.body || '',
      tag: data.tag || 'whatomate',
      icon: 'icons/icon-192.png',
      badge: 'icons/badge-72.png',
      data: { url: data.url || './' }
    })
  })())
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  // Resolve the chat URL against the SW scope so base-path deployments work.
  const target = new URL(event.notification.data?.url || './', self.registration.scope).href

  event.waitUntil((async () => {
    const wins = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    for (const w of wins) {
      if (w.url.startsWith(self.registration.scope)) {
        await w.focus()
        if (w.navigate) await w.navigate(target)
        return
      }
    }
    await self.clients.openWindow(target)
  })())
})
