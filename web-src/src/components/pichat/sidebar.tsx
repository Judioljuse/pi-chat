import { useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { Bot, ChevronDown, ChevronsUpDown, LogOut, MessagesSquare, Plus, Search, Settings2, Shield, UserRound } from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/motion/dropdown-menu'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { fmtShort } from '@/lib/format'
import { plainText } from '@/lib/markdown'
import { navigate } from '@/lib/router'
import { fail, loadAgents, loadGroups, notify, useStore, workingAgentIds } from '@/lib/store'
import type { GroupSummary } from '@/lib/types'
import { cn } from '@/lib/utils'
import { PLATFORM } from '@/lib/format'
import { PrincipalAvatar } from './avatar'
import { NotifyBanner } from './notify-banner'

export async function logout() {
  try {
    await api('POST', '/auth/logout', {})
  } catch {
    /* ignore */
  }
  window.dispatchEvent(new Event('pichat:logout'))
}

export function Sidebar({ className }: { className?: string }) {
  const me = useStore((s) => s.me)
  const groups = useStore((s) => s.groups)
  const loaded = useStore((s) => s.groupsLoaded)
  const route = useStore((s) => s.route)
  const working = useStore((s) => s.working)
  const [q, setQ] = useState('')
  const [creating, setCreating] = useState(false)

  const current = route.name === 'chat' || route.name === 'settings' ? route.gid : undefined
  const filtered = useMemo(() => {
    const k = q.trim().toLowerCase()
    if (!k) return groups
    return groups.filter((g) => g.name.toLowerCase().includes(k) || g.topic.toLowerCase().includes(k))
  }, [groups, q])
  const rooms = filtered.filter((g) => g.kind === 'group')
  const dms = filtered.filter((g) => g.kind === 'dm')
  const agents = useStore((s) => s.agents)
  const busyAgents = useMemo(() => workingAgentIds({ working }), [working])
  const isWorking = (g: GroupSummary) =>
    g.peer ? g.peer.kind === 'agent' && busyAgents.has(g.peer.id) : Object.keys(working[g.id] ?? {}).length > 0
  const platformOf = (id: string) => agents?.find((a) => a.id === id)?.platform
  const onlineOf = (id: string) => agents?.find((a) => a.id === id)?.online
  const search = useRef<HTMLInputElement>(null)
  const mac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        search.current?.focus()
      }
    }
    const onNew = () => setCreating(true)
    window.addEventListener('keydown', onKey)
    window.addEventListener('pichat:new-group', onNew)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('pichat:new-group', onNew)
    }
  }, [])

  return (
    <aside className={cn('flex h-full min-h-0 w-full flex-col bg-sidebar text-sidebar-foreground', className)}>
      <div className="flex h-12 shrink-0 items-center gap-2 px-3">
        <span className="grid size-6 place-items-center rounded-[7px] bg-foreground font-serif text-[14px] text-background italic">π</span>
        <span className="flex-1 font-serif text-[17px] tracking-tight">pichat</span>
        <Button variant="ghost" size="icon-sm" aria-label="新建群" title="新建群" onClick={() => setCreating(true)}>
          <Plus />
        </Button>
      </div>

      <div className="flex flex-col gap-px px-2 pb-1">
        <label className="mb-1.5 flex h-8 items-center gap-2 rounded-control border border-border bg-card px-2 text-muted-foreground transition-colors focus-within:border-input">
          <Search className="size-4 shrink-0" />
          <input
            ref={search}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => e.key === 'Escape' && (setQ(''), e.currentTarget.blur())}
            placeholder="搜索会话"
            aria-label="搜索会话"
            className="h-full min-w-0 flex-1 bg-transparent text-[13px] text-foreground outline-none placeholder:text-muted-foreground"
          />
          <kbd className="pointer-coarse:hidden rounded-[4px] border border-border bg-background px-1 font-sans text-[10.5px] leading-4 text-muted-foreground">
            {mac ? '⌘K' : 'Ctrl K'}
          </kbd>
        </label>
        <NavItem href="#/" icon={<MessagesSquare />} active={route.name === 'home' || route.name === 'chat'}>
          消息
        </NavItem>
        <NavItem href="#/agents" icon={<Bot />} active={route.name === 'agents'}>
          我的 agent
        </NavItem>
        {me?.isAdmin && (
          <NavItem href="#/admin" icon={<Shield />} active={route.name === 'admin'}>
            管理
          </NavItem>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
        {!loaded ? (
          <div className="flex flex-col gap-2 px-1 pt-4">
            {[0, 1, 2].map((i) => (
              <div key={i} className="flex items-center gap-2.5 p-1.5">
                <Skeleton className="size-8 rounded-[9px]" />
                <div className="flex flex-1 flex-col gap-1.5">
                  <Skeleton className="h-3 w-24" />
                  <Skeleton className="h-3 w-36" />
                </div>
              </div>
            ))}
          </div>
        ) : groups.length === 0 ? (
          <p className="px-3 pt-6 text-[13px] text-muted-foreground">还没有群。点右上角 + 建一个。</p>
        ) : (
          <>
            <ListSection title="群聊" items={rooms} current={current} isWorking={isWorking} platformOf={platformOf} onlineOf={onlineOf} onAdd={() => setCreating(true)} />
            <ListSection title="私聊" items={dms} current={current} isWorking={isWorking} platformOf={platformOf} onlineOf={onlineOf} />
            {q && !rooms.length && !dms.length && <p className="px-3 pt-6 text-[13px] text-muted-foreground">没有匹配的会话</p>}
          </>
        )}
      </div>

      <NotifyBanner />
      {me && (
        <div className="shrink-0 border-t border-sidebar-border p-2">
          <DropdownMenu>
            <DropdownMenuTrigger>
              <button
                type="button"
                className={cn(
                  'flex w-full items-center gap-2.5 rounded-control px-2 py-1.5 text-left transition-colors hover:bg-sidebar-accent',
                  route.name === 'me' && 'bg-sidebar-accent',
                )}
              >
                <PrincipalAvatar id={me.id} name={me.displayName} kind="human" size="sm" />
                <span className="min-w-0 flex-1 truncate text-[13.5px] font-medium">{me.displayName}</span>
                <ChevronsUpDown className="size-3.5 text-muted-foreground" />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent side="top" className="min-w-56">
              <DropdownMenuLabel>
                {me.name}
                {me.isAdmin ? '（管理员）' : ''}
              </DropdownMenuLabel>
              <DropdownMenuItem onSelect={() => navigate('#/me')}>
                <UserRound className="size-4" />
                我的账号
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => navigate('#/agents')}>
                <Settings2 className="size-4" />
                管理我的 agent
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem tone="destructive" onSelect={logout}>
                <LogOut className="size-4" />
                退出登录
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      )}

      <NewGroupDialog open={creating} onOpenChange={setCreating} />
    </aside>
  )
}

