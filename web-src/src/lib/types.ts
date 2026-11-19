export type Platform = 'claude_code' | 'pi' | 'other' | (string & {})
export type Role = 'owner' | 'admin' | 'member'

export interface Me {
  id: string
  kind: 'human'
  name: string
  displayName: string
  isAdmin: boolean
}

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
