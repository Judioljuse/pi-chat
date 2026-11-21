import { useEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import { Check, Copy, Ellipsis, FolderGit2, KeyRound, Monitor, Pencil, Plus, RefreshCw, RotateCcw, Search, Trash2, X } from 'lucide-react'
import TaskRows, { type TaskRow } from '@/components/beautiful/task-rows'
import CodeBlock from '@/components/beautiful/code-block'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/motion/dropdown-menu'
import { Collapsible, CollapsibleContent, CollapsibleIndicator, CollapsibleTrigger } from '@/components/motion/collapsible'
import { Alert, AlertClose, AlertContent, AlertDescription, AlertIcon } from '@/components/motion/alert'
import { AgentStateChip } from '@/components/pichat/agent-state'
import { AgentTag, PrincipalAvatar } from '@/components/pichat/avatar'
import { useConfirm } from '@/components/pichat/confirm'
import { CopyField } from '@/components/pichat/copy-field'
import { Empty, Field, Page, Section } from '@/components/pichat/page'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { groupAgents, type MachineGroup, type ProjectGroup } from '@/lib/agent-groups'
import { api, ApiError } from '@/lib/api'
import { copy, fmtTime, PLATFORM, PLATFORM_LIMITS } from '@/lib/format'
import { agentStateOf, terminalModeOf, fail, getState, loadAgents, loadGroups, loadInfo, notify, setState, useStore, workingAgentIds } from '@/lib/store'
import type { Agent, Grant, Member } from '@/lib/types'
import { cn } from '@/lib/utils'

type PairCode = { code: string; instruction: string; expiresAt?: number }

const NONE = '__none'
const ALL = '*'

function replaceAgent(id: string, patch: Partial<Agent>) {
  setState((s) => ({ agents: s.agents?.map((x) => (x.id === id ? { ...x, ...patch } : x)) ?? null }))
}

export function Agents() {
  const agents = useStore((s) => s.agents)
  const [refreshing, setRefreshing] = useState(false)

  useEffect(() => {
    loadAgents().catch(fail)
    if (!getState().groupsLoaded) loadGroups().catch(() => {})
  }, [])

  const refresh = async () => {
    setRefreshing(true)
    await loadAgents().catch(fail)
    setRefreshing(false)
  }

  return (
    <Page
      title="我的 agent"
      back="#/"
      actions={
        <Button variant="ghost" size="sm" onClick={refresh} disabled={refreshing}>
          <RefreshCw className={cn(refreshing && 'animate-spin')} />
          刷新
        </Button>
      }
    >
      <PairSection />
      {agents == null ? (
        <Skeleton className="h-64 w-full rounded-card" />
      ) : agents.length === 0 ? (
        <Empty className="py-10">还没有 agent。生成配对码，让你的 Claude Code、pi 或 Codex 接进来。</Empty>
      ) : (
        <AgentGroups agents={agents} />
      )}
    </Page>
  )
}

// ---- grouping by machine and project (v0.2.7) ----

/** Custom group names from GET /agent-groups; an older server without the endpoint leaves them empty. */
function useGroupNames() {
  const [names, setNames] = useState<Record<string, string>>({})
  const [supported, setSupported] = useState(true)
  useEffect(() => {
    api<{ names?: Record<string, string> }>('GET', '/agent-groups')
      .then((r) => setNames(r.names ?? {}))
      .catch((e) => {
        if (e instanceof ApiError && (e.status === 404 || e.status === 405)) setSupported(false)
      })
  }, [])
  const rename = async (key: string, name: string) => {
    try {
      const r = await api<{ names?: Record<string, string> }>('PUT', '/agent-groups', { key, name })
      setNames(r.names ?? {})
      notify(name ? '已改名' : '已恢复默认名称')
      return true
    } catch (e) {
      fail(e)
      return false
    }
  }
  return { names, rename: supported ? rename : undefined }
}

function AgentGroups({ agents }: { agents: Agent[] }) {
  const { names, rename } = useGroupNames()
  const [q, setQ] = useState('')
  const focus = useStore((s) => (s.route.name === 'agents' ? s.route.focus : undefined))
  const needle = q.trim().toLowerCase()
  const shown = useMemo(
    () => (needle ? agents.filter((a) => a.displayName.toLowerCase().includes(needle)) : agents),
    [agents, needle],
  )
  const machines = useMemo(() => groupAgents(shown, names), [shown, names])
  return (
    <div className="flex flex-col gap-3">
      {agents.length > 3 && (
        <label className="flex h-8 items-center gap-2 rounded-control border border-border bg-card px-2 text-muted-foreground transition-colors focus-within:border-input">
          <Search className="size-4 shrink-0" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => e.key === 'Escape' && setQ('')}
            placeholder="按名字搜索 agent"
            aria-label="按名字搜索 agent"
            className="h-full min-w-0 flex-1 bg-transparent text-[13px] text-foreground outline-none placeholder:text-muted-foreground"
          />
          {q && (
            <button type="button" aria-label="清除搜索" onClick={() => setQ('')} className="grid size-5 place-items-center rounded-chip hover:bg-accent hover:text-foreground">
              <X className="size-3.5" />
            </button>
          )}
        </label>
      )}
      {machines.length === 0 ? (
        <Empty className="py-6">没有名字包含「{q.trim()}」的 agent</Empty>
      ) : (
        machines.map((m) => (
          <MachineSection
            key={m.key}
            m={m}
            forceOpen={!!needle || (!!focus && m.agents.some((a) => a.id === focus))}
            rename={rename}
          />
        ))
      )}
    </div>
  )
}

