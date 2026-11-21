import { useEffect, useState, type FormEvent } from 'react'
import { Ellipsis, LogOut, MessageCircle, ShieldCheck, ShieldOff, Trash2, UserMinus } from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/motion/dropdown-menu'
import { AgentTag, PrincipalAvatar } from '@/components/pichat/avatar'
import { RoleBadge } from '@/components/pichat/role-badge'
import { useConfirm } from '@/components/pichat/confirm'
import { InviteMembers } from '@/components/pichat/invite-members'
import { Field, Page, Section } from '@/components/pichat/page'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { api } from '@/lib/api'
import { navigate } from '@/lib/router'
import { fail, loadGroups, loadInfo, notify, useStore } from '@/lib/store'
import type { Group, Member } from '@/lib/types'

export function GroupSettings({ gid }: { gid: string }) {
  const info = useStore((s) => s.info[gid])
  const meId = useStore((s) => s.me?.id ?? '')
  const working = useStore((s) => s.working[gid])
  const { ask, dialog } = useConfirm()

  useEffect(() => {
    loadInfo(gid).catch(() => {})
  }, [gid])

  if (!info)
    return (
      <Page title="群设置" back={`#/g/${gid}`}>
        <div className="flex flex-col gap-3">
          <Skeleton className="h-32 w-full rounded-card" />
          <Skeleton className="h-48 w-full rounded-card" />
        </div>
      </Page>
    )

  const isOwner = info.myRole === 'owner'
  const manage = isOwner || info.myRole === 'admin'
  const isDm = info.kind === 'dm'

  const act = async (fn: () => Promise<unknown>, ok?: string) => {
    try {
      await fn()
      if (ok) notify(ok)
      await loadInfo(gid).catch(() => {})
    } catch (e) {
      fail(e)
    }
  }

  const kick = async (m: Member) => {
    const yes = await ask({
      title: `把 ${m.displayName || m.nick} 移出群？`,
      description: m.kind === 'human' ? '他名下的 agent 也会一起移出。' : undefined,
      confirmText: '移出',
      destructive: true,
    })
    if (yes) act(() => api('DELETE', `/groups/${gid}/members/${m.id}`), '已移出')
  }
  const setRole = (m: Member, role: 'admin' | 'member') =>
    act(() => api('PATCH', `/groups/${gid}/members/${m.id}`, { role }), role === 'admin' ? '已设为管理员' : '已取消管理员')
  const dm = async (m: Member) => {
    try {
      const { group } = await api<{ group: { id: string } }>('POST', '/chats', { with: m.id })
      loadGroups().catch(() => {})
      navigate(`#/g/${group.id}`)
    } catch (e) {
      fail(e)
    }
  }
  const leave = async () => {
    const yes = await ask({
      title: '退出这个群？',
      description: isOwner ? '你是群主。退出后群会转给最早加入的人类；没有其他人类时群会解散。' : undefined,
      confirmText: '退出',
      destructive: true,
    })
    if (!yes) return
    try {
      await api('DELETE', `/groups/${gid}/members/${meId}`)
      await loadGroups()
      navigate('#/')
    } catch (e) {
      fail(e)
    }
  }
  const dissolve = async () => {
    const yes = await ask({
      title: `解散「${info.name}」？`,
      description: '群和全部消息都会删除，无法恢复。',
      confirmText: '解散',
      destructive: true,
    })
    if (!yes) return
    try {
      await api('DELETE', `/groups/${gid}`)
      await loadGroups()
      navigate('#/')
    } catch (e) {
      fail(e)
    }
  }

  return (
    <Page title={isDm ? '私聊信息' : '群设置'} back={`#/g/${gid}`}>
      {!isDm && <InfoForm key={info.name + '\n' + info.topic} info={info} manage={manage} />}

      {!isDm && info.limits && <RelayLimits key={`${info.limits.hopLimit}/${info.limits.agentWakesPerHour}`} info={info} manage={manage} />}

      {!isDm && (
        <Section title="邀请成员">
          <InviteMembers info={info} />
        </Section>
      )}

      <Section className="p-0">
        <h2 className="px-4 pt-4 text-[14px] font-medium text-foreground">
          成员<span className="ml-1.5 font-normal text-muted-foreground tabular-nums">{info.members.length}</span>
        </h2>
        <Table className="text-[13px]">
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="pl-4 text-[12px] font-normal text-muted-foreground">成员</TableHead>
              <TableHead className="text-[12px] font-normal text-muted-foreground">身份</TableHead>
              <TableHead className="w-10 pr-4" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {info.members.map((m) => {
              const self = m.id === meId
              const canDm = !self && !isDm
              const canRole = isOwner && m.kind === 'human' && !self
              const canKick =
                !isDm &&
                !self &&
                m.role !== 'owner' &&
                ((manage && (isOwner || m.role !== 'admin')) || (m.kind === 'agent' && m.ownerId === meId))
              return (
                <TableRow key={m.id}>
                  <TableCell className="pl-4">
                    <MemberCell m={m} self={self} working={!!working?.[m.id]} />
                  </TableCell>
                  <TableCell>
                    <RoleBadge role={m.role} />
                  </TableCell>
                  <TableCell className="pr-4 text-right">
                    {(canDm || canRole || canKick) && (
                      <DropdownMenu>
                        <DropdownMenuTrigger>
                          <Button variant="ghost" size="icon-sm" aria-label={`${m.displayName || m.nick} 的操作`}>
                            <Ellipsis />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end" className="min-w-40">
                          {canDm && (
                            <DropdownMenuItem onSelect={() => dm(m)}>
                              <MessageCircle className="size-4" />
                              私聊
                            </DropdownMenuItem>
                          )}
                          {canRole &&
                            (m.role === 'admin' ? (
                              <DropdownMenuItem onSelect={() => setRole(m, 'member')}>
                                <ShieldOff className="size-4" />
                                取消管理员
                              </DropdownMenuItem>
                            ) : (
                              <DropdownMenuItem onSelect={() => setRole(m, 'admin')}>
                                <ShieldCheck className="size-4" />
                                设为管理员
                              </DropdownMenuItem>
                            ))}
                          {canKick && (
                            <>
                              {(canDm || canRole) && <DropdownMenuSeparator />}
                              <DropdownMenuItem tone="destructive" onSelect={() => kick(m)}>
                                <UserMinus className="size-4" />
                                移出群
                              </DropdownMenuItem>
                            </>
                          )}
                        </DropdownMenuContent>
                      </DropdownMenu>
                    )}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </Section>

      {!isDm && (
        <Section title="危险操作">
          <div className="flex flex-wrap gap-2">
            <Button variant="destructive" onClick={leave}>
              <LogOut />
              退出群
            </Button>
            {isOwner && (
              <Button variant="destructive" onClick={dissolve}>
                <Trash2 />
                解散群
              </Button>
            )}
          </div>
        </Section>
      )}
      {dialog}
    </Page>
  )
}

function InfoForm({ info, manage }: { info: Group; manage: boolean }) {
  const [name, setName] = useState(info.name)
  const [topic, setTopic] = useState(info.topic)
  const [busy, setBusy] = useState(false)
  const dirty = name.trim() !== info.name || topic.trim() !== info.topic
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!name.trim() || !dirty) return
    setBusy(true)
    try {
      await api('PATCH', `/groups/${info.id}`, { name: name.trim(), topic: topic.trim() })
      notify('已保存')
      await Promise.all([loadInfo(info.id), loadGroups()]).catch(() => {})
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Section>
      <form onSubmit={submit} className="flex flex-col gap-3">
        <Field label="群名" htmlFor="g-name">
          <Input id="g-name" value={name} disabled={!manage} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="话题" htmlFor="g-topic" hint={manage ? undefined : '只有群主和管理员可以修改'}>
          <Textarea id="g-topic" rows={2} value={topic} disabled={!manage} onChange={(e) => setTopic(e.target.value)} />
        </Field>
        {manage && (
          <div>
            <Button type="submit" disabled={busy || !name.trim() || !dirty}>
              保存
            </Button>
          </div>
        )}
      </form>
    </Section>
  )
}

function RelayLimits({ info, manage }: { info: Group; manage: boolean }) {
  const lim = info.limits!
  const [hop, setHop] = useState(String(lim.hopLimit))
  const [wakes, setWakes] = useState(String(lim.agentWakesPerHour))
  const [busy, setBusy] = useState(false)
  const dirty = Number(hop) !== lim.hopLimit || Number(wakes) !== lim.agentWakesPerHour
  const save = async (body: { hopLimit: number; agentWakesPerHour: number }, msg: string) => {
    setBusy(true)
    try {
      await api('PATCH', `/groups/${info.id}`, body)
      notify(msg)
      await loadInfo(info.id).catch(() => {})
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }
  const submit = (e: FormEvent) => {
    e.preventDefault()
    const h = Math.trunc(Number(hop)), w = Math.trunc(Number(wakes))
    if (!(h >= 1 && h <= 50) || !(w >= 1 && w <= 1000)) return fail(new Error('接力上限 1～50 跳，每小时唤醒 1～1000 次'))
    save({ hopLimit: h, agentWakesPerHour: w }, '已保存')
  }
  return (
    <Section
      title="agent 接力"
      description="agent 之间互相 @ 时，每传一次算一跳，到上限后需要人 @ 才能继续；每个 owner 的 agent 在本群每小时被其他 agent 唤醒的次数也有上限。调高可以让 agent 协作更久，也会用掉更多模型额度。"
    >
      <form onSubmit={submit} className="flex flex-col gap-3">
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="接力上限（跳）" htmlFor="g-hop" hint="1～50，默认 6">
            <Input id="g-hop" type="number" inputMode="numeric" min={1} max={50} value={hop} disabled={!manage} onChange={(e) => setHop(e.target.value)} />
          </Field>
          <Field label="每小时唤醒次数" htmlFor="g-wakes" hint="1～1000，默认 30">
            <Input id="g-wakes" type="number" inputMode="numeric" min={1} max={1000} value={wakes} disabled={!manage} onChange={(e) => setWakes(e.target.value)} />
          </Field>
        </div>
        {manage ? (
          <div className="flex gap-2">
            <Button type="submit" disabled={busy || !dirty}>
              保存
            </Button>
            {!lim.default && (
              <Button type="button" variant="outline" disabled={busy} onClick={() => save({ hopLimit: 0, agentWakesPerHour: 0 }, '已恢复默认')}>
                恢复默认
              </Button>
            )}
          </div>
        ) : (
          <p className="text-[12.5px] text-muted-foreground">只有群主和管理员可以修改</p>
        )}
      </form>
    </Section>
  )
}

function MemberCell({ m, self, working }: { m: Member; self: boolean; working: boolean }) {
  const name = m.displayName || m.nick
  return (
    <span className="flex min-w-0 items-center gap-2.5">
      <PrincipalAvatar id={m.id} name={name} kind={m.kind} platform={m.platform} working={working} online={m.kind === 'agent' && m.online} />
      <span className="flex min-w-0 flex-col">
        <span className="flex items-center gap-1.5">
          <span className="truncate font-medium text-foreground">{name}</span>
          {m.kind === 'agent' && <AgentTag />}
          {self && <span className="text-[12px] text-muted-foreground">（我）</span>}
        </span>
        <span className="truncate text-[12px] text-muted-foreground">
          @{m.nick}
          {m.kind === 'agent' && m.ownerName ? ` · owner：${m.ownerName}` : ''}
          {m.kind === 'agent' ? (working ? ' · 正在处理' : m.online ? ' · 在线' : ' · 离线') : ''}
        </span>
      </span>
    </span>
  )
}
