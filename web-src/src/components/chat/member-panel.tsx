import { useState } from 'react'
import { MessageCircle, X } from 'lucide-react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { api } from '@/lib/api'
import { EASE_OUT } from '@/lib/ease'
import { navigate } from '@/lib/router'
import { fail } from '@/lib/store'
import type { Group, Member } from '@/lib/types'
import { MemberLine } from './composer'

const ORDER = { owner: 0, admin: 1, member: 2 } as const

function sortMembers(list: Member[]) {
  return [...list].sort(
    (a, b) =>
      ORDER[a.role] - ORDER[b.role] ||
      (a.kind === b.kind ? 0 : a.kind === 'human' ? -1 : 1) ||
      Number(!!b.online) - Number(!!a.online) ||
      (a.nick || '').localeCompare(b.nick || '', 'zh-CN'),
  )
}

export function MemberList({ info, meId, workingIds, onDone }: { info: Group; meId: string; workingIds: Set<string>; onDone?: () => void }) {
  const [busy, setBusy] = useState('')
  const startDm = async (pid: string) => {
    setBusy(pid)
    try {
      const { group } = await api<{ group: Group }>('POST', '/chats', { with: pid })
      onDone?.()
      navigate(`#/g/${group.id}`)
    } catch (e) {
      fail(e)
    } finally {
      setBusy('')
    }
  }
  const humans = sortMembers(info.members.filter((m) => m.kind === 'human'))
  const agents = sortMembers(info.members.filter((m) => m.kind === 'agent'))
  const section = (title: string, list: Member[]) =>
    list.length > 0 && (
      <div className="flex flex-col gap-0.5">
        <div className="px-2 pt-2 pb-1 text-[12px] font-medium text-muted-foreground">
          {title} <span className="tabular-nums">{list.length}</span>
        </div>
        {list.map((m) => (
          <div key={m.id} className="group/row flex min-h-12 items-center gap-2.5 rounded-control px-2 py-1.5 hover:bg-accent">
            <MemberLine m={m} working={workingIds.has(m.id)} stacked profile />
            {m.id !== meId && info.kind !== 'dm' && (
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={`和 ${m.nick} 私聊`}
                title="私聊"
                disabled={busy === m.id}
                onClick={() => startDm(m.id)}
                className="shrink-0 text-muted-foreground opacity-0 group-hover/row:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100"
              >
                <MessageCircle />
              </Button>
            )}
          </div>
        ))}
      </div>
    )
  return (
    <div className="flex flex-col gap-2 p-2">
      {section('成员', humans)}
      {section('Agent', agents)}
    </div>
  )
}

/** Desktop: a side column that slides open inside the chat pane. Mobile: a sheet. */
export function MemberPanel({
  info,
  meId,
  workingIds,
  open,
  onOpenChange,
  wide,
}: {
  info: Group
  meId: string
  workingIds: Set<string>
  open: boolean
  onOpenChange: (open: boolean) => void
  wide: boolean
}) {
  const reduce = useReducedMotion()
  if (wide)
    return (
      <AnimatePresence>
        {open && (
          <motion.aside
            key="members"
            aria-label="群成员"
            initial={reduce ? { opacity: 0 } : { opacity: 0, x: 24, filter: 'blur(4px)' }}
            animate={{ opacity: 1, x: 0, filter: 'blur(0px)' }}
            exit={reduce ? { opacity: 0 } : { opacity: 0, x: 16, filter: 'blur(2px)', transition: { duration: 0.15, ease: EASE_OUT } }}
            transition={{ duration: 0.4, ease: EASE_OUT }}
            className="w-[280px] shrink-0 border-l border-border"
          >
            <div className="flex h-full w-[280px] flex-col">
              <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border pr-2 pl-4">
                <h2 className="flex-1 text-[14px] font-medium text-foreground">
                  群成员 <span className="font-normal text-muted-foreground tabular-nums">· {info.members.length}</span>
                </h2>
                <Button variant="ghost" size="icon-sm" className="text-muted-foreground" aria-label="关闭" onClick={() => onOpenChange(false)}>
                  <X />
                </Button>
              </div>
              <div className="min-h-0 flex-1 overflow-y-auto">
                <MemberList info={info} meId={meId} workingIds={workingIds} />
              </div>
            </div>
          </motion.aside>
        )}
      </AnimatePresence>
    )
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-[86vw] max-w-sm gap-0 bg-card p-0">
        <SheetHeader className="border-b border-border px-4 py-3">
          <SheetTitle className="text-[14px]">
            群成员 <span className="font-normal text-muted-foreground tabular-nums">· {info.members.length}</span>
          </SheetTitle>
          <SheetDescription className="sr-only">群里的成员和 agent</SheetDescription>
        </SheetHeader>
        <div className="min-h-0 flex-1 overflow-y-auto">
          <MemberList info={info} meId={meId} workingIds={workingIds} onDone={() => onOpenChange(false)} />
        </div>
      </SheetContent>
    </Sheet>
  )
}