const COLLAPSED_KEY = 'pichat.agentGroups.collapsed'
const readCollapsed = (): string[] => {
  try {
    const v = JSON.parse(localStorage.getItem(COLLAPSED_KEY) || '[]')
    return Array.isArray(v) ? v.filter((x) => typeof x === 'string') : []
  } catch {
    return []
  }
}

function MachineSection({
  m,
  forceOpen,
  rename,
}: {
  m: MachineGroup<Agent>
  forceOpen: boolean
  rename?: (key: string, name: string) => Promise<boolean>
}) {
  const [open, setOpenState] = useState(() => !readCollapsed().includes(m.key))
  const setOpen = (o: boolean) => {
    setOpenState(o)
    const rest = readCollapsed().filter((k) => k !== m.key)
    localStorage.setItem(COLLAPSED_KEY, JSON.stringify(o ? rest : [...rest, m.key].slice(-100)))
  }
  const [editing, setEditing] = useState(false)
  const flat = m.projects.length === 1 && !m.projects[0].key
  return (
    <Collapsible open={open || forceOpen} onOpenChange={setOpen} className="flex flex-col" data-machine={m.key}>
      <div className="group/head flex min-h-9 items-center gap-1.5">
        {editing && rename ? (
          <InlineRename
            value={m.label}
            placeholder={m.defaultLabel}
            onCancel={() => setEditing(false)}
            onSave={async (name) => (await rename(m.key, name === m.defaultLabel ? '' : name)) && setEditing(false)}
          />
        ) : (
          <CollapsibleTrigger className="-ml-2 min-h-8 w-auto min-w-0 flex-initial gap-2 rounded-control px-2 py-1 font-normal hover:bg-accent/60">
            <Monitor className="size-4 shrink-0 text-muted-foreground" />
            <span className="truncate text-[14px] font-medium text-foreground" title={m.label !== m.defaultLabel ? m.defaultLabel : undefined}>
              {m.label}
            </span>
            <span className="shrink-0 text-[12.5px] text-muted-foreground tabular-nums">
              {m.agents.length} 个 · {m.online} 个在线
            </span>
            <CollapsibleIndicator className="ml-0" />
          </CollapsibleTrigger>
        )}
        {!editing && rename && !m.unknown && (
          <RenameButton label={`重命名电脑「${m.label}」`} onClick={() => setEditing(true)} />
        )}
      </div>
      <CollapsibleContent>
        <div className="flex flex-col gap-4 pt-1 pb-2">
          {flat
            ? m.projects[0].agents.map((a) => <AgentCard key={a.id} a={a} />)
            : m.projects.map((p) => <ProjectBlock key={p.key ?? 'other'} p={p} rename={rename} />)}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

function ProjectBlock({ p, rename }: { p: ProjectGroup<Agent>; rename?: (key: string, name: string) => Promise<boolean> }) {
  const [editing, setEditing] = useState(false)
  return (
    <div className="flex flex-col gap-2 border-l border-border pl-3" data-project={p.key ?? ''}>
      <div className="group/head flex min-h-7 items-center gap-1.5">
        {editing && rename && p.key ? (
          <InlineRename
            value={p.label}
            placeholder={p.defaultLabel}
            onCancel={() => setEditing(false)}
            onSave={async (name) => (await rename(p.key!, name === p.defaultLabel ? '' : name)) && setEditing(false)}
          />
        ) : (
          <>
            <FolderGit2 className={cn('size-3.5 shrink-0 text-muted-foreground', !p.key && 'opacity-60')} />
            <span className="truncate text-[13px] font-medium text-body" title={p.title || undefined}>
              {p.label}
            </span>
            <span className="shrink-0 text-[12px] text-muted-foreground tabular-nums">{p.agents.length}</span>
            {rename && p.key && <RenameButton label={`重命名项目分组「${p.label}」`} onClick={() => setEditing(true)} />}
          </>
        )}
      </div>
      {p.agents.map((a) => (
        <AgentCard key={a.id} a={a} />
      ))}
    </div>
  )
}

function RenameButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      aria-label={label}
      title="重命名"
      onClick={onClick}
      className="grid size-6 shrink-0 place-items-center rounded-chip text-muted-foreground opacity-0 transition-opacity group-hover/head:opacity-100 hover:bg-accent hover:text-foreground focus-visible:opacity-100 [@media(hover:none)]:opacity-100 [&_svg]:size-3.5"
    >
      <Pencil />
    </button>
  )
}

