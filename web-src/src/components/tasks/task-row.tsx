import { useState } from 'react'
import { ChevronDown, ChevronRight, Ellipsis, Pencil, Trash2, UserRound } from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/motion/dropdown-menu'
import { PrincipalAvatar } from '@/components/pichat/avatar'
import { memberName } from '@/components/chat/util'
import { fmtShort, fmtTime } from '@/lib/format'
import { fail, updateTask } from '@/lib/store'
import { STATUS_LABEL, TASK_STATUSES, type Task, type TaskStatus } from '@/lib/tasks'
import type { Member } from '@/lib/types'
import { cn } from '@/lib/utils'

const CHIP: Record<TaskStatus, string> = {
  doing: 'bg-brand-light text-brand',
  blocked: 'bg-mention-me-light text-warning',
  todo: 'bg-accent text-body',
  done: 'bg-accent text-muted-foreground',
}

const DOT: Record<TaskStatus, string> = {
  doing: 'bg-brand',
  blocked: 'bg-warning',
  todo: 'bg-muted-foreground/50',
  done: 'bg-success',
}

/** Status chip that opens a status menu. */
function StatusMenu({ task, disabled }: { task: Task; disabled?: boolean }) {
  const set = (v: string) => {
    if (v === task.status) return
    updateTask(task, { status: v }).catch(fail)
  }
  const chip = (
    <button
      type="button"
      disabled={disabled}
      aria-label={`状态：${STATUS_LABEL[task.status]}，点击修改`}
      className={cn(
        'inline-flex h-5 shrink-0 items-center gap-1 rounded-chip px-1.5 text-[11.5px] leading-none font-medium whitespace-nowrap transition-opacity hover:opacity-80 disabled:cursor-default disabled:hover:opacity-100',
        CHIP[task.status],
      )}
    >
      <span className={cn('size-1.5 rounded-full', DOT[task.status])} />
      {STATUS_LABEL[task.status]}
    </button>
  )
  if (disabled) return chip
  return (
    <DropdownMenu>
      <DropdownMenuTrigger>{chip}</DropdownMenuTrigger>
      <DropdownMenuContent className="min-w-36">
        <DropdownMenuRadioGroup value={task.status} onValueChange={set}>
          {TASK_STATUSES.map((s) => (
            <DropdownMenuRadioItem key={s} value={s} closeOnSelect>
              <span className={cn('size-1.5 rounded-full', DOT[s])} />
              {STATUS_LABEL[s]}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export interface TaskRowProps {
  task: Task
  meId: string
  /** Members of the task's group, for 重新指派; undefined while not loaded. */
  members?: Member[]
  /** Owner/admin of the group, or the task's creator. */
  canDelete: boolean
  /** Show the group name (global board). */
  showGroup?: boolean
  onJump: (task: Task) => void
  onEdit: (task: Task) => void
  onDelete: (task: Task) => void
  onGroup?: (task: Task) => void
  /** Called when the actions menu opens (the board loads the group's members then). */
  onMenu?: (task: Task) => void
}

export function TaskRow({ task, meId, members, canDelete, showGroup, onJump, onEdit, onDelete, onGroup, onMenu }: TaskRowProps) {
  const [open, setOpen] = useState(false)
  const a = task.assignee
  const member = a ? members?.find((m) => m.id === a.id) : undefined
  const assigneeName = member ? memberName(member) : a?.name
  const reassign = (id: string) => {
    const next = id === '' ? null : id
    if (next === (a?.id ?? null)) return
    updateTask(task, { assigneeId: next }).catch(fail)
  }
  const who = task.updatedBy?.name
  return (
    <li className="group/task rounded-control px-2 py-1.5 hover:bg-accent/60">
      <div className="flex min-w-0 items-start gap-2">
        <span className="pt-px">
          <StatusMenu task={task} />
        </span>
        <div className="min-w-0 flex-1">
          <button
            type="button"
            onClick={() => task.note && setOpen((o) => !o)}
            aria-expanded={task.note ? open : undefined}
            className={cn(
              'flex w-full min-w-0 items-start gap-1 text-left text-[13.5px] leading-5 break-words text-foreground',
              task.status === 'done' && 'text-muted-foreground line-through decoration-muted-foreground/50',
              !task.note && 'cursor-default',
            )}
          >
            <span className="min-w-0 flex-1">{task.title}</span>
            {task.note &&
              (open ? (
                <ChevronDown className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
              ) : (
                <ChevronRight className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
              ))}
          </button>
          <div className="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-[12px] text-muted-foreground">
            {showGroup && task.groupName && (
              <button type="button" onClick={() => onGroup?.(task)} className="max-w-40 truncate font-medium text-body hover:underline" title="打开这个群的任务">
                {task.groupName}
              </button>
            )}
            {a && (
              <span className="inline-flex min-w-0 items-center gap-1">
                <PrincipalAvatar id={a.id} name={assigneeName || a.name} kind={a.kind ?? member?.kind ?? 'human'} platform={member?.platform} size="xs" />
                <span className="max-w-28 truncate">{assigneeName}</span>
              </span>
            )}
            <span className="tabular-nums" title={`${who ? `${who} 更新于 ` : '更新于 '}${fmtTime(task.updatedAt)}`}>
              {fmtShort(task.updatedAt)}
            </span>
            {task.sourceSeq && (
              <button type="button" onClick={() => onJump(task)} className="font-mono tabular-nums hover:text-brand hover:underline" title="跳到来源消息">
                #{task.sourceSeq}
              </button>
            )}
          </div>
          {open && task.note && (
            <p className="mt-1.5 rounded-control bg-accent/70 px-2.5 py-2 text-[12.5px] leading-relaxed break-words whitespace-pre-wrap text-body">{task.note}</p>
          )}
        </div>
        <DropdownMenu onOpenChange={(o) => o && onMenu?.(task)}>
          <DropdownMenuTrigger>
            <button
              type="button"
              aria-label="任务操作"
              className="grid size-6 shrink-0 place-items-center rounded-md text-muted-foreground opacity-0 transition-opacity group-hover/task:opacity-100 hover:bg-accent hover:text-foreground focus-visible:opacity-100 aria-expanded:opacity-100 pointer-coarse:opacity-100"
            >
              <Ellipsis className="size-4" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="min-w-44">
            <DropdownMenuItem onSelect={() => onEdit(task)}>
              <Pencil />
              编辑
            </DropdownMenuItem>
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <UserRound />
                重新指派
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="max-h-72 min-w-44 overflow-y-auto">
                {members ? (
                  <DropdownMenuRadioGroup value={a?.id ?? ''} onValueChange={reassign}>
                    {members.map((m) => (
                      <DropdownMenuRadioItem key={m.id} value={m.id} closeOnSelect textValue={memberName(m)}>
                        <PrincipalAvatar id={m.id} name={memberName(m)} kind={m.kind} platform={m.platform} size="xs" />
                        <span className="min-w-0 truncate">
                          {memberName(m)}
                          {m.id === meId ? '（我）' : ''}
                        </span>
                      </DropdownMenuRadioItem>
                    ))}
                    <DropdownMenuRadioItem value="" closeOnSelect>
                      不指派
                    </DropdownMenuRadioItem>
                  </DropdownMenuRadioGroup>
                ) : (
                  <DropdownMenuLabel>加载成员中…</DropdownMenuLabel>
                )}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            {canDelete && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem tone="destructive" onSelect={() => onDelete(task)}>
                  <Trash2 />
                  删除
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </li>
  )
}
