import { api, ApiError } from './api'
import { maybeNotify } from './desktop-notify'
import {
  applyAgentEvent,
  applyDecisionEvent,
  applyTaskEvent,
  fullSync,
  getState,
  loadInfo,
  markRead,
  mergeMsgs,
  patchChat,
  patchMember,
  patchMessage,
  refreshGroupsSoon,
  setState,
  setWorking,
  softSync,
} from './store'
import type { AgentEvent } from './store'
import type { Message } from './types'

// everHello survives reconnects so each later hello catches up on the gap.
let es: EventSource | null = null
let everHello = false
let retryTimer: ReturnType<typeof setTimeout> | undefined

function parse<T>(e: Event): T {
  try {
    return JSON.parse((e as MessageEvent).data || '{}') as T
  } catch {
    return {} as T
  }
}

export function connectEvents() {
  closeEvents()
  const src = new EventSource('/api/v1/events')
  es = src
  const on = <T>(type: string, f: (d: T) => void) => src.addEventListener(type, (e) => f(parse<T>(e)))

  on('hello', () => {
    if (everHello) softSync()
    everHello = true
  })
  on<Message>('message', (m) => {
    if (getState().chats[m.groupId]?.loaded) patchChat(m.groupId, (c) => ({ msgs: mergeMsgs(c.msgs, [m]) }))
    markRead(m.groupId)
    refreshGroupsSoon()
    maybeNotify(m).catch(() => {})
  })
  on<{ groupId: string; seq: number }>('recall', ({ groupId, seq }) => {
    patchMessage(groupId, seq, { recalled: true, text: '', files: [] })
    refreshGroupsSoon()
  })
  on<{ groupId: string; memberId: string; seq: number }>('read', ({ groupId, memberId, seq }) => {
    const mem = getState().info[groupId]?.members.find((x) => x.id === memberId)
    if (mem && seq > (mem.lastReadSeq || 0)) patchMember(groupId, memberId, { lastReadSeq: seq })
    if (memberId === getState().me?.id) refreshGroupsSoon()
  })
  on<{ groupId: string }>('group', ({ groupId }) => {
    if (getState().info[groupId]) loadInfo(groupId).catch(() => {})
    refreshGroupsSoon()
  })
  on('groups', () => refreshGroupsSoon())
  on<{ groupId: string; agentId: string; startedAt: number }>('working', ({ groupId, agentId, startedAt }) => {
    if (groupId && agentId) setWorking(groupId, agentId, startedAt || 0)
  })
  on<AgentEvent>('agent', (e) => {
    if (e.agentId && e.state) applyAgentEvent(e)
  })
  on<{ groupId?: string; task?: unknown; taskId?: string; deleted?: boolean }>('task', (e) => applyTaskEvent(e))
  on<{ groupId?: string; decision?: import('./types').Decision }>('decision', (e) => applyDecisionEvent(e))
  on('resync', () => fullSync())
  on<{ available?: boolean }>('update', (e) => {
    if (getState().me?.isAdmin) setState({ updateAvailable: !!e.available })
  })
  on('restarting', () => setState({ restarting: true }))

  src.onerror = () => {
    if (src.readyState !== EventSource.CLOSED) return
    clearTimeout(retryTimer)
    // EventSource hides the status; /me routes a 401 to the login handler.
    retryTimer = setTimeout(() => {
      if (!getState().me) return
      api('GET', '/me').then(
        () => connectEvents(),
        (e) => {
          if (!(e instanceof ApiError) || e.status !== 401) connectEvents()
        },
      )
    }, 5000)
  }
}

export function closeEvents() {
  clearTimeout(retryTimer)
  if (es) {
    es.close()
    es = null
  }
}

export const eventsAlive = () => !!es && es.readyState !== EventSource.CLOSED
