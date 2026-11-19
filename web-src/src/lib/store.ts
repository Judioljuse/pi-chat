import { useSyncExternalStore } from 'react'
import { toast } from 'sonner'
import { api, ApiError, errorText } from './api'
import type { Agent, AgentState, Group, GroupSummary, Me, Member, Message, Working } from './types'

export type Route =
  | { name: 'home' }
  | { name: 'login' }
  | { name: 'redeem'; code: string }
  | { name: 'chat'; gid: string }
  | { name: 'settings'; gid: string }
  | { name: 'agents' }
  | { name: 'admin' }
  | { name: 'me' }

export interface ChatState {
  msgs: Message[]
  hasMore: boolean
  loaded: boolean
  loading: boolean
  firstUnread: number | null
  /** Replied-to messages outside the loaded window, fetched on demand; null while loading. */
  quotes: Record<number, Message | null>
}

export interface State {
  me: Me | null
  route: Route
  groups: GroupSummary[]
  groupsLoaded: boolean
  info: Record<string, Group>
  chats: Record<string, ChatState>
  /** groupId -> agentId -> startedAt (ms). */
  working: Record<string, Record<string, number>>
  agents: Agent[] | null
  /** A message to scroll to once its chat is open (desktop notification click). */
  focus: { gid: string; seq: number } | null
}

const initial = (): State => ({
  me: null,
  route: { name: 'home' },
  groups: [],
  groupsLoaded: false,
  info: {},
  chats: {},
  working: {},
  agents: null,
  focus: null,
})

let state: State = initial()
const subs = new Set<() => void>()

export const getState = () => state

export function setState(patch: Partial<State> | ((s: State) => Partial<State>)) {
  const p = typeof patch === 'function' ? patch(state) : patch
  state = { ...state, ...p }
  subs.forEach((f) => f())
}

export function resetState() {
  setState(initial())
}

const subscribe = (f: () => void) => {
  subs.add(f)
  return () => subs.delete(f)
}

/** Subscribe to a slice; the selector must return a stable reference for unchanged data. */
export function useStore<T>(select: (s: State) => T): T {
  return useSyncExternalStore(subscribe, () => select(state))
}

export const route = () => state.route
export const routeGid = () => {
  const r = state.route
  return r.name === 'chat' || r.name === 'settings' ? r.gid : undefined
}

// ---- toasts ----

export const notify = (msg: string, ms = 3500) => toast(msg, { duration: ms })
export const fail = (e: unknown) => toast.error(errorText(e))

// ---- chat helpers ----

const emptyChat = (): ChatState => ({ msgs: [], hasMore: true, loaded: false, loading: false, firstUnread: null, quotes: {} })

export const chatOf = (id: string): ChatState => state.chats[id] ?? emptyChat()

export function patchChat(id: string, f: (c: ChatState) => Partial<ChatState>) {
  setState((s) => {
    const c = s.chats[id] ?? emptyChat()
    return { chats: { ...s.chats, [id]: { ...c, ...f(c) } } }
  })
}

export function mergeMsgs(list: Message[], add: Message[] | undefined): Message[] {
  if (!add?.length) return list
  const m = new Map(list.map((x) => [x.seq, x]))
  for (const x of add) m.set(x.seq, { ...m.get(x.seq), ...x })
  return [...m.values()].sort((a, b) => a.seq - b.seq)
}

export function patchMessage(gid: string, seq: number, p: Partial<Message>) {
  const c = state.chats[gid]
  if (!c?.msgs.some((x) => x.seq === seq)) return
  patchChat(gid, (c) => ({ msgs: c.msgs.map((x) => (x.seq === seq ? { ...x, ...p } : x)) }))
}

export function patchMember(gid: string, pid: string, p: Partial<Member>) {
  const g = state.info[gid]
  if (!g) return
  setState((s) => ({
    info: { ...s.info, [gid]: { ...g, members: g.members.map((m) => (m.id === pid ? { ...m, ...p } : m)) } },
  }))
}

// ---- working ----

