import { useState } from 'react'
import { ChevronLeft, Settings, UserPlus, Users } from 'lucide-react'
import { AvatarGroup } from '@/components/motion/avatar-group'
import { PrincipalAvatar } from '@/components/pichat/avatar'
import { agentStateText } from '@/components/pichat/agent-state'
import { ProfileTrigger } from '@/components/pichat/profile-card'
import { InviteMembers } from '@/components/pichat/invite-members'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { agentStateOf, terminalModeOf } from '@/lib/store'
import type { Group, GroupSummary } from '@/lib/types'
import { memberName } from './util'

export function ChatHeader({
  gid,
  info,
  summary,
  onMembers,
  workingIds,
}: {
  gid: string
  info?: Group
  summary?: GroupSummary
  onMembers: () => void
  workingIds?: Set<string>
}) {
  const [inviteOpen, setInviteOpen] = useState(false)
  const isDm = (info?.kind ?? summary?.kind) === 'dm'
  const name = summary?.name || info?.name || (isDm ? '私聊' : '')
  const sub = isDm ? (summary?.peer?.kind === 'agent' ? 'agent 私聊' : '私聊') : info?.topic
  const byId = new Map(info?.members.map((m) => [m.id, m]))
  const people = [...(info?.members ?? [])]
    .sort((a, b) => (a.kind === b.kind ? 0 : a.kind === 'human' ? -1 : 1))
    // The avatar tooltip shows the name; agents also show their listener state.
    .map((m) => ({
      id: m.id,
      name: m.kind === 'agent' ? `${memberName(m)} · ${agentStateText(agentStateOf(m, workingIds?.has(m.id)), terminalModeOf(m))}` : memberName(m),
    }))

  return (
    <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-2 min-[900px]:px-4">
      <Button asChild variant="ghost" size="icon-sm" className="text-muted-foreground min-[900px]:hidden">
        <a href="#/" aria-label="返回">
          <ChevronLeft />
        </a>
      </Button>
      <div className="flex min-w-0 flex-1 items-baseline gap-2">
        <h1 className="truncate text-[15px] font-medium text-foreground">{name || '…'}</h1>
        {sub && <span className="hidden truncate text-[12.5px] text-muted-foreground sm:inline">{sub}</span>}
      </div>
      {info && (
        <>
          <div className="hidden items-center gap-1 sm:flex">
            <AvatarGroup
              members={people}
              max={4}
              size="sm"
              overlap={5}
              label="群成员"
              renderAvatar={(p) => {
                const m = byId.get(p.id)!
                return (
                  <ProfileTrigger
                    seed={{ id: m.id, kind: m.kind, name: memberName(m), platform: m.platform, ownerName: m.ownerName }}
                    className="block rounded-[7px]"
                  >
                    <PrincipalAvatar
                      id={m.id}
                      name={memberName(m)}
                      kind={m.kind}
                      platform={m.platform}
                      size="sm"
                      className="[&>span:first-child]:ring-2 [&>span:first-child]:ring-card"
                    />
                  </ProfileTrigger>
                )
              }}
            />
            <Button variant="ghost" size="xs" className="text-muted-foreground tabular-nums" aria-label={`群成员（${info.members.length} 人）`} onClick={onMembers}>
              <Users />
              {info.members.length}
            </Button>
          </div>
          <Button variant="ghost" size="icon-sm" className="text-muted-foreground sm:hidden" aria-label="群成员" onClick={onMembers}>
            <Users />
          </Button>
          {info.kind !== 'dm' && (
            <Button variant="ghost" size="icon-sm" className="text-muted-foreground" aria-label="邀请成员" title="邀请成员" onClick={() => setInviteOpen(true)}>
              <UserPlus />
            </Button>
          )}
          {info.kind !== 'dm' && (
            <Button asChild variant="ghost" size="icon-sm" className="text-muted-foreground">
              <a href={`#/g/${gid}/settings`} aria-label="群设置" title="群设置">
                <Settings />
              </a>
            </Button>
          )}
          {info.kind !== 'dm' && (
            <Dialog open={inviteOpen} onOpenChange={setInviteOpen}>
              <DialogContent className="gap-0 p-0 sm:max-w-md">
                <DialogHeader className="gap-1 px-5 pt-5">
                  <DialogTitle className="font-serif text-[20px] font-normal text-foreground">邀请成员</DialogTitle>
                  <DialogDescription className="text-[13px]">
                    {info.myRole === 'owner' || info.myRole === 'admin' ? '拉已有用户、你的 agent 进群，或者生成邀请链接。' : '把你的 agent 拉进群。邀请其他人请找群主或管理员。'}
                  </DialogDescription>
                </DialogHeader>
                <div className="px-5 pt-4 pb-5">
                  <InviteMembers info={info} />
                </div>
              </DialogContent>
            </Dialog>
          )}
        </>
      )}
    </header>
  )
}
