export type Platform = 'claude_code' | 'pi' | 'codex' | 'other' | (string & {})
export type Role = 'owner' | 'admin' | 'member'

export interface Me {
  id: string
  kind: 'human'
  name: string
  displayName: string
  isAdmin: boolean
}

/** What the agent's listener is doing; 'offline' when it has no SSE connection. */
export type AgentState = 'offline' | 'idle' | 'running' | 'attached'

export interface Member {
  id: string
  kind: 'human' | 'agent'
  nick: string
  displayName: string
  role: Role
  ownerId?: string | null
  ownerName?: string
  platform?: Platform | null
  online?: boolean
  lastReadSeq: number
  /** Agents only. */
  state?: AgentState
  mainSession?: string | null
  /** Listener notice (session used by a terminal process, fork detected), until replaced or dismissed. */
  notice?: string | null
}

export interface Working {
  agentId: string
  startedAt: number
}

export interface Group {
  id: string
  kind: 'group' | 'dm'
  name: string
  topic: string
  ownerId?: string | null
  lastSeq: number
  myRole: Role
  members: Member[]
  working?: Working[]
  createdAt?: number
  /** Agent relay limits for this group (server default unless the owner/admins changed them). */
  limits?: {
    /** -1 = unlimited (hops are still counted for display). */
    hopLimit: number
    agentWakesPerHour: number
    /** Per target agent: wakes caused by agent-authored messages in this group per hour (v0.3.3). */
    agentWakesPerAgentHour?: number
    unlimitedHops?: boolean
    default: boolean
  }
  /** Collaboration rules shown to every agent (server default text when rulesDefault). */
  rules?: string
  rulesDefault?: boolean
  /** Only for agent callers: who can command this agent in the group. */
  commanders?: { id: string; nick: string; kind: 'human' | 'agent' }[]
}

export interface GroupSummary {
  id: string
  kind: 'group' | 'dm'
  name: string
  topic: string
  lastSeq: number
  unread: number
  mentionsMe: number
  working?: string[]
  lastMessage: { seq: number; authorName: string; text: string; createdAt: number } | null
  peer?: { id: string; displayName: string; kind: 'human' | 'agent'; platform?: Platform | null }
}

export interface Author {
  id: string
  kind: 'human' | 'agent'
  name: string
  ownerId?: string | null
  ownerName?: string
  platform?: Platform | null
}

export interface FileRef {
  id: string
  name: string
  mime: string
  size: number
}

/** Where an agent-authored message came from. */
export interface Via {
  kind: 'run' | 'direct'
  session?: string | null
  tier?: string | null
  host?: string | null
}

export interface Message {
  seq: number
  groupId: string
  kind: 'text' | 'system'
  author: Author | null
  text: string
  mentions?: string[]
  mentionAll?: boolean
  replyTo?: number | null
  hop?: number
  files?: FileRef[]
  createdAt: number
  recalled: boolean
  new?: boolean
  via?: Via | null
}

export interface SendResult {
  message: Message
  mentioned?: { id: string; nick: string }[]
  mentionHint?: string | { unknown?: string[]; available?: string[] }
  woken?: string[]
  notWoken?: { id: string; reason: string }[]
  duplicate?: boolean
}

export interface Grant {
  userId: string
  groupId: string
  userName?: string
  /** 'agent' when another agent is granted to command this one. */
  userKind?: 'human' | 'agent' | ''
  groupName?: string
  createdAt?: number
}

export interface Agent {
  id: string
  displayName: string
  platform?: Platform | null
  intro: string
  commanderTier: 'project' | 'full'
  ownerNotes: string
  online: boolean
  disabled?: boolean
  lastSeenAt?: number | null
  wake: { kind: 'listener' | 'webhook' | 'none'; url: string; failStreak: number; lastError: string }
  grants: Grant[]
  recentRuns: { groupId: string; ok: boolean; detail: string; createdAt: number }[]
  runtime?: AgentRuntime | null
  sessions?: { session: string; host?: string | null; startedAt: number }[]
}

export interface AgentRuntime {
  state: AgentState
  session?: string | null
  host?: string | null
  name?: string | null
  at?: number | null
  notice?: string | null
  noticeAt?: number | null
  /** Listener script the listener reported; latest: same as the one this server serves. */
  listener?: { sha256: string; version: string; autoUpdate: boolean; skip?: 'git' | 'readonly' | null; latest: boolean } | null
  /** Owner only: working directories the listener reported (absent on older listeners). */
  dirs?: RuntimeDirs | null
}

/** Listener work dirs: main session cwd, config.dir, and per-group binds (groupId -> dir). */
export interface RuntimeDirs {
  main?: string | null
  default?: string | null
  binds?: Record<string, string> | null
  /** v0.2.7: git checkout of main (else default); absent on older listeners and outside a repo. */
  repo?: RepoInfo | null
  /** v0.2.7: git checkout per reported dir, keyed by dir. */
  repos?: Record<string, RepoInfo> | null
}

/** A git checkout as the listener sees it. root is the main repository's root, also for a worktree. */
export interface RepoInfo {
  root: string
  branch?: string | null
  worktree: boolean
  /** Worktrees: the worktree's own top level. */
  path?: string | null
}

/** Someone the caller can open a DM with (GET /contacts). */
export interface Contact {
  id: string
  kind: 'human' | 'agent'
  /** Login name, humans only. */
  name?: string
  displayName: string
  platform?: Platform | null
  ownerId?: string | null
  ownerName?: string
  online?: boolean
  state?: AgentState
}

/** GET /principals/{id}. host, mainSession and dirs are only sent to the agent's owner. */
export interface PrincipalProfile {
  id: string
  kind: 'human' | 'agent'
  name?: string
  displayName: string
  platform?: Platform | null
  ownerId?: string | null
  ownerName?: string
  intro?: string
  online?: boolean
  state?: AgentState
  lastSeenAt?: number | null
  commonGroups?: { id: string; name: string }[]
  host?: string | null
  mainSession?: string | null
  dirs?: RuntimeDirs | null
  disabled?: boolean
}

export interface UpdateRelease {
  tag: string
  name: string
  publishedAt: string
  notes: string
  htmlUrl: string
  asset: { name: string; size: number } | null
}

export type UpdatePhase = 'idle' | 'downloading' | 'verifying' | 'restarting' | 'error'

export interface UpdateStatus {
  enabled: boolean
  disabledReason?: string
  current: string
  builtin: string
  running: 'builtin' | 'installed'
  platform: string
  repo: string
  latest: UpdateRelease | null
  available: boolean
  checkedAt: number
  error?: string
  state: { phase: UpdatePhase; bytes: number; total: number; error?: string }
  installed: { version: string; path: string; installedAt: number; failed?: boolean } | null
}

export interface Invite {
  kind: 'admin' | 'user' | 'group'
  group?: { id: string; name: string; topic: string }
  valid: boolean
}

export interface AdminUser {
  id: string
  name: string
  displayName: string
  isAdmin: boolean
  createdAt: number
}
