import { useEffect, useMemo, useState } from 'react'
import { ChevronRight, ListChecks, LoaderCircle, Plus, X } from 'lucide-react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { LoaderGrid } from '@/components/beautiful/loading-state'
import { memberName } from '@/components/chat/util'
import { PrincipalAvatar } from '@/components/pichat/avatar'
import { useConfirm } from '@/components/pichat/confirm'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { errorText } from '@/lib/api'
import { EASE_OUT } from '@/lib/ease'
import { deleteTask, fail, loadTasks, setState, useStore } from '@/lib/store'
import { openCount, sectionTasks, type Task, type TaskSection } from '@/lib/tasks'
import type { Group } from '@/lib/types'
import { cn } from '@/lib/utils'
import { TaskDialog, type TaskDraft } from './task-dialog'
import { TaskRow } from './task-row'

export const TASK_PANEL_PX = 360

/** Owner/admin of the group, or the creator, may delete. */
export const canDeleteTask = (t: Task, meId: string, role?: string) => role === 'owner' || role === 'admin' || t.createdBy?.id === meId

/** The group's tasks from the store, loading them on first use. */
export function useGroupTasks(gid: string) {
  const tasks = useStore((s) => s.tasks[gid])
  const [err, setErr] = useState('')
  useEffect(() => {
    setErr('')
    loadTasks(gid).catch((e) => setErr(errorText(e)))
  }, [gid])
  return { tasks, err }
}

export function useOpenTaskCount(gid: string) {
  return useStore((s) => openCount(s.tasks[gid]))
}

function Section({
  s,
  info,
  meId,
  working,
  onJump,
  onEdit,
  onDelete,
}: {
  s: TaskSection
  info: Group
  meId: string
  working: boolean
  onJump: (t: Task) => void
  onEdit: (t: Task) => void
  onDelete: (t: Task) => void
}) {
  const [showDone, setShowDone] = useState(false)
  const a = s.assignee
  const member = a ? info.members.find((m) => m.id === a.id) : undefined
  const name = a ? (member ? memberName(member) : a.name) : '未指派'
  const row = (t: Task) => (
    <TaskRow
      key={t.id}
      task={t}
      meId={meId}
      members={info.members}
      canDelete={canDeleteTask(t, meId, info.myRole)}
      onJump={onJump}
      onEdit={onEdit}
      onDelete={onDelete}
    />
  )
  return (
    <section className="flex flex-col gap-0.5">
      <h3 className="flex h-7 items-center gap-2 px-2 text-[12.5px] font-medium text-body">
        {a ? (
          <PrincipalAvatar id={a.id} name={name} kind={member?.kind ?? a.kind ?? 'human'} platform={member?.platform} size="xs" working={working} />
        ) : (
          <span className="grid size-5 place-items-center rounded-full border border-dashed border-border text-muted-foreground" aria-hidden="true" />
        )}
        <span className="min-w-0 truncate">
          {name}
          {a?.id === meId ? '（我）' : ''}
        </span>
        {working && (
          <span className="inline-flex items-center gap-1.5 text-[11.5px] font-normal text-brand" title="正在处理">
            <LoaderGrid cell={3} />
            处理中
          </span>
        )}
        <span className="ml-auto font-normal text-muted-foreground tabular-nums">{s.open.length}</span>
      </h3>
      {s.open.length > 0 && <ul className="flex flex-col">{s.open.map(row)}</ul>}
      {s.done.length > 0 && (
        <>
          <button
            type="button"
            onClick={() => setShowDone((v) => !v)}
            aria-expanded={showDone}
            className="flex h-7 items-center gap-1 rounded-control px-2 text-left text-[12px] text-muted-foreground hover:bg-accent/60 hover:text-foreground"
          >
            <ChevronRight className={cn('size-3.5 transition-transform', showDone && 'rotate-90')} />
            已完成 <span className="tabular-nums">{s.done.length}</span>
          </button>
          {showDone && <ul className="flex flex-col">{s.done.map(row)}</ul>}
        </>
      )}
    </section>
  )
}