export function setWorking(gid: string, agentId: string, startedAt: number) {
  setState((s) => {
    const g = { ...s.working[gid] }
    if (startedAt) g[agentId] = startedAt
    else delete g[agentId]
    return { working: { ...s.working, [gid]: g } }
  })
}

function replaceWorking(gid: string, list: Working[] | undefined) {
  const g: Record<string, number> = {}
  for (const w of list ?? []) g[w.agentId] = w.startedAt
  setState((s) => ({ working: { ...s.working, [gid]: g } }))
}

/** Agents running anywhere, from the group summaries plus live events. */
export function workingAgentIds(s: Pick<State, 'working'>): Set<string> {
  const ids = new Set<string>()
  for (const g of Object.values(s.working)) for (const id of Object.keys(g)) ids.add(id)
  return ids
}

// ---- agent runtime ----

export interface AgentEvent {
  agentId: string
  state: AgentState
  session?: string | null
  host?: string | null
  name?: string | null
  /** Sent on every event; null when there is no notice. Absent from older servers. */
  notice?: string | null
  noticeAt?: number | null
}

/** Apply a live 'agent' SSE event to every group member list and to my agents page. */
export function applyAgentEvent(e: AgentEvent) {
  setState((s) => {
    const info: Record<string, Group> = {}
    for (const [gid, g] of Object.entries(s.info)) {
      if (!g.members.some((m) => m.id === e.agentId)) {
        info[gid] = g
        continue
      }
      info[gid] = {
        ...g,
        members: g.members.map((m) =>
          m.id === e.agentId
            ? {
                ...m,
                state: e.state,
                online: e.state !== 'offline',
                mainSession: e.session ?? m.mainSession,
                notice: e.notice !== undefined ? e.notice : m.notice,
              }
            : m,
        ),
      }
    }
    const agents =
      s.agents?.map((a) =>
        a.id === e.agentId
          ? {
              ...a,
              online: e.state !== 'offline',
              runtime: {
                ...a.runtime,
                state: e.state,
                session: e.session ?? a.runtime?.session,
                host: e.host ?? a.runtime?.host,
                name: e.name ?? a.runtime?.name,
                at: Date.now(),
                ...(e.notice !== undefined && { notice: e.notice, noticeAt: e.noticeAt }),
              },
            }
          : a,
      ) ?? null
    return { info, agents }
  })
}

/** Effective state: no SSE connection means offline whatever was stored; a live run means running. */
export function agentStateOf(m: { online?: boolean; state?: AgentState | null }, working = false): AgentState {
  if (m.online === false || m.state === 'offline') return 'offline'
  if (m.state === 'attached') return 'attached'
  if (working || m.state === 'running') return 'running'
  return m.online || m.state ? 'idle' : 'offline'
}

// ---- data ----

export async function loadGroups() {
  const { groups } = await api<{ groups: GroupSummary[] }>('GET', '/groups')
  const list = groups ?? []
  setState((s) => {
    const working = { ...s.working }
    for (const g of list) {
      const known = working[g.id] ?? {}
      const next: Record<string, number> = {}
      for (const id of g.working ?? []) next[id] = known[id] ?? Date.now()
      working[g.id] = next
    }
    return { groups: list, groupsLoaded: true, working }
  })
}

let groupsTimer: ReturnType<typeof setTimeout> | undefined
export function refreshGroupsSoon() {
  clearTimeout(groupsTimer)
  groupsTimer = setTimeout(() => loadGroups().catch(() => {}), 300)
}

export async function loadInfo(id: string): Promise<Group> {
  try {
    const { group } = await api<{ group: Group }>('GET', `/groups/${id}`)
    setState((s) => ({ info: { ...s.info, [id]: group } }))
    replaceWorking(id, group.working)
    return group
  } catch (e) {
    if (e instanceof ApiError && (e.status === 403 || e.status === 404)) {
      setState((s) => {
        const info = { ...s.info }
        const chats = { ...s.chats }
        delete info[id]
        delete chats[id]
        return { info, chats }
      })
      if (routeGid() === id) {
        notify('你已不在这个群里')
        location.hash = '#/'
      }
    }
    throw e
  }
}