/** Pencil -> inline input. Enter saves, Esc cancels; an empty name resets to the default label. */
function InlineRename({
  value,
  placeholder,
  onSave,
  onCancel,
}: {
  value: string
  placeholder: string
  onSave: (name: string) => Promise<unknown>
  onCancel: () => void
}) {
  const [name, setName] = useState(value)
  const [busy, setBusy] = useState(false)
  const save = async () => {
    if (busy) return
    const v = name.trim()
    if (v === value) return onCancel()
    setBusy(true)
    await onSave(v)
    setBusy(false)
  }
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      save()
    } else if (e.key === 'Escape') onCancel()
  }
  return (
    <div className="flex min-w-0 flex-1 items-center gap-1">
      <Input
        autoFocus
        value={name}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={onKey}
        maxLength={40}
        placeholder={`${placeholder}（留空恢复默认）`}
        aria-label="分组名称"
        className="h-7 max-w-72 bg-card text-[13px]"
        disabled={busy}
      />
      <Button variant="ghost" size="icon-sm" aria-label="保存" onClick={save} disabled={busy}>
        <Check />
      </Button>
      <Button variant="ghost" size="icon-sm" aria-label="取消" onClick={onCancel} disabled={busy}>
        <X />
      </Button>
    </div>
  )
}

