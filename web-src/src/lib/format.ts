import { notify } from './store'

export function fmtTime(ms?: number | null): string {
  if (!ms) return ''
  const d = new Date(ms)
  const now = new Date()
  const hm = d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
  if (d.toDateString() === now.toDateString()) return hm
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  if (d.toDateString() === yesterday.toDateString()) return `昨天 ${hm}`
  const md = `${d.getMonth() + 1}/${d.getDate()}`
  return d.getFullYear() === now.getFullYear() ? `${md} ${hm}` : `${d.getFullYear()}/${md} ${hm}`
}

/** Compact time for list rows: "12:08", "昨天", "3/14". */
export function fmtShort(ms?: number | null): string {
  if (!ms) return ''
  const d = new Date(ms)
  const now = new Date()
  if (d.toDateString() === now.toDateString())
    return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  if (d.toDateString() === yesterday.toDateString()) return '昨天'
  const md = `${d.getMonth() + 1}/${d.getDate()}`
  return d.getFullYear() === now.getFullYear() ? md : `${d.getFullYear()}/${md}`
}

/** Full date for tables: "11-19 09:42", with the year when it differs. */
export function fmtDateTime(ms?: number | null): string {
  if (!ms) return ''
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  const s = `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
  return d.getFullYear() === new Date().getFullYear() ? s : `${d.getFullYear()}-${s}`
}

export const fmtSize = (n: number) =>
  n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : `${(n / 1048576).toFixed(1)} MB`

export function fmtElapsed(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${s % 60}s`
  return `${Math.floor(m / 60)}h ${m % 60}m`
}

export async function copy(text: string, done = '已复制') {
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    const t = document.createElement('textarea')
    t.value = text
    t.style.cssText = 'position:fixed;opacity:0'
    document.body.appendChild(t)
    t.select()
    document.execCommand('copy')
    t.remove()
  }
  notify(done)
}

export const randId = () =>
  Array.from(crypto.getRandomValues(new Uint8Array(12)), (b) => b.toString(16).padStart(2, '0')).join('')

export const ROLE: Record<string, string> = { owner: '群主', admin: '管理员', member: '成员' }
export const PLATFORM: Record<string, string> = { claude_code: 'Claude Code', pi: 'pi', other: '其他' }
export const NOT_WOKEN: Record<string, string> = {
  hop_limit: 'agent 接力已到上限',
  budget: 'owner 的 agent 唤醒预算已用完',
  sender_limit: '你唤醒它太频繁了',
  offline: '不在线',
  disabled: '已停用',
}