function TasksBody({ info, meId, onPicked, onNew }: { info: Group; meId: string; onPicked?: () => void; onNew: () => void }) {
  const gid = info.id
  const { tasks, err } = useGroupTasks(gid)
  const working = useStore((s) => s.working[gid])
  const [draft, setDraft] = useState<TaskDraft | null>(null)
  const { ask, dialog } = useConfirm()

  const nameOf = useMemo(() => {
    const by = new Map(info.members.map((m) => [m.id, memberName(m)]))
    return (id: string) => by.get(id)
  }, [info.members])
  const sections = useMemo(() => (tasks ? sectionTasks(tasks, meId, nameOf) : []), [tasks, meId, nameOf])

  const jump = (t: Task) => {
    if (!t.sourceSeq) return
    setState({ focus: { gid, seq: t.sourceSeq } })
    onPicked?.()
  }
  const remove = async (t: Task) => {
    if (!(await ask({ title: '删除这个任务？', description: `「${t.title}」会从任务列表里删除，不能恢复。`, confirmText: '删除', destructive: true }))) return
    deleteTask(t).catch(fail)
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-3 pt-3 pb-1">
        <Button variant="outline" size="sm" className="w-full" onClick={onNew}>
          <Plus />
          新建任务
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-1.5 pt-1 pb-3">
        {err && !tasks ? (
          <p className="px-2 py-6 text-center text-[13px] text-destructive">{err}</p>
        ) : !tasks ? (
          <div className="grid place-items-center py-8">
            <LoaderCircle className="size-4 animate-spin text-muted-foreground" aria-label="加载中" />
          </div>
        ) : !tasks.length ? (
          <div className="flex flex-col items-center gap-2 px-4 py-10 text-center">
            <ListChecks className="size-5 text-muted-foreground" />
            <p className="text-[13px] text-muted-foreground">还没有任务。可以新建一个，或者在消息上选「转为任务」。</p>
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            {sections.map((s) => (
              <Section
                key={s.key || '-'}
                s={s}
                info={info}
                meId={meId}
                working={!!s.key && !!working?.[s.key]}
                onJump={jump}
                onEdit={(t) => setDraft({ mode: 'edit', task: t })}
                onDelete={remove}
              />
            ))}
          </div>
        )}
      </div>
      <TaskDialog draft={draft} onClose={() => setDraft(null)} members={info.members} meId={meId} />
      {dialog}
    </div>
  )
}

/** Desktop: a side column in the chat pane (shares the slot with members and search). Mobile: a full-screen sheet. */
export function TaskPanel({
  info,
  meId,
  open,
  onOpenChange,
  wide,
  onNew,
}: {
  info: Group
  meId: string
  open: boolean
  onOpenChange: (open: boolean) => void
  wide: boolean
  onNew: () => void
}) {
  const reduce = useReducedMotion()
  const count = useOpenTaskCount(info.id)
  const title = (
    <>
      任务 {count > 0 && <span className="font-normal text-muted-foreground tabular-nums">· {count}</span>}
    </>
  )
  if (wide)
    return (
      <AnimatePresence>
        {open && (
          <motion.aside
            key="tasks"
            aria-label="任务"
            initial={reduce ? { opacity: 0 } : { opacity: 0, x: 24, filter: 'blur(4px)' }}
            animate={{ opacity: 1, x: 0, filter: 'blur(0px)' }}
            exit={reduce ? { opacity: 0 } : { opacity: 0, x: 16, filter: 'blur(2px)', transition: { duration: 0.15, ease: EASE_OUT } }}
            transition={{ duration: 0.4, ease: EASE_OUT }}
            className="shrink-0 border-l border-border"
            style={{ width: TASK_PANEL_PX }}
          >
            <div className="flex h-full flex-col" style={{ width: TASK_PANEL_PX }}>
              <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border pr-2 pl-4">
                <h2 className="flex-1 text-[14px] font-medium text-foreground">{title}</h2>
                <Button variant="ghost" size="icon-sm" className="text-muted-foreground" aria-label="关闭" onClick={() => onOpenChange(false)}>
                  <X />
                </Button>
              </div>
              <TasksBody info={info} meId={meId} onNew={onNew} />
            </div>
          </motion.aside>
        )}
      </AnimatePresence>
    )
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full max-w-none gap-0 bg-card p-0 data-[side=right]:w-full data-[side=right]:sm:max-w-none">
        <SheetHeader className="border-b border-border px-4 py-3">
          <SheetTitle className="text-[14px]">{title}</SheetTitle>
          <SheetDescription className="sr-only">这个群的任务，按负责人分组</SheetDescription>
        </SheetHeader>
        <TasksBody info={info} meId={meId} onNew={onNew} onPicked={() => onOpenChange(false)} />
      </SheetContent>
    </Sheet>
  )
}
