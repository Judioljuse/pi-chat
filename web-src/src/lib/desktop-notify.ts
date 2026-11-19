import { useSyncExternalStore } from 'react'
import { api } from './api'
import { plainText } from './markdown'
import { navigate } from './router'
import { getState, loadInfo, notify, routeGid, setState } from './store'
import type { Message } from './types'

// ---- preference (localStorage) + permission, as one tiny external store ----

const PREF = 'pichat:desktop-notify' // 'on' | 'off'
const DISMISSED = 'pichat:desktop-notify-banner'

export const notifySupported = () => typeof window !== 'undefined' && 'Notification' in window
export const notifySecure = () => typeof window !== 'undefined' && window.isSecureContext

const subs = new Set<() => void>()
const emit = () => subs.forEach((f) => f())
const subscribe = (f: () => void) => {
  subs.add(f)
  return () => subs.delete(f)
}

export interface NotifyStatus {
  supported: boolean
  secure: boolean
  permission: NotificationPermission | 'unsupported'
  /** Permission granted and the user has not turned it off. */
  enabled: boolean
  /** Sidebar banner was closed. */
  dismissed: boolean
}

let cached: NotifyStatus | null = null
function snapshot(): NotifyStatus {
  const supported = notifySupported()
  const secure = notifySecure()
  const permission: NotifyStatus['permission'] = supported ? Notification.permission : 'unsupported'
  const enabled = supported && permission === 'granted' && localStorage.getItem(PREF) !== 'off'
  const dismissed = localStorage.getItem(DISMISSED) === '1'
  const next: NotifyStatus = { supported, secure, permission, enabled, dismissed }
  const prev = cached
  if (prev && (Object.keys(next) as (keyof NotifyStatus)[]).every((k) => prev[k] === next[k])) return prev
  cached = next
  return next
}

export const useNotifyStatus = () => useSyncExternalStore(subscribe, snapshot)

// The permission can change in browser settings; re-read it when the tab comes back.
if (typeof document !== 'undefined') document.addEventListener('visibilitychange', emit)

/** Must run from a user click: browsers ignore or penalize permission prompts without a gesture. */
export async function enableDesktopNotify(): Promise<boolean> {
  if (!notifySupported() || !notifySecure()) {
    notify('桌面通知需要 HTTPS 或 localhost 访问（例如 tailscale serve 或 ssh -L）', 6000)
    return false
  }
  let p = Notification.permission
  if (p === 'default') p = await Notification.requestPermission()
  if (p !== 'granted') {
    notify(p === 'denied' ? '浏览器已拒绝通知，请在地址栏的网站设置里允许通知' : '没有开启桌面通知')
    emit()
    return false
  }
  localStorage.setItem(PREF, 'on')
  localStorage.setItem(DISMISSED, '1')
  emit()
  return true
}

export function disableDesktopNotify() {
  localStorage.setItem(PREF, 'off')
  emit()
}

export function dismissNotifyBanner() {
  localStorage.setItem(DISMISSED, '1')
  emit()
}

// ---- deciding and showing ----

async function repliedAuthor(m: Message): Promise<string | undefined> {
  if (!m.replyTo) return
  const c = getState().chats[m.groupId]
  const local = c?.msgs.find((x) => x.seq === m.replyTo) ?? c?.quotes[m.replyTo]
  if (local) return local.author?.id
  try {
    const r = await api<{ messages: Message[] }>('GET', `/groups/${m.groupId}/messages?after=${m.replyTo - 1}&limit=1`)
    return r.messages?.find((x) => x.seq === m.replyTo)?.author?.id
  } catch {
    return
  }
}

/** Why a message concerns me: 'mention' (@ me), 'reply', 'dm'; null when it doesn't. Matches the server's @我 badge. */
async function reasonFor(m: Message, meId: string): Promise<'mention' | 'reply' | 'dm' | null> {
  if (m.kind !== 'text' || m.recalled || !m.author || m.author.id === meId) return null
  let kind: string | undefined = getState().groups.find((x) => x.id === m.groupId)?.kind ?? getState().info[m.groupId]?.kind
  // A brand-new DM is not in the (debounced) group list yet: ask the server.
  if (!kind) kind = await loadInfo(m.groupId).then((g) => g.kind).catch(() => undefined)
  if (kind === 'dm') return 'dm'
  // @all is meant for agents (the server records no @me for humans), so it doesn't notify either.
  if (m.mentions?.includes(meId)) return 'mention'
  if ((await repliedAuthor(m)) === meId) return 'reply'
  return null
}

