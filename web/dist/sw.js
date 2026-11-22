// pichat service worker (v0.5.0): shows Web Push notifications and opens the chat on tap.
// No caching: the app always loads fresh from the server.
self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()))

self.addEventListener('push', (e) => {
  let d = {}
  try { d = e.data ? e.data.json() : {} } catch { d = { title: 'pichat', body: e.data ? e.data.text() : '' } }
  const title = d.title || 'pichat'
  e.waitUntil((async () => {
    // Skip the notification when a pichat window is focused on that chat already.
    const wins = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    if (d.groupId && wins.some((w) => w.focused && w.url.includes('#/g/' + d.groupId))) return
    await self.registration.showNotification(title, {
      body: d.body || '',
      tag: d.tag || undefined,
      icon: '/static/icon-192.png',
      badge: '/static/icon-192.png',
      data: { url: d.url || '/' },
    })
  })())
})

self.addEventListener('notificationclick', (e) => {
  e.notification.close()
  const url = new URL(e.notification.data?.url || '/', self.location.origin).href
  e.waitUntil((async () => {
    const wins = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    for (const w of wins) {
      if ('focus' in w) {
        await w.focus()
        if ('navigate' in w) await w.navigate(url).catch(() => {})
        return
      }
    }
    await self.clients.openWindow(url)
  })())
})

// The browser rotated the subscription: tell the server about the new one.
self.addEventListener('pushsubscriptionchange', (e) => {
  e.waitUntil((async () => {
    const old = e.oldSubscription
    const key = old?.options?.applicationServerKey
    if (!key) return
    const sub = await self.registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key })
    await fetch('/api/v1/push/subscriptions', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(sub.toJSON()) })
    if (old) await fetch('/api/v1/push/subscriptions', { method: 'DELETE', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ endpoint: old.endpoint }) })
  })())
})