function NavItem({ href, icon, active, children }: { href: string; icon: ReactNode; active?: boolean; children: ReactNode }) {
  return (
    <a
      href={href}
      className={cn(
        'flex h-8 items-center gap-2 rounded-control px-2 text-[13.5px] text-body transition-colors hover:bg-sidebar-accent hover:text-foreground [&_svg]:size-4 [&_svg]:shrink-0',
        active && 'bg-sidebar-accent text-foreground',
      )}
    >
      {icon}
      {children}
    </a>
  )
}

function ListSection({
  title,
  items,
  current,
  isWorking,
  platformOf,
  onlineOf,
  onAdd,
}: {
  title: string
  items: GroupSummary[]
  current?: string
  isWorking: (g: GroupSummary) => boolean
  platformOf: (id: string) => string | null | undefined
  onlineOf: (id: string) => boolean | undefined
  onAdd?: () => void
}) {
  const [open, setOpen] = useState(true)
  if (!items.length) return null
  const unread = items.reduce((n, g) => n + (g.unread || 0), 0)
  return (
    <div className="pt-4">
      <div className="group/section flex h-6 items-center gap-1 px-2 text-[12px] text-muted-foreground">
        <button
          type="button"
          onClick={() => setOpen(!open)}
          aria-expanded={open}
          className="flex items-center gap-1 rounded-chip transition-colors hover:text-foreground"
        >
          {title}
          <ChevronDown className={cn('size-3 transition-transform duration-200 ease-(--ease-out)', !open && '-rotate-90')} />
        </button>
        {!open && unread > 0 && <span className="text-[11px] text-brand tabular-nums">{unread > 99 ? '99+' : unread}</span>}
        <span className="flex-1" />
        {onAdd && (
          <button
            type="button"
            onClick={onAdd}
            aria-label="新建群"
            title="新建群"
            className="grid size-5 place-items-center rounded-chip opacity-0 transition-opacity group-hover/section:opacity-100 hover:bg-sidebar-accent hover:text-foreground focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
          >
            <Plus className="size-3.5" />
          </button>
        )}
      </div>
      {open && (
        <nav className="flex flex-col gap-px animate-fade-in">
          {items.map((g) => (
            <ConversationRow key={g.id} g={g} active={g.id === current} working={isWorking(g)} platformOf={platformOf} onlineOf={onlineOf} />
          ))}
        </nav>
      )}
    </div>
  )
}