// Chrome does not decode SVG notification icons: render the favicon to a PNG once.
let pngIconUrl: Promise<string | undefined> | null = null
function pngIcon(): Promise<string | undefined> {
  pngIconUrl ??= new Promise((resolve) => {
    const img = new Image()
    img.onload = () => {
      const c = document.createElement('canvas')
      c.width = c.height = 192
      const ctx = c.getContext('2d')
      if (!ctx) return resolve(undefined)
      ctx.drawImage(img, 0, 0, 192, 192)
      c.toBlob((b) => resolve(b ? URL.createObjectURL(b) : undefined), 'image/png')
    }
    img.onerror = () => resolve(undefined)
    img.src = '/static/icon.svg'
  })
  return pngIconUrl
}

const shown = new Set<string>()

/** Called for every live 'message' SSE event. */
export async function maybeNotify(m: Message) {
  const me = getState().me
  if (!me || !snapshot().enabled) return
  const looking = document.visibilityState === 'visible' && document.hasFocus() && routeGid() === m.groupId && getState().route.name === 'chat'
  if (looking) return
  const key = `${m.groupId}:${m.seq}`
  if (shown.has(key)) return
  const reason = await reasonFor(m, me.id)
  if (!reason) return
  shown.add(key)
  const g = getState().groups.find((x) => x.id === m.groupId) ?? getState().info[m.groupId]
  const who = m.author?.name || '有人'
  const where = reason === 'dm' ? '' : ` · ${g?.name || '群聊'}`
  const verb = reason === 'reply' ? '回复了你' : reason === 'mention' ? '@了你' : '私聊你'
  const body = plainText(m.text, 140) || (m.files?.length ? '[附件]' : '')
  try {
    const n = new Notification(`${who} ${verb}${where}`, { body, tag: key, icon: await pngIcon() })
    n.onclick = () => {
      window.focus()
      n.close()
      setState({ focus: { gid: m.groupId, seq: m.seq } })
      navigate(`#/g/${m.groupId}`)
    }
  } catch {
    /* Some mobile browsers only allow notifications from a service worker. */
  }
}

// ---- favicon badge ----

let baseIcon: HTMLImageElement | null = null
let iconHref: string | null = null
let badgeOn: boolean | null = null
let badgeUrl: string | null = null

function iconLink(): HTMLLinkElement | null {
  return document.querySelector<HTMLLinkElement>('link[rel="icon"]')
}

/** Red dot over the favicon while there are unread @me / replies / DMs. */
export function setFaviconBadge(on: boolean) {
  if (badgeOn === on) return
  badgeOn = on
  const link = iconLink()
  if (!link) return
  if (iconHref === null) iconHref = link.href
  if (!on) {
    link.type = 'image/svg+xml'
    link.href = iconHref
    return
  }
  const draw = () => {
    if (!badgeOn || !baseIcon) return
    const c = document.createElement('canvas')
    c.width = c.height = 64
    const ctx = c.getContext('2d')
    if (!ctx) return
    ctx.drawImage(baseIcon, 0, 0, 64, 64)
    ctx.beginPath()
    ctx.arc(48, 16, 14, 0, Math.PI * 2)
    ctx.fillStyle = '#e5484d'
    ctx.fill()
    ctx.lineWidth = 4
    ctx.strokeStyle = '#ffffff'
    ctx.stroke()
    // The page CSP allows blob: images but not data: URLs.
    c.toBlob((b) => {
      if (!b || !badgeOn) return
      if (badgeUrl) URL.revokeObjectURL(badgeUrl)
      badgeUrl = URL.createObjectURL(b)
      link.type = 'image/png'
      link.href = badgeUrl
    }, 'image/png')
  }
  if (baseIcon?.complete) return draw()
  baseIcon = new Image()
  baseIcon.onload = draw
  baseIcon.src = iconHref
}
