import { NOT_WOKEN } from '@/lib/format'
import type { MentionTarget } from '@/lib/markdown'
import { plainText } from '@/lib/markdown'
import { notify } from '@/lib/store'
import type { FileRef, Group, Member, Message, SendResult } from '@/lib/types'

export const RECALL_MS = 2 * 60 * 1000
export const GROUP_GAP_MS = 5 * 60 * 1000
export const MAX_FILES = 8
export const MAX_FILE_SIZE = 10 * 1024 * 1024
export const INLINE_IMG = /^image\/(png|jpeg|gif|webp)$/

export const isCoarse = () => typeof matchMedia !== 'undefined' && matchMedia('(pointer: coarse)').matches

export const memberName = (m: Pick<Member, 'nick' | 'displayName'>) => m.nick || m.displayName

/** A message the user sent that the server has not confirmed yet. */
export interface Pending {
  opKey: string
  text: string
  files: FileRef[]
  mentions: string[]
  replyTo?: number
  createdAt: number
  status: 'sending' | 'error'
  error?: string
}

export function mentionTargets(members: Member[], meId: string | undefined): MentionTarget[] {
  const list: MentionTarget[] = members
    .filter((m) => m.nick)
    .map((m) => ({ nick: m.nick, kind: m.id === meId ? 'me' : m.kind === 'agent' ? 'agent' : 'human' }))
  list.push({ nick: 'all', kind: 'all' })
  return list
}

export function snippet(m: Message | null | undefined, max = 60): string {
  if (!m) return ''
  if (m.recalled) return '[已撤回]'
  if (m.text) return plainText(m.text, max)
  return m.files?.length ? '[附件]' : ''
}

const viaKey = (m: Message) => (m.via ? `${m.via.kind}:${m.via.session ?? ''}:${m.via.host ?? ''}` : '')

const JOIN = /^(.+) 加入了群$/

export type Row =
  | { type: 'msg'; key: string; m: Message; grouped: boolean; pending?: Pending }
  | { type: 'sys'; key: string; seqs: number[]; text: string; createdAt: number }
  | { type: 'divider'; key: string }

/** Turn the message list into render rows: unread divider, grouped runs, collapsed join notices. */
export function buildRows(msgs: Message[], firstUnread: number | null, keyOf: (seq: number) => string): Row[] {
  const rows: Row[] = []
  let prev: Message | undefined
  for (let i = 0; i < msgs.length; i++) {
    const m = msgs[i]
    if (firstUnread === m.seq && i > 0) {
      rows.push({ type: 'divider', key: 'unread' })
      prev = undefined
    }
    if (m.kind === 'system') {
      const last = rows.at(-1)
      const join = JOIN.exec(m.text)
      const lastJoin = last?.type === 'sys' && prev?.kind === 'system' && JOIN.test(prev.text)
      if (join && last?.type === 'sys' && lastJoin) {
        const names = last.text.replace(JOIN, '$1')
        last.seqs.push(m.seq)
        last.text = `${names}、${join[1]} 加入了群`
        last.createdAt = m.createdAt
      } else {
        rows.push({ type: 'sys', key: `m${m.seq}`, seqs: [m.seq], text: m.text, createdAt: m.createdAt })
      }
      prev = m
      continue
    }
    const grouped =
      !!prev &&
      prev.kind !== 'system' &&
      !!m.author &&
      prev.author?.id === m.author.id &&
      m.createdAt - prev.createdAt < GROUP_GAP_MS &&
      !m.replyTo &&
      // A different agent session starts a new run, so its provenance line shows.
      viaKey(prev) === viaKey(m)
    rows.push({ type: 'msg', key: keyOf(m.seq), m, grouped })
    prev = m
  }
  return rows
}

export function reportWake(r: SendResult, info: Group) {
  const nick = (id: string) => info.members.find((m) => m.id === id)?.nick || id
  const notes: string[] = []
  if (r.notWoken?.length)
    notes.push(r.notWoken.map((x) => `${nick(x.id)} 未唤醒（${NOT_WOKEN[x.reason] || x.reason}）`).join('；'))
  const h = r.mentionHint
  if (typeof h === 'string' && h) notes.push(h)
  else if (h && typeof h === 'object') {
    const unknown = h.unknown?.length ? `没有找到 ${h.unknown.map((x) => '@' + x).join('、')}` : '有 @ 没有匹配到成员'
    notes.push(h.available?.length ? `${unknown}，可以 @：${h.available.join('、')}` : unknown)
  }
  if (notes.length) notify(notes.join('\n'), 6000)
}