function ConversationRow({
  g,
  active,
  working,
  platformOf,
  onlineOf,
}: {
  g: GroupSummary
  active: boolean
  working: boolean
  platformOf: (id: string) => string | null | undefined
  onlineOf: (id: string) => boolean | undefined
}) {
  const name = g.name || (g.kind === 'dm' ? '私聊' : '未命名群')
  const last = g.lastMessage
  const preview = last ? `${g.kind === 'dm' ? '' : last.authorName + '：'}${plainText(last.text)}` : g.topic
  const hot = g.unread > 0 || g.mentionsMe > 0
  const peer = g.peer
  return (
    <a
      href={`#/g/${g.id}`}
      title={g.topic || undefined}
      aria-current={active ? 'page' : undefined}
      className={cn(
        'flex items-center gap-2.5 rounded-control px-2 py-1.5 transition-colors hover:bg-sidebar-accent',
        active && 'bg-sidebar-accent',
      )}
    >
      {peer ? (
        <PrincipalAvatar id={peer.id} name={peer.displayName || name} kind={peer.kind} platform={peer.platform ?? platformOf(peer.id)}
          working={working}
          online={peer.kind === 'agent' && onlineOf(peer.id)}
        />
      ) : (
        <PrincipalAvatar id={g.id} name={name} kind="human" className="[&>span:first-child]:rounded-[9px]" working={working} />
      )}
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="flex items-baseline gap-2">
          <span className={cn('min-w-0 flex-1 truncate text-[13.5px] text-foreground', hot ? 'font-medium' : 'font-normal')}>{name}</span>
          <span className="shrink-0 text-[11px] text-muted-foreground tabular-nums">{fmtShort(last?.createdAt)}</span>
        </span>
        <span className="flex items-center gap-1.5">
          <span className="min-w-0 flex-1 truncate text-[12.5px] text-muted-foreground">{preview || ' '}</span>
          {g.mentionsMe > 0 && (
            <span className="shrink-0 rounded-full bg-mention-me-light px-1.5 text-[10.5px] leading-4 font-semibold text-mention-me">@我</span>
          )}
          {g.unread > 0 && (
            <span className="min-w-4 shrink-0 rounded-full bg-brand px-1 text-center text-[10.5px] leading-4 font-medium text-primary-foreground tabular-nums">
              {g.unread > 99 ? '99+' : g.unread}
            </span>
          )}
        </span>
      </span>
    </a>
  )
}

function NewGroupDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const agents = useStore((s) => s.agents)
  const [name, setName] = useState('')
  const [topic, setTopic] = useState('')
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (open && agents == null) loadAgents().catch(() => {})
  }, [open, agents])

  const toggle = (id: string, on: boolean) =>
    setPicked((p) => {
      const n = new Set(p)
      if (on) n.add(id)
      else n.delete(id)
      return n
    })

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!name.trim() || busy) return
    setBusy(true)
    try {
      const { group } = await api<{ group: { id: string } }>('POST', '/groups', { name: name.trim(), topic: topic.trim() })
      const results = await Promise.allSettled(
        [...picked].map((agentId) => api('POST', `/groups/${group.id}/members`, { agentId })),
      )
      const failed = results.filter((r) => r.status === 'rejected').length
      if (failed) notify(`有 ${failed} 个 agent 没能拉进群，可以在群设置里重试`)
      setName('')
      setTopic('')
      setPicked(new Set())
      onOpenChange(false)
      await loadGroups()
      navigate(`#/g/${group.id}`)
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-0 p-0 sm:max-w-md">
        <form onSubmit={submit} className="flex flex-col">
          <DialogHeader className="gap-1 px-5 pt-5">
            <DialogTitle className="font-serif text-[20px] font-normal text-foreground">新建群</DialogTitle>
            <DialogDescription className="text-[13px]">建好后可以生成邀请链接，邀请别人和他们的 agent。</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-3 px-5 py-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="ng-name" className="text-[13px] font-medium text-body">
                群名
              </Label>
              <Input id="ng-name" autoFocus value={name} maxLength={64} onChange={(e) => setName(e.target.value)} />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="ng-topic" className="text-[13px] font-medium text-body">
                话题<span className="ml-1 font-normal text-muted-foreground">（可选）</span>
              </Label>
              <Textarea
                id="ng-topic"
                rows={2}
                value={topic}
                onChange={(e) => setTopic(e.target.value)}
                placeholder="这个群用来做什么"
                className="min-h-0"
              />
            </div>
            {!!agents?.length && (
              <div className="flex flex-col gap-1.5">
                <span className="text-[13px] font-medium text-body">
                  拉我的 agent 进群<span className="ml-1 font-normal text-muted-foreground">（可选）</span>
                </span>
                <div className="flex max-h-44 flex-col gap-px overflow-y-auto rounded-control border border-border p-1">
                  {agents.map((a) => (
                    <label
                      key={a.id}
                      className="flex cursor-pointer items-center gap-2.5 rounded-chip px-2 py-1.5 transition-colors hover:bg-accent"
                    >
                      <Checkbox checked={picked.has(a.id)} onCheckedChange={(v) => toggle(a.id, v === true)} />
                      <PrincipalAvatar id={a.id} name={a.displayName} kind="agent" platform={a.platform} size="sm" online={a.online} />
                      <span className="min-w-0 flex-1 truncate text-[13px] text-foreground">{a.displayName}</span>
                      <span className="text-[12px] text-muted-foreground">{PLATFORM[a.platform ?? ''] || a.platform}</span>
                    </label>
                  ))}
                </div>
              </div>
            )}
          </div>
          <DialogFooter className="mx-0 mb-0 border-t border-border px-5 py-3">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" disabled={!name.trim() || busy}>
              创建
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
