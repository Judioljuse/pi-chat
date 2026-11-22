import { useEffect, useState } from 'react'
import { api } from './api'

/** Web Push (v0.5.0). iPhone/iPad: only works in the web app added to the home screen (iOS 16.4+). */

export const isIOS = () => /iPhone|iPad|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
export const isStandalone = () =>
  matchMedia('(display-mode: standalone)').matches || (navigator as unknown as { standalone?: boolean }).standalone === true
export const pushSupported = () =>
  typeof window !== 'undefined' && window.isSecureContext && 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window

/** Registers /sw.js once; later calls get the same registration. */
let reg: Promise<ServiceWorkerRegistration> | null = null
export function swRegistration() {
  reg ??= navigator.serviceWorker.register('/sw.js', { scope: '/' })
  return reg
}

function keyBytes(b64: string) {
  const s = atob(b64.replace(/-/g, '+').replace(/_/g, '/') + '==='.slice((b64.length + 3) % 4))
  return Uint8Array.from(s, (c) => c.charCodeAt(0))
}

export type PushState = 'unsupported' | 'need-install' | 'denied' | 'off' | 'on'

export async function pushState(): Promise<PushState> {
  if (!pushSupported()) return isIOS() && !isStandalone() ? 'need-install' : 'unsupported'
  if (Notification.permission === 'denied') return 'denied'
  const r = await navigator.serviceWorker.getRegistration('/')
  const sub = await r?.pushManager.getSubscription()
  return sub ? 'on' : 'off'
}

/** Must run from a click (iOS asks for permission only on a user gesture). */
export async function enablePush(): Promise<void> {
  const p = Notification.permission === 'granted' ? 'granted' : await Notification.requestPermission()
  if (p !== 'granted') throw new Error(p === 'denied' ? '通知权限被拒绝了：请在系统设置里允许 pichat 发通知' : '没有开启通知权限')
  const { publicKey } = await api<{ publicKey: string }>('GET', '/push/key')
  const r = await swRegistration()
  await navigator.serviceWorker.ready
  let sub = await r.pushManager.getSubscription()
  // A subscription made with another server key can't receive our pushes: replace it.
  const cur = sub?.options?.applicationServerKey
  if (sub && cur && btoa(String.fromCharCode(...new Uint8Array(cur))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '') !== publicKey) {
    await sub.unsubscribe()
    sub = null
  }
  try {
    sub ??= await r.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(publicKey) })
  } catch (e) {
    // Chrome subscribes through Google's push service (FCM), which can't be reached from mainland
    // China; it then fails with AbortError "Registration failed - push service error / permission denied".
    const msg = e instanceof Error ? `${e.name} ${e.message}` : String(e)
    if (/AbortError|push service|Registration failed/i.test(msg))
      throw new Error('浏览器没能在它的推送服务上登记（常见原因：Chrome 的推送要经过 Google，国内网络连不上）。iPhone 请用添加到主屏幕的 pichat，电脑可以试 Safari 或 Edge。')
    throw e
  }
  await api('POST', '/push/subscriptions', sub.toJSON())
}

export async function disablePush(): Promise<void> {
  const r = await navigator.serviceWorker.getRegistration('/')
  const sub = await r?.pushManager.getSubscription()
  if (!sub) return
  await api('DELETE', '/push/subscriptions', { endpoint: sub.endpoint }).catch(() => {})
  await sub.unsubscribe()
}

export function usePushState() {
  const [st, setSt] = useState<PushState | null>(null)
  const refresh = () => pushState().then(setSt, () => setSt('unsupported'))
  useEffect(() => {
    refresh()
    document.addEventListener('visibilitychange', refresh)
    return () => document.removeEventListener('visibilitychange', refresh)
  }, [])
  return [st, refresh] as const
}

/** Keeps the server's copy of this device's subscription fresh (it may have been pruned). */
export async function resyncPush() {
  if (!pushSupported() || Notification.permission !== 'granted') return
  const r = await navigator.serviceWorker.getRegistration('/')
  const sub = await r?.pushManager.getSubscription()
  if (sub) await api('POST', '/push/subscriptions', sub.toJSON()).catch(() => {})
}
