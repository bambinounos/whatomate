import { api } from './api'

// Web Push subscription management. The service worker (public/sw.js) only
// handles push/notificationclick — it never intercepts fetches — so
// registering it is safe for the SPA. No-ops entirely unless the server has
// [push] enabled in config.toml AND the user granted notification permission.

const basePath = ((window as any).__BASE_PATH__ ?? '').replace(/\/$/, '')

function urlBase64ToUint8Array(base64String: string): Uint8Array {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4)
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
  const rawData = atob(base64)
  const outputArray = new Uint8Array(rawData.length)
  for (let i = 0; i < rawData.length; i++) {
    outputArray[i] = rawData.charCodeAt(i)
  }
  return outputArray
}

function pushSupported(): boolean {
  return 'serviceWorker' in navigator && 'PushManager' in window && typeof Notification !== 'undefined'
}

/**
 * Register the service worker and subscribe to Web Push (idempotent).
 * Call after login and whenever notification permission is granted.
 */
export async function initPush(): Promise<void> {
  if (!pushSupported() || Notification.permission !== 'granted') return

  try {
    const resp = await api.get('/push/vapid-key')
    const { enabled, public_key: publicKey } = resp.data.data || {}
    if (!enabled || !publicKey) return

    const reg = await navigator.serviceWorker.register(`${basePath}/sw.js`)

    let sub = await reg.pushManager.getSubscription()
    if (!sub) {
      sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(publicKey) as BufferSource
      })
    }

    // (Re-)register server-side on every init: harmless when unchanged, and it
    // reassigns the subscription if a different user logs in on this browser.
    await api.post('/push/subscribe', sub.toJSON())
  } catch (err) {
    // Push is a progressive enhancement — never break the app over it.
    console.warn('Web push setup failed:', err)
  }
}

/**
 * Unsubscribe this browser from Web Push (call before logout while the
 * session cookie is still valid).
 */
export async function teardownPush(): Promise<void> {
  if (!pushSupported()) return

  try {
    const reg = await navigator.serviceWorker.getRegistration(`${basePath}/sw.js`)
    const sub = await reg?.pushManager.getSubscription()
    if (!sub) return

    await api.delete('/push/subscribe', { data: { endpoint: sub.endpoint } }).catch(() => {})
    await sub.unsubscribe()
  } catch (err) {
    console.warn('Web push teardown failed:', err)
  }
}
