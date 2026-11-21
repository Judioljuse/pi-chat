/** Group tasks (v0.3.2): types and pure helpers shared by the chat task panel and the #/tasks board. */

export type TaskStatus = 'todo' | 'doing' | 'blocked' | 'done'

export interface TaskPerson {
  id: string
  name: string
  kind?: 'human' | 'agent'
}

export interface Task {
  id: string
  groupId: string
  groupName: string
  title: string
  note: string
  status: TaskStatus
  assignee: TaskPerson | null
  sourceSeq: number | null
  createdBy: TaskPerson | null
  updatedBy: TaskPerson | null
  createdAt: number
  updatedAt: number
  doneAt: number | null
}

export const TASK_STATUSES: TaskStatus[] = ['doing', 'blocked', 'todo', 'done']
export const OPEN_STATUSES: TaskStatus[] = ['doing', 'blocked', 'todo']
export const STATUS_LABEL: Record<TaskStatus, string> = { todo: '待办', doing: '进行中', blocked: '受阻', done: '已完成' }
export const TITLE_MAX = 200
export const NOTE_MAX = 4000

const STATUS_RANK: Record<TaskStatus, number> = { doing: 0, blocked: 1, todo: 2, done: 3 }

export const isOpen = (t: Pick<Task, 'status'>) => t.status !== 'done'

function person(raw: unknown): TaskPerson | null {
  if (!raw || typeof raw !== 'object') return null
  const r = raw as Record<string, unknown>
  const id = typeof r.id === 'string' ? r.id : ''
  if (!id) return null
  const name = [r.name, r.nick, r.displayName].find((x) => typeof x === 'string' && x) as string | undefined
  const kind = r.kind === 'agent' || r.kind === 'human' ? r.kind : undefined
  return { id, name: name || id, ...(kind && { kind }) }
}

const str = (v: unknown) => (typeof v === 'string' ? v : '')
const num = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) ? v : Number(v) || 0)

/** Normalizes a task from the API or an SSE event; null when it has no id. */
export function normalizeTask(raw: unknown): Task | null {
  if (!raw || typeof raw !== 'object') return null
  const r = raw as Record<string, unknown>
  const id = str(r.id)
  if (!id) return null
  const status = (TASK_STATUSES as string[]).includes(str(r.status)) ? (r.status as TaskStatus) : 'todo'
  const seq = num(r.sourceSeq)
  return {
    id,
    groupId: str(r.groupId),
    groupName: str(r.groupName),
    title: str(r.title),
    note: str(r.note),
    status,
    assignee: person(r.assignee),
    sourceSeq: Number.isInteger(seq) && seq > 0 ? seq : null,
    createdBy: person(r.createdBy),
    updatedBy: person(r.updatedBy),
    createdAt: num(r.createdAt),
    updatedAt: num(r.updatedAt) || num(r.createdAt),
    doneAt: num(r.doneAt) || null,
  }
}

export function normalizeTasks(list: unknown): Task[] {
  if (!Array.isArray(list)) return []
  return list.map(normalizeTask).filter((t): t is Task => !!t)
}

/** doing, blocked, todo, done; newest change first within a status. */
export function sortTasks(list: Task[]): Task[] {
  return [...list].sort((a, b) => STATUS_RANK[a.status] - STATUS_RANK[b.status] || b.updatedAt - a.updatedAt || a.id.localeCompare(b.id))
}

/** Insert or replace by id. */
export function upsertTask(list: Task[], t: Task): Task[] {
  const i = list.findIndex((x) => x.id === t.id)
  if (i < 0) return [...list, t]
  const next = list.slice()
  next[i] = t
  return next
}

export const removeTask = (list: Task[], id: string) => list.filter((x) => x.id !== id)

export const openCount = (list: Task[] | undefined) => (list ? list.filter(isOpen).length : 0)

export interface TaskSection {
  /** Assignee id, or '' for unassigned. */
  key: string
  assignee: TaskPerson | null
  open: Task[]
  done: Task[]
}

/**
 * One section per assignee: me first, then by name, unassigned last.
 * `nameOf` lets the caller show the current group nick instead of the stored name.
 */
export function sectionTasks(list: Task[], meId?: string, nameOf?: (id: string) => string | undefined): TaskSection[] {
  const by = new Map<string, TaskSection>()
  for (const t of sortTasks(list)) {
    const key = t.assignee?.id ?? ''
    let s = by.get(key)
    if (!s) {
      const a = t.assignee ? { ...t.assignee, name: nameOf?.(t.assignee.id) || t.assignee.name } : null
      s = { key, assignee: a, open: [], done: [] }
      by.set(key, s)
    }
    ;(isOpen(t) ? s.open : s.done).push(t)
  }
  return [...by.values()].sort((a, b) => {
    if (!a.key !== !b.key) return a.key ? -1 : 1
    if (meId && (a.key === meId) !== (b.key === meId)) return a.key === meId ? -1 : 1
    return (a.assignee?.name ?? '').localeCompare(b.assignee?.name ?? '', 'zh-CN')
  })
}

/** Title for "转为任务": the message's first line-collapsed 80 characters. */
export function taskTitleFrom(text: string, max = 80): string {
  const s = text.replace(/\s+/g, ' ').trim()
  return Array.from(s).slice(0, max).join('')
}

/** Query string for GET /tasks. */
export function boardQuery(opts: { assignee?: string; status?: 'open' | 'all' }): string {
  const p = new URLSearchParams()
  if (opts.assignee) p.set('assignee', opts.assignee)
  p.set('status', opts.status ?? 'open')
  return p.toString()
}

/** Whether a task belongs on the board for the current filters. */
export function matchesBoard(t: Task, opts: { assignee?: string; status?: 'open' | 'all' }, meId?: string): boolean {
  if ((opts.status ?? 'open') === 'open' && !isOpen(t)) return false
  if (!opts.assignee) return true
  const want = opts.assignee === 'me' ? meId : opts.assignee
  return t.assignee?.id === want
}
