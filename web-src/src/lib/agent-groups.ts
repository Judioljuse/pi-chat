// Grouping on the agents page (#/agents): machine (runtime.host), then git checkout (runtime.dirs.repo).
// Pure so `node --test` can run src/lib/agent-groups.test.mjs against it.

export interface GroupableAgent {
  id: string
  displayName: string
  online: boolean
  runtime?: {
    host?: string | null
    dirs?: { repo?: { root: string; branch?: string | null; worktree: boolean; path?: string | null } | null } | null
  } | null
}

export interface ProjectGroup<A> {
  /** Rename key ("proj:<host>|<root>|<branch or worktree path>"); null for 其他目录 (not renamable). */
  key: string | null
  label: string
  /** Label without a custom name, for the reset hint. */
  defaultLabel: string
  /** Full repo path, for the tooltip. */
  title: string
  agents: A[]
}

export interface MachineGroup<A> {
  key: string
  label: string
  defaultLabel: string
  /** No host reported. */
  unknown: boolean
  agents: A[]
  online: number
  projects: ProjectGroup<A>[]
}

export const UNKNOWN_HOST = '未知电脑'
export const OTHER_DIRS = '其他目录'

const basename = (p: string) => {
  const parts = p.replace(/[\\/]+$/, '').split(/[\\/]/)
  return parts.at(-1) || p
}

export const hostKey = (host: string) => `host:${host || '?'}`

export function projectKey(host: string, repo: { root: string; branch?: string | null; worktree: boolean; path?: string | null }) {
  const where = repo.worktree ? repo.path || repo.branch || '' : repo.branch || ''
  return `proj:${host || '?'}|${repo.root}|${where}`
}

export function projectLabel(repo: { root: string; branch?: string | null; worktree: boolean; path?: string | null }) {
  const name = basename(repo.root)
  if (repo.worktree) return `${name} · worktree ${repo.branch || basename(repo.path || '')}`.trim()
  return repo.branch ? `${name} · ${repo.branch}` : name
}

const cmp = (a: string, b: string) => a.localeCompare(b, 'zh-CN')

/** Machines sorted by name (unknown last); projects by label (其他目录 last); agents keep their input order. */
export function groupAgents<A extends GroupableAgent>(agents: A[], names: Record<string, string> = {}): MachineGroup<A>[] {
  const machines = new Map<string, MachineGroup<A>>()
  for (const a of agents) {
    const host = (a.runtime?.host || '').trim()
    const key = hostKey(host)
    let m = machines.get(key)
    if (!m) {
      const defaultLabel = host || UNKNOWN_HOST
      m = { key, label: names[key] || defaultLabel, defaultLabel, unknown: !host, agents: [], online: 0, projects: [] }
      machines.set(key, m)
    }
    m.agents.push(a)
    if (a.online) m.online++
    const repo = a.runtime?.dirs?.repo
    const pkey = repo?.root ? projectKey(host, repo) : null
    let p = m.projects.find((x) => x.key === pkey)
    if (!p) {
      const defaultLabel = repo?.root ? projectLabel(repo) : OTHER_DIRS
      p = {
        key: pkey,
        label: (pkey && names[pkey]) || defaultLabel,
        defaultLabel,
        title: repo?.root ? (repo.worktree && repo.path ? `${repo.path}（${repo.root} 的 worktree）` : repo.root) : '',
        agents: [],
      }
      m.projects.push(p)
    }
    p.agents.push(a)
  }
  const list = [...machines.values()].sort((a, b) => Number(a.unknown) - Number(b.unknown) || cmp(a.label, b.label))
  for (const m of list) m.projects.sort((a, b) => Number(!a.key) - Number(!b.key) || cmp(a.label, b.label))
  return list
}