export async function loadAgents() {
  const { agents } = await api<{ agents: Agent[] }>('GET', '/agents')
  setState({ agents: agents ?? [] })
  return agents ?? []
}

const PAGE = 50
type Page = { messages: Message[]; lastSeq: number }

export async function openChat(id: string) {
  const g = await loadInfo(id)
  if (!chatOf(id).loaded) {
    const meId = state.me?.id
    const myRead = g.members.find((x) => x.id === meId)?.lastReadSeq ?? 0
    const r = await api<Page>('GET', `/groups/${id}/messages?before=${g.lastSeq + 1}&limit=${PAGE}`)
    patchChat(id, (c) => {
      const msgs = mergeMsgs(c.msgs, r.messages)
      return {
        msgs,
        hasMore: (r.messages?.length ?? 0) >= PAGE && (msgs[0]?.seq ?? 0) > 1,
        firstUnread: myRead < g.lastSeq ? (msgs.find((x) => x.seq > myRead && x.author?.id !== meId)?.seq ?? null) : null,
        loaded: true,
      }
    })
  } else await catchUp(id)
  markRead(id)
}

export async function catchUp(id: string) {
  if (!state.chats[id]?.loaded) return
  for (let i = 0; i < 20; i++) {
    const after = chatOf(id).msgs.at(-1)?.seq ?? 0
    const r = await api<Page>('GET', `/groups/${id}/messages?after=${after}&limit=${PAGE}`)
    patchChat(id, (c) => ({ msgs: mergeMsgs(c.msgs, r.messages) }))
    if ((r.messages?.length ?? 0) < PAGE) break
  }
}

export async function loadOlder(id: string) {
  const c = state.chats[id]
  if (!c?.loaded || c.loading || !c.hasMore) return
  const before = c.msgs[0]?.seq
  if (!before || before <= 1) {
    patchChat(id, () => ({ hasMore: false }))
    return
  }
  patchChat(id, () => ({ loading: true }))
  try {
    const r = await api<Page>('GET', `/groups/${id}/messages?before=${before}&limit=${PAGE}`)
    patchChat(id, (c) => {
      const msgs = mergeMsgs(c.msgs, r.messages)
      return { msgs, hasMore: (r.messages?.length ?? 0) >= PAGE && (msgs[0]?.seq ?? 0) > 1 }
    })
  } finally {
    patchChat(id, () => ({ loading: false }))
  }
}

/** Fetch a single message (for reply quotes outside the loaded window). */
export function loadQuote(gid: string, seq: number) {
  const c = chatOf(gid)
  if (seq in c.quotes || c.msgs.some((m) => m.seq === seq)) return
  patchChat(gid, (c) => ({ quotes: { ...c.quotes, [seq]: null } }))
  api<Page>('GET', `/groups/${gid}/messages?after=${seq - 1}&limit=1`)
    .then((r) => {
      const m = r.messages?.find((x) => x.seq === seq)
      if (m) patchChat(gid, (c) => ({ quotes: { ...c.quotes, [seq]: m } }))
    })
    .catch(() => {})
}

const readTimers: Record<string, ReturnType<typeof setTimeout>> = {}
export function markRead(id: string) {
  if (document.visibilityState !== 'visible' || routeGid() !== id || state.route.name !== 'chat') return
  clearTimeout(readTimers[id])
  readTimers[id] = setTimeout(async () => {
    try {
      await api('POST', `/groups/${id}/read`, {})
      setState((s) => ({ groups: s.groups.map((g) => (g.id === id ? { ...g, unread: 0, mentionsMe: 0 } : g)) }))
    } catch {
      /* ignore */
    }
  }, 400)
}

export function softSync() {
  refreshGroupsSoon()
  const id = routeGid()
  if (id && state.chats[id]?.loaded) {
    loadInfo(id).catch(() => {})
    catchUp(id)
      .then(() => markRead(id))
      .catch(() => {})
  }
}

export function fullSync() {
  setState({ chats: {}, info: {} })
  refreshGroupsSoon()
  const r = state.route
  if (r.name === 'chat') openChat(r.gid).catch(() => {})
}