function GroupSelect({
  value,
  onChange,
  placeholder,
  extra,
  className,
}: {
  value: string
  onChange: (v: string) => void
  placeholder: string
  extra?: { value: string; label: string }[]
  className?: string
}) {
  const groups = useStore((s) => s.groups)
  const rooms = useMemo(() => groups.filter((g) => g.kind === 'group'), [groups])
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className={cn('w-full bg-background', className)}>
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent position="popper">
        {extra?.map((x) => (
          <SelectItem key={x.value} value={x.value}>
            {x.label}
          </SelectItem>
        ))}
        {rooms.map((g) => (
          <SelectItem key={g.id} value={g.id}>
            {g.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function PairSection() {
  const [pair, setPair] = useState<PairCode | null>(null)
  const [gid, setGid] = useState(NONE)
  const [busy, setBusy] = useState(false)
  const gen = async () => {
    setBusy(true)
    try {
      setPair(await api<PairCode>('POST', '/pairing-codes', gid !== NONE ? { groupId: gid } : {}))
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Section
      title="添加 agent"
      description="生成配对码，把下面这段话发给你的 agent（Claude Code、pi 或 Codex），它会自己完成接入。配对码 30 分钟内有效，只能用一次。"
    >
      <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
        <div className="flex-1">
          <Field label="配对后自动加入群（可选）">
            <GroupSelect value={gid} onChange={setGid} placeholder="不加入" extra={[{ value: NONE, label: '不加入' }]} />
          </Field>
        </div>
        <Button onClick={gen} disabled={busy}>
          <Plus />
          {pair ? '重新生成' : '生成配对码'}
        </Button>
      </div>
      {pair && <PairCodeView pair={pair} />}
    </Section>
  )
}

function PairCodeView({ pair }: { pair: PairCode }) {
  return (
    <div className="flex flex-col gap-3 animate-fade-up">
      <div className="flex items-center gap-3 rounded-card bg-brand-light px-4 py-3">
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="text-[12px] text-brand">配对码</span>
          <span className="font-mono text-[22px] tracking-[0.12em] text-foreground tabular-nums">{pair.code}</span>
        </div>
        {pair.expiresAt && <span className="text-[12px] text-muted-foreground">{fmtTime(pair.expiresAt)} 前有效</span>}
      </div>
      {pair.instruction && (
        <CodeBlock
          filename="发给 agent 的话"
          lines={pair.instruction.split('\n')}
          code={pair.instruction}
          labels={{ copy: '复制', copied: '已复制' }}
          className="max-w-none"
        />
      )}
    </div>
  )
}

function AgentCard({ a }: { a: Agent }) {
  const working = useStore((s) => workingAgentIds(s).has(a.id))
  // #/agents/<id> (the profile card's 管理 button): expand this card and scroll to it.
  const focused = useStore((s) => s.route.name === 'agents' && s.route.focus === a.id)
  const { ask, dialog } = useConfirm()
  const [renaming, setRenaming] = useState(false)
  const [reset, setReset] = useState<PairCode | null>(null)
  const [open, setOpen] = useState(focused)
  const card = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!focused) return
    setOpen(true)
    const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches
    const t = setTimeout(() => card.current?.scrollIntoView({ block: 'start', behavior: reduce ? 'auto' : 'smooth' }), 50)
    return () => clearTimeout(t)
  }, [focused])
  const platform = PLATFORM[a.platform ?? ''] || a.platform || '未知平台'

  const patch = async (body: Partial<Pick<Agent, 'displayName' | 'commanderTier' | 'ownerNotes' | 'disabled'>>, msg = '已保存') => {
    try {
      const { agent } = await api<{ agent?: Agent }>('PATCH', `/agents/${a.id}`, body)
      replaceAgent(a.id, agent ?? body)
      notify(msg)
      return true
    } catch (e) {
      fail(e)
      return false
    }
  }

  const setTier = async (tier: string) => {
    if (!tier || tier === a.commanderTier) return
    if (
      tier === 'full' &&
      !(await ask({
        title: `把「${a.displayName}」设为 full 档？`,
        description: '指挥者 @ 它时，它会拥有所在机器上当前用户的完整权限：读写全部目录、执行任意命令。请只授权你信任的人。',
        confirmText: '设为 full',
        destructive: true,
      }))
    )
      return
    patch({ commanderTier: tier as Agent['commanderTier'] }, tier === 'full' ? '已切换到 full' : '已切换到 project')
  }

  const resetToken = async () => {
    const yes = await ask({
      title: '重置 token？',
      description: '旧 token 立即失效，需要用新的配对码重新接入。',
      confirmText: '重置',
      destructive: true,
    })
    if (!yes) return
    try {
      setReset(await api<PairCode>('POST', `/agents/${a.id}/reset-token`, {}))
      setOpen(true)
    } catch (e) {
      fail(e)
    }
  }

  const del = async () => {
    const yes = await ask({
      title: `删除「${a.displayName}」？`,
      description: '会吊销它的 token，并把它移出所有群。',
      confirmText: '删除',
      destructive: true,
    })
    if (!yes) return
    try {
      await api('DELETE', `/agents/${a.id}`)
      await loadAgents()
      notify('已删除')
    } catch (e) {
      fail(e)
    }
  }

  const state = agentStateOf({ online: a.online, state: a.runtime?.state }, working)
  const terminal = terminalModeOf({ online: a.online, state: a.runtime?.state })
  const status =
    state !== 'offline' ? '' : a.lastSeenAt ? `最后在线 ${fmtTime(a.lastSeenAt)}` : '从未上线'

  return (
    <Collapsible
      ref={card}
      id={`agent-${a.id}`}
      open={open}
      onOpenChange={setOpen}
      className={cn('rounded-card border border-border bg-card animate-fade-up', a.disabled && 'opacity-80')}
    >
      <header className="flex items-center gap-3 p-3 pl-4">
        <CollapsibleTrigger className="-my-1 -ml-2 min-w-0 flex-1 gap-3 rounded-control px-2 py-1 hover:bg-accent/60 font-normal">
        <PrincipalAvatar id={a.id} name={a.displayName} kind="agent" platform={a.platform} size="lg" working={working} online={a.online && !working} />
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <div className="flex flex-wrap items-center gap-1.5">
            <h2 className="truncate text-[15px] font-medium text-foreground">{a.displayName}</h2>
            <AgentTag />
            <span className="rounded-chip bg-tag px-1.5 py-px text-[11px] text-brand">{platform}</span>
            {a.disabled && <span className="rounded-chip bg-destructive/10 px-1.5 py-px text-[11px] text-destructive">已停用</span>}
          </div>
          <div className="flex items-center gap-1.5 text-[12.5px] text-muted-foreground">
            <AgentStateChip state={state} terminal={terminal} className="text-[12.5px]" />
            {status && <span>· {status}</span>}
            {state !== 'offline' && a.runtime?.host && <span className="truncate">· {a.runtime.host}</span>}
          </div>
          {a.intro && <p className="truncate text-[12.5px] text-body">{a.intro}</p>}
        </div>
        <CollapsibleIndicator className="ml-0" />
        </CollapsibleTrigger>
        <label className="flex shrink-0 items-center gap-2 text-[12.5px] text-muted-foreground">
          <span className="max-sm:sr-only">启用</span>
          <Switch checked={!a.disabled} onCheckedChange={(on) => patch({ disabled: !on }, on ? '已启用' : '已停用')} aria-label="启用" />
        </label>
        <DropdownMenu>
          <DropdownMenuTrigger>
            <Button variant="ghost" size="icon-sm" aria-label="更多操作">
              <Ellipsis />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="min-w-40">
            <DropdownMenuItem onSelect={() => setRenaming(true)}>
              <Pencil className="size-4" />
              改名
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={resetToken}>
              <KeyRound className="size-4" />
              重置 token
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem tone="destructive" onSelect={del}>
              <Trash2 className="size-4" />
              删除
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </header>

      <CollapsibleContent>
      {reset && (
        <div className="flex flex-col gap-2 px-4 pb-4">
          <p className="text-[13px] text-body">把下面这段话发给 {a.displayName}，它会用新配对码重新接入。</p>
          <PairCodeView pair={reset} />
        </div>
      )}

      <Separator />
      <div className="flex flex-col gap-5 p-4">
        <Row label="指挥档位" hint={`${a.commanderTier === 'full' ? '指挥者 @ 它时：完整权限，读写全部目录、执行任意命令。' : '指挥者 @ 它时：只在绑定的项目目录里工作。'}其他人 @ 它只能让它回话。`}>
          <Segmented
            value={a.commanderTier}
            onValueChange={setTier}
            options={[
              { value: 'project', label: '项目内', hint: 'project' },
              { value: 'full', label: '完整权限', hint: 'full' },
            ]}
          />
        </Row>

        {PLATFORM_LIMITS[a.platform ?? ''] && (
          <p className="rounded-control bg-accent/60 px-3 py-2 text-[12.5px] leading-relaxed text-body">{PLATFORM_LIMITS[a.platform ?? '']}</p>
        )}
        <Runtime a={a} state={state} />
        <NotesEditor a={a} onSave={(ownerNotes) => patch({ ownerNotes })} />
        <Grants a={a} />
        <WakeEditor a={a} />
        <Runs a={a} />
      </div>
      </CollapsibleContent>

      <RenameDialog a={a} open={renaming} onOpenChange={setRenaming} onSave={(displayName) => patch({ displayName }, '已改名')} />
      {dialog}
    </Collapsible>
  )
}

/** Segmented control on shadcn ToggleGroup: accent track, raised card for the selected option. */
function Segmented({
  value,
  onValueChange,
  options,
}: {
  value: string
  onValueChange: (v: string) => void
  options: { value: string; label: string; hint?: string }[]
}) {
  return (
    <ToggleGroup
      type="single"
      spacing={0}
      value={value}
      onValueChange={(v) => v && onValueChange(v)}
      className="h-8 w-fit gap-0.5 rounded-control bg-accent p-0.5"
    >
      {options.map((o) => (
        <ToggleGroupItem
          key={o.value}
          value={o.value}
          title={o.hint}
          className="h-7 gap-1.5 rounded-[6px]! px-3 text-[13px] font-normal text-muted-foreground hover:bg-transparent hover:text-foreground data-[state=on]:bg-card data-[state=on]:text-foreground data-[state=on]:shadow-hairline"
        >
          {o.label}

        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}

function Row({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-2">
      <div className="text-[13px] font-medium text-foreground">{label}</div>
      {children}
      {hint && <p className="text-[12px] text-muted-foreground">{hint}</p>}
    </div>
  )
}

function RenameDialog({
  a,
  open,
  onOpenChange,
  onSave,
}: {
  a: Agent
  open: boolean
  onOpenChange: (o: boolean) => void
  onSave: (name: string) => Promise<boolean>
}) {
  const [name, setName] = useState(a.displayName)
  useEffect(() => {
    if (open) setName(a.displayName)
  }, [open, a.displayName])
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (name.trim() && name.trim() !== a.displayName && (await onSave(name.trim()))) onOpenChange(false)
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>给 agent 改名</DialogTitle>
          </DialogHeader>
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} maxLength={64} aria-label="名字" />
          <DialogFooter>
            <Button type="submit" disabled={!name.trim() || name.trim() === a.displayName}>
              保存
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function NotesEditor({ a, onSave }: { a: Agent; onSave: (v: string) => Promise<boolean> }) {
  const [notes, setNotes] = useState(a.ownerNotes || '')
  const [busy, setBusy] = useState(false)
  useEffect(() => setNotes(a.ownerNotes || ''), [a.ownerNotes])
  const dirty = notes !== (a.ownerNotes || '')
  return (
    <Row label="长期说明" hint="写给 agent 的固定要求，每次唤醒都会带上。">
      <div className="relative">
        <Textarea
          rows={3}
          maxLength={2000}
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          placeholder="例如：回复保持简短；改代码前先说明计划"
          className="bg-background pb-6"
        />
        <span className="pointer-events-none absolute right-2.5 bottom-1.5 font-mono text-[11px] text-muted-foreground tabular-nums">
          {notes.length}/2000
        </span>
      </div>
      {dirty && (
        <div className="flex gap-2 animate-fade-in">
          <Button
            size="sm"
            disabled={busy}
            onClick={async () => {
              setBusy(true)
              await onSave(notes)
              setBusy(false)
            }}
          >
            保存说明
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setNotes(a.ownerNotes || '')}>
            还原
          </Button>
        </div>
      )}
    </Row>
  )
}

function findHuman(id: string) {
  for (const g of Object.values(getState().info)) {
    const m = g.members?.find((x) => x.id === id)
    if (m) return m.displayName || m.nick
  }
  return ''
}

function Grants({ a }: { a: Agent }) {
  const groups = useStore((s) => s.groups)
  const info = useStore((s) => s.info)
  const meId = useStore((s) => s.me?.id)
  const [gid, setGid] = useState('')
  const [uid, setUid] = useState('')
  const [busy, setBusy] = useState(false)
  const rooms = useMemo(() => groups.filter((g) => g.kind === 'group'), [groups])

  useEffect(() => {
    setUid('')
    const ids = gid === ALL ? rooms.map((g) => g.id) : gid ? [gid] : []
    for (const id of ids) if (!getState().info[id]) loadInfo(id).catch(() => {})
    // Refetch only when the picked group changes.
  }, [gid])

  // Humans first, then agents (an agent can be granted to command this one); never me or this agent.
  const people = useMemo(() => {
    const src = gid === ALL ? rooms.map((g) => info[g.id]) : gid ? [info[gid]] : []
    const m = new Map<string, Member>()
    // Agent commanders must be granted per group (server rejects 所有群 for agents).
    for (const g of src) for (const x of g?.members ?? []) if (x.id !== meId && x.id !== a.id && !(gid === ALL && x.kind === 'agent')) m.set(x.id, x)
    return [...m.values()].sort((x, y) => (x.kind === y.kind ? 0 : x.kind === 'human' ? -1 : 1))
  }, [gid, rooms, info, meId, a.id])

  const userName = (g: Grant) => (g.userId === ALL ? '所有人类' : g.userName || findHuman(g.userId) || g.userId)
  const groupName = (g: Grant) =>
    g.groupId === ALL ? '所有群' : g.groupName || groups.find((x) => x.id === g.groupId)?.name || g.groupId

  const change = async (method: 'PUT' | 'DELETE', body: { userId: string; groupId: string }) => {
    setBusy(true)
    try {
      const { grants } = await api<{ grants: Grant[] }>(method, `/agents/${a.id}/grants`, body)
      replaceAgent(a.id, { grants: grants ?? [] })
      if (method === 'PUT') {
        setUid('')
        notify('已授权')
      } else notify('已撤销')
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Row label="指挥者授权" hint="你本人永远是指挥者。被授权的人或 agent 在对应的群里 @ 它时，它会按上面的档位执行任务。授权 agent 后，agent 之间的接力仍受 6 跳上限和每小时唤醒次数限制；被授权的 agent 被非指挥者叫醒时写的消息不算指令。">
      {a.grants?.length ? (
        <ul className="flex flex-col divide-y divide-border rounded-control border border-border">
          {a.grants.map((g) => (
            <li key={g.userId + g.groupId} className="flex items-center gap-2 px-3 py-2 text-[13px]">
              <span className="min-w-0 flex-1 truncate">
                <span className="font-medium text-foreground">{userName(g)}</span>
                {g.userKind === 'agent' && <AgentTag className="ml-1 align-middle" />}
                <span className="mx-1.5 text-muted-foreground">在</span>
                <span className="text-body">{groupName(g)}</span>
              </span>
              <Button
                variant="ghost"
                size="xs"
                className="text-muted-foreground hover:text-destructive"
                disabled={busy}
                onClick={() => change('DELETE', { userId: g.userId, groupId: g.groupId })}
              >
                <X />
                撤销
              </Button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-[12.5px] text-muted-foreground">暂无授权</p>
      )}
      <div className="flex flex-col gap-2 sm:flex-row">
        <GroupSelect value={gid} onChange={setGid} placeholder="选择群" extra={[{ value: ALL, label: '所有群' }]} className="sm:flex-1" />
        <Select value={uid} onValueChange={setUid} disabled={!gid}>
          <SelectTrigger className="w-full bg-background sm:flex-1">
            <SelectValue placeholder="选择人或 agent" />
          </SelectTrigger>
          <SelectContent position="popper">
            {gid && gid !== ALL && <SelectItem value={ALL}>该群所有人类</SelectItem>}
            {people.map((m) => (
              <SelectItem key={m.id} value={m.id}>
                {m.displayName || m.nick}
                {m.kind === 'agent' && <AgentTag />}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button disabled={!gid || !uid || busy} onClick={() => change('PUT', { userId: uid, groupId: gid })}>
          授权
        </Button>
      </div>
    </Row>
  )
}

const WAKE_HINT: Record<Agent['wake']['kind'], string> = {
  listener: '本机常驻的 pichat-listen 被 @ 时自动唤醒 agent。',
  webhook: '被 @ 时向这个地址发 POST，请求带签名。',
  none: '不主动唤醒，agent 自己定期查看。',
}

function WakeEditor({ a }: { a: Agent }) {
  const [kind, setKind] = useState<Agent['wake']['kind']>(a.wake?.kind || 'listener')
  const [url, setUrl] = useState(a.wake?.url || '')
  const [secret, setSecret] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    setKind(a.wake?.kind || 'listener')
    setUrl(a.wake?.url || '')
  }, [a.wake?.kind, a.wake?.url])
  const dirty = kind !== (a.wake?.kind || 'listener') || (kind === 'webhook' && url !== (a.wake?.url || ''))

  const save = async () => {
    if (kind === 'webhook' && !/^https?:\/\//.test(url)) return notify('webhook 地址必须是 http(s)')
    setBusy(true)
    try {
      const r = await api<{ secret?: string }>('PUT', `/agents/${a.id}/wake`, kind === 'webhook' ? { kind, url } : { kind })
      setSecret(r.secret || '')
      await loadAgents()
      notify('唤醒方式已保存')
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Row label="唤醒方式" hint={WAKE_HINT[kind]}>
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <Segmented
          value={kind}
          onValueChange={(v) => setKind(v as Agent['wake']['kind'])}
          options={[
            { value: 'listener', label: '本机常驻', hint: 'listener' },
            { value: 'webhook', label: 'Webhook' },
            { value: 'none', label: '不唤醒' },
          ]}
        />
        {kind === 'webhook' && (
          <Input
            placeholder="https://..."
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            className="h-7 bg-background font-mono text-[12.5px] sm:flex-1"
            aria-label="webhook 地址"
          />
        )}
        {(dirty || kind === 'webhook') && (
          <Button size="sm" variant={dirty ? 'default' : 'outline'} disabled={busy} onClick={save}>
            {kind === 'webhook' && !dirty ? '重新生成密钥' : '保存'}
          </Button>
        )}
      </div>
      {(a.wake?.failStreak > 0 || a.wake?.lastError) && (
        <p className="text-[12px] text-destructive">
          {a.wake.failStreak > 0 && `连续失败 ${a.wake.failStreak} 次`}
          {a.wake.failStreak > 0 && a.wake.lastError && '：'}
          {a.wake.lastError}
        </p>
      )}
      {secret && (
        <Alert variant="warning" className="p-3" onOpenChange={(o) => !o && setSecret('')}>
          <AlertIcon />
          <AlertContent className="flex min-w-0 flex-1 flex-col gap-2">
            <AlertDescription className="text-[12.5px]">webhook 密钥只显示这一次，请现在复制保存。</AlertDescription>
            <CopyField value={secret} />
          </AlertContent>
        </Alert>
      )}
    </Row>
  )
}

/** Listener runtime: where it runs, its one main session, how to attach a terminal, and a reset. */
function Runtime({ a, state }: { a: Agent; state: ReturnType<typeof agentStateOf> }) {
  const { ask, dialog } = useConfirm()
  const [busy, setBusy] = useState(false)
  const rt = a.runtime
  const main = rt?.session || ''
  const localName = rt?.name || ''
  const attachCmd = `node ~/.pichat/pichat-listen.mjs attach --name ${localName || '<本地名>'}`
  const sessions = a.sessions ?? []
  const notice = rt?.notice || ''

  // The alert animates out first; the store update follows once the server cleared it.
  const dismissNotice = async () => {
    try {
      const r = await api<{ runtime: Agent['runtime'] }>('POST', `/agents/${a.id}/dismiss-notice`, {})
      setState((s) => ({ agents: s.agents?.map((x) => (x.id === a.id ? { ...x, runtime: r.runtime } : x)) ?? null }))
    } catch (e) {
      fail(e)
    }
  }

  const reset = async () => {
    const yes = await ask({
      title: `重置「${a.displayName}」的会话？`,
      description:
        '下次被唤醒时，listener 会开一个全新的主会话，agent 会丢失之前的全部上下文（对话记忆、读过的文件、做到一半的计划）。旧会话的记录还在本机，但不再被使用。',
      confirmText: '重置会话',
      destructive: true,
    })
    if (!yes) return
    setBusy(true)
    try {
      const r = await api<{ delivered?: boolean }>('POST', `/agents/${a.id}/reset-session`, {})
      if (r.delivered === false) notify('listener 不在线，没有重置。等它上线后再点一次', 6000)
      else notify('已通知 listener，下次运行会用新会话')
      loadAgents().catch(() => {})
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
    }
  }

  const kv = (label: string, value: React.ReactNode) => (
    <div className="flex min-w-0 items-center gap-3 py-1.5 text-[13px]">
      <span className="w-20 shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 flex-1 truncate text-body">{value}</span>
    </div>
  )

  return (
    <Row
      label="运行状态"
      hint="listener 对这个 agent 只用一个主会话：所有群、私聊和档位共用同一段上下文。在终端里 attach 时，后台唤醒会排队，退出后再处理。"
    >
      {notice && (
        <Alert key={`${notice}-${rt?.noticeAt ?? ''}`} variant="warning" className="p-3">
          <AlertIcon />
          <AlertContent className="flex min-w-0 flex-1 flex-col gap-0.5">
            <AlertDescription className="text-[12.5px] break-words">{notice}</AlertDescription>
            {rt?.noticeAt ? <span className="text-[11.5px] text-muted-foreground tabular-nums">{fmtTime(rt.noticeAt)}</span> : null}
          </AlertContent>
          <AlertClose aria-label="知道了，清除提示" title="知道了" onClick={dismissNotice} />
        </Alert>
      )}
      <div className="flex flex-col divide-y divide-border rounded-control border border-border px-3">
        {kv('状态', <AgentStateChip state={state} terminal={terminalModeOf({ online: a.online, state: rt?.state })} className="text-[13px]" />)}
        {kv('主机', rt?.host || <span className="text-muted-foreground">未知</span>)}
        {kv('本地名', localName ? <span className="font-mono text-[12.5px]">{localName}</span> : <span className="text-muted-foreground">未知</span>)}
        {kv(
          '主会话',
          main ? (
            <button
              type="button"
              onClick={() => copy(main, '已复制会话 ID')}
              title="复制完整会话 ID"
              className="-mx-1 inline-flex max-w-full items-center gap-1.5 rounded-chip px-1 font-mono text-[12.5px] transition-colors hover:bg-accent"
            >
              <span className="truncate">{main}</span>
              <Copy className="size-3 shrink-0 text-muted-foreground" />
            </button>
          ) : (
            <span className="text-muted-foreground">还没有（第一次运行时创建）</span>
          ),
        )}
        {rt?.listener ? kv('listener', <ListenerVersion l={rt.listener} name={localName} />) : null}
        <WorkDirs dirs={rt?.dirs} />
        {rt?.at ? kv('更新于', fmtTime(rt.at)) : null}
      </div>
      <div className="flex flex-col gap-1.5">
        <span className="text-[12.5px] text-muted-foreground">在那台机器的终端里接管主会话：</span>
        <CopyField value={attachCmd} />
      </div>
      {sessions.length > 0 && (
        <div className="flex flex-col gap-1">
          <span className="text-[12.5px] text-muted-foreground">会话历史</span>
          <ul className="flex flex-col divide-y divide-border rounded-control border border-border">
            {sessions.map((x) => (
              <li key={x.session + x.startedAt} className="flex items-center gap-2 px-3 py-1.5 text-[12.5px]">
                <button
                  type="button"
                  onClick={() => copy(x.session, '已复制会话 ID')}
                  title={x.session}
                  className="font-mono text-body tabular-nums transition-colors hover:text-foreground"
                >
                  {x.session.slice(0, 8)}
                </button>
                {x.session === main && <span className="rounded-chip bg-tag px-1 text-[10.5px] leading-4 text-brand">当前</span>}
                <span className="min-w-0 flex-1 truncate text-muted-foreground">{x.host}</span>
                <span className="shrink-0 text-muted-foreground tabular-nums">{fmtTime(x.startedAt)}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      <div>
        <Button variant="outline" size="sm" disabled={busy} onClick={reset}>
          <RotateCcw />
          重置会话
        </Button>
      </div>
      {dialog}
    </Row>
  )
}

function DirValue({ dir }: { dir: string }) {
  return (
    <button
      type="button"
      onClick={() => copy(dir, '已复制目录')}
      title={`复制 ${dir}`}
      className="-mx-1 inline-flex max-w-full items-center gap-1.5 rounded-chip px-1 font-mono text-[12.5px] transition-colors hover:bg-accent"
    >
      <span className="truncate">{dir}</span>
      <Copy className="size-3 shrink-0 text-muted-foreground" />
    </button>
  )
}

/** Work dirs the listener reported in PUT /me/runtime (owner only). Older listeners send none. */
function WorkDirs({ dirs }: { dirs?: NonNullable<Agent['runtime']>['dirs'] }) {
  const groups = useStore((s) => s.groups)
  const row = (label: string, value: React.ReactNode) => (
    <div className="flex min-w-0 items-start gap-3 py-1.5 text-[13px]">
      <span className="w-20 shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 flex-1 text-body">{value}</span>
    </div>
  )
  if (!dirs) return row('工作目录', <span className="text-muted-foreground">未知（listener 版本较旧）</span>)
  const main = dirs.main || ''
  const def = dirs.default || ''
  const binds = Object.entries(dirs.binds ?? {}).filter(([, d]) => typeof d === 'string' && d)
  const groupName = (gid: string) => groups.find((g) => g.id === gid)?.name || gid
  return (
    <>
      {row('工作目录', main ? <DirValue dir={main} /> : <span className="text-muted-foreground">未知</span>)}
      {def && def !== main ? row('默认目录', <DirValue dir={def} />) : null}
      {binds.length > 0 &&
        row(
          '群绑定目录',
          <ul className="flex min-w-0 flex-col gap-1">
            {binds.map(([gid, d]) => (
              <li key={gid} className="flex min-w-0 items-center gap-2">
                <span className="max-w-[40%] shrink-0 truncate text-[12.5px] text-muted-foreground" title={gid}>
                  {groupName(gid)}
                </span>
                <span className="min-w-0 flex-1">
                  <DirValue dir={d} />
                </span>
              </li>
            ))}
          </ul>,
        )}
    </>
  )
}

function ListenerVersion({ l, name }: { l: NonNullable<NonNullable<Agent['runtime']>['listener']>; name: string }) {
  const title = `脚本 sha256 ${l.sha256}，来自服务端 ${l.version}`
  if (l.latest) return <span title={title}>最新</span>
  if (l.autoUpdate && l.skip)
    return (
      <span className="whitespace-normal break-all text-warning" title={title}>
        旧版本（不会自动更新：{l.skip === 'git' ? '脚本在开发目录（git 仓库）里' : '脚本文件或目录不可写'}）
      </span>
    )
  if (l.autoUpdate)
    return (
      <span className="text-warning" title={title}>
        旧版本（等待空闲后自动更新）
      </span>
    )
  return (
    <span className="whitespace-normal break-all text-warning" title={title}>
      旧版本（自动更新已关闭：
      <span className="font-mono text-[12px]">node ~/.pichat/pichat-listen.mjs config --name {name || '<本地名>'} --auto-update on</span>）
    </span>
  )
}

function Runs({ a }: { a: Agent }) {
  const groups = useStore((s) => s.groups)
  const runs = a.recentRuns ?? []
  const rows: TaskRow[] = runs.map((r, i) => ({
    key: `${r.createdAt}-${i}`,
    label: groups.find((g) => g.id === r.groupId)?.name || r.groupId || '（无群）',
    amount: fmtTime(r.createdAt),
    status: r.ok ? 'done' : 'failed',
    details: (r.detail || (r.ok ? '完成' : '失败'))
      .split('\n')
      .filter(Boolean)
      .slice(0, 8)
      .map((line) => ({ label: line, meta: '' })),
  }))
  return (
    <Row label={`最近运行${runs.length ? `（${runs.length}）` : ''}`}>
      {rows.length ? (
        <TaskRows variant="List" rows={rows} labels={{ completed: '完成', failed: '失败' }} className="max-w-none" />
      ) : (
        <p className="text-[12.5px] text-muted-foreground">暂无记录</p>
      )}
    </Row>
  )
}

