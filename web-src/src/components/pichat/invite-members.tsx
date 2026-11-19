import { useEffect, useMemo, useState } from 'react'
import { Link, Plus, Search } from 'lucide-react'
import { PrincipalAvatar } from '@/components/pichat/avatar'
import { CopyField } from '@/components/pichat/copy-field'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { api } from '@/lib/api'
import { PLATFORM } from '@/lib/format'
import { fail, loadAgents, loadInfo, notify, useStore } from '@/lib/store'
import type { Group } from '@/lib/types'

interface DirUser {
  id: string
  name: string
  displayName: string
  inGroup?: boolean
}

/**
 * The 邀请成员 area: 「已有用户」 and 「邀请链接」 for owner/admin, 「我的 agent」 for everyone.
 * Used in group settings and in the chat header's 邀请 dialog. Not for DMs.
 */
export function InviteMembers({ info }: { info: Group }) {
  const manage = info.myRole === 'owner' || info.myRole === 'admin'
  // Plain members only get the agents list; a tab bar with one tab would look like a stray button.
  if (!manage) return <AgentsTab info={info} />
  return (
    <Tabs defaultValue="users" className="gap-3">
      <TabsList className="w-full sm:w-fit">
        <TabsTrigger value="users">已有用户</TabsTrigger>
        <TabsTrigger value="agents">我的 agent</TabsTrigger>
        <TabsTrigger value="link">邀请链接</TabsTrigger>
      </TabsList>
      <TabsContent value="users">
        <UsersTab info={info} />
      </TabsContent>
      <TabsContent value="agents">
        <AgentsTab info={info} />
      </TabsContent>
      <TabsContent value="link">
        <LinkTab gid={info.id} />
      </TabsContent>
    </Tabs>
  )
}

function UsersTab({ info }: { info: Group }) {
  const gid = info.id
  const [users, setUsers] = useState<DirUser[] | null>(null)
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState('')
  const [err, setErr] = useState(false)
  const [retry, setRetry] = useState(0)
  // Member list changes (SSE `group` reloads info) refresh the directory too.
  const memberKey = info.members.map((m) => m.id).join(',')
  useEffect(() => {
    let live = true
    setErr(false)
    api<{ users: DirUser[] }>('GET', `/users?groupId=${encodeURIComponent(gid)}`).then(
      (r) => {
        if (live) setUsers(r.users)
      },
      (e) => {
        if (!live) return
        fail(e)
        setErr(true)
      },
    )
    return () => {
      live = false
    }
  }, [gid, memberKey, retry])

  const candidates = useMemo(() => (users ?? []).filter((u) => !u.inGroup), [users])
  const shown = useMemo(() => {
    const s = q.trim().toLowerCase()
    if (!s) return candidates
    return candidates.filter((u) => u.displayName.toLowerCase().includes(s) || u.name.toLowerCase().includes(s))
  }, [candidates, q])

  const add = async (u: DirUser) => {
    setBusy(u.id)
    try {
      await api('POST', `/groups/${gid}/members`, { userId: u.id })
      notify(`已把 ${u.displayName} 拉进群`)
      setUsers((list) => list?.map((x) => (x.id === u.id ? { ...x, inGroup: true } : x)) ?? null)
      loadInfo(gid).catch(() => {})
    } catch (e) {
      fail(e)
    } finally {
      setBusy('')
    }
  }

  if (err && users == null)
    return (
      <p className="flex items-center gap-2 text-[13px] text-muted-foreground">
        用户列表加载失败。
        <Button size="sm" variant="outline" onClick={() => setRetry((n) => n + 1)}>
          重试
        </Button>
      </p>
    )
  if (users == null) return <Skeleton className="h-24 w-full" />
  if (candidates.length === 0)
    return <p className="text-[13px] text-muted-foreground">实例里的其他用户都已在群里，或者用邀请链接拉新朋友。</p>
  return (
    <div className="flex flex-col gap-2">
      <div className="relative">
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="搜索名字或用户名" aria-label="搜索用户" className="pl-8" />
      </div>
      {shown.length === 0 ? (
        <p className="px-2 py-3 text-[13px] text-muted-foreground">没有匹配的用户。</p>
      ) : (
        <div className="-mx-2 flex max-h-72 flex-col overflow-y-auto">
          {shown.map((u) => (
            <div key={u.id} className="flex items-center gap-2.5 rounded-control px-2 py-1.5 hover:bg-accent">
              <PrincipalAvatar id={u.id} name={u.displayName} kind="human" />
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="truncate text-[13.5px] font-medium text-foreground">{u.displayName}</span>
                <span className="truncate text-[12px] text-muted-foreground">@{u.name}</span>
              </span>
              <Button size="sm" variant="outline" disabled={busy === u.id} onClick={() => add(u)}>
                <Plus />
                拉进群
              </Button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function AgentsTab({ info }: { info: Group }) {
  const gid = info.id
  const agents = useStore((s) => s.agents)
  const [busy, setBusy] = useState('')
  useEffect(() => {
    loadAgents().catch(() => {})
  }, [])
  const memberIds = new Set(info.members.map((m) => m.id))
  const addable = (agents ?? []).filter((a) => !memberIds.has(a.id))
  const add = async (id: string, name: string) => {
    setBusy(id)
    try {
      await api('POST', `/groups/${gid}/members`, { agentId: id })
      notify(`已把 ${name} 拉进群`)
      await loadInfo(gid).catch(() => {})
    } catch (e) {
      fail(e)
    } finally {
      setBusy('')
    }
  }
  if (agents == null) return <Skeleton className="h-9 w-full" />
  if (addable.length === 0)
    return (
      <p className="text-[13px] text-muted-foreground">
        没有可以拉进来的 agent。
        <a href="#/agents" className="text-brand hover:underline">
          去添加
        </a>
      </p>
    )
  return (
    <div className="-mx-2 flex max-h-72 flex-col overflow-y-auto">
      {addable.map((a) => (
        <div key={a.id} className="flex items-center gap-2.5 rounded-control px-2 py-1.5 hover:bg-accent">
          <PrincipalAvatar id={a.id} name={a.displayName} kind="agent" platform={a.platform} online={a.online} />
          <span className="flex min-w-0 flex-1 flex-col">
            <span className="truncate text-[13.5px] font-medium text-foreground">{a.displayName}</span>
            <span className="text-[12px] text-muted-foreground">
              {PLATFORM[a.platform ?? ''] || a.platform || '未知平台'} · {a.online ? '在线' : '离线'}
            </span>
          </span>
          <Button size="sm" variant="outline" disabled={busy === a.id} onClick={() => add(a.id, a.displayName)}>
            <Plus />
            拉进群
          </Button>
        </div>
      ))}
    </div>
  )
}

function LinkTab({ gid }: { gid: string }) {
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const gen = async () => {
    setBusy(true)
    try {
      const r = await api<{ url: string }>('POST', `/groups/${gid}/invite`, {})
      setUrl(r.url)
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="flex flex-col gap-3">
      <p className="text-[13px] text-muted-foreground">7 天有效，最多 20 次。生成新链接会作废旧链接。没有账号的人可以用它注册并入群。</p>
      {url && <CopyField value={url} />}
      <div>
        <Button variant={url ? 'outline' : 'default'} onClick={gen} disabled={busy}>
          <Link />
          {url ? '重新生成' : '生成邀请链接'}
        </Button>
      </div>
    </div>
  )
}
