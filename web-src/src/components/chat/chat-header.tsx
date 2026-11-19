import { ChevronLeft, Settings, Users } from 'lucide-react'
import { AvatarGroup } from '@/components/motion/avatar-group'
import { PrincipalAvatar } from '@/components/pichat/avatar'
import { Button } from '@/components/ui/button'
import type { Group, GroupSummary } from '@/lib/types'
import { memberName } from './util'

export function ChatHeader({
  gid,
  info,
  summary,
  onMembers,
}: {
  gid: string
  info?: Group
  summary?: GroupSummary
  onMembers: () => void
}) {
  const isDm = (info?.kind ?? summary?.kind) === 'dm'
  const name = summary?.name || info?.name || (isDm ? '私聊' : '')
  const sub = isDm ? (summary?.peer?.kind === 'agent' ? 'agent 私聊' : '私聊') : info?.topic
  const byId = new Map(info?.members.map((m) => [m.id, m]))
  const people = [...(info?.members ?? [])]
    .sort((a, b) => (a.kind === b.kind ? 0 : a.kind === 'human' ? -1 : 1))
    .map((m) => ({ id: m.id, name: memberName(m) }))

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
                  <PrincipalAvatar
                    id={m.id}
                    name={p.name}
                    kind={m.kind}
                    platform={m.platform}
                    size="sm"
                    className="[&>span:first-child]:ring-2 [&>span:first-child]:ring-card"
                  />
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
            <Button asChild variant="ghost" size="icon-sm" className="text-muted-foreground">
              <a href={`#/g/${gid}/settings`} aria-label="群设置" title="群设置">
                <Settings />
              </a>
            </Button>
          )}
        </>
      )}
    </header>
  )
}
