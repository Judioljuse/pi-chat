import { useEffect, useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { memberName } from '@/components/chat/util'
import { createTask, fail, updateTask } from '@/lib/store'
import { NOTE_MAX, STATUS_LABEL, TASK_STATUSES, TITLE_MAX, type Task, type TaskStatus } from '@/lib/tasks'
import type { Member } from '@/lib/types'

/** What the dialog edits: a new task (optionally prefilled from a message) or an existing one. */
export type TaskDraft = { mode: 'new'; gid: string; title?: string; sourceSeq?: number | null } | { mode: 'edit'; task: Task }

const NONE = '__none'

export function TaskDialog({
  draft,
  onClose,
  members,
  meId,
}: {
  draft: TaskDraft | null
  onClose: () => void
  /** Members of the task's group; undefined while unknown (the assignee select then only offers the current value). */
  members?: Member[]
  meId: string
}) {
  const [title, setTitle] = useState('')
  const [note, setNote] = useState('')
  const [assignee, setAssignee] = useState(NONE)
  const [status, setStatus] = useState<TaskStatus>('todo')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!draft) return
    if (draft.mode === 'new') {
      setTitle(draft.title ?? '')
      setNote('')
      setAssignee(meId || NONE)
      setStatus('todo')
    } else {
      const t = draft.task
      setTitle(t.title)
      setNote(t.note)
      setAssignee(t.assignee?.id ?? NONE)
      setStatus(t.status)
    }
    setBusy(false)
  }, [draft, meId])

  const options = (members ?? []).map((m) => ({ id: m.id, name: memberName(m) + (m.id === meId ? '（我）' : ''), kind: m.kind }))
  const current = draft?.mode === 'edit' ? draft.task.assignee : null
  if (current && !options.some((o) => o.id === current.id)) options.push({ id: current.id, name: current.name, kind: current.kind ?? 'human' })
  options.sort((a, b) => (a.id === meId ? -1 : b.id === meId ? 1 : a.kind === b.kind ? 0 : a.kind === 'human' ? -1 : 1))

  const clean = title.replace(/\s+/g, ' ').trim()
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!draft || !clean || busy) return
    setBusy(true)
    const assigneeId = assignee === NONE ? null : assignee
    try {
      if (draft.mode === 'new') {
        await createTask(draft.gid, {
          title: clean,
          note: note.trim() || undefined,
          assigneeId: assigneeId ?? undefined,
          status,
          sourceSeq: draft.sourceSeq || undefined,
        })
      } else {
        const t = draft.task
        const patch: { title?: string; note?: string; assigneeId?: string | null; status?: string } = {}
        if (clean !== t.title) patch.title = clean
        if (note.trim() !== t.note.trim()) patch.note = note.trim()
        if (assigneeId !== (t.assignee?.id ?? null)) patch.assigneeId = assigneeId
        if (status !== t.status) patch.status = status
        if (Object.keys(patch).length) await updateTask(t, patch)
      }
      onClose()
    } catch (err) {
      fail(err)
      setBusy(false)
    }
  }

  const sourceSeq = draft?.mode === 'new' ? draft.sourceSeq : draft?.task.sourceSeq
  return (
    <Dialog open={!!draft} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="gap-0 p-0 sm:max-w-md">
        <form onSubmit={submit} className="flex flex-col">
          <DialogHeader className="gap-1 px-5 pt-5">
            <DialogTitle className="font-serif text-[20px] font-normal text-foreground">{draft?.mode === 'edit' ? '编辑任务' : '新建任务'}</DialogTitle>
            <DialogDescription className="text-[13px]">
              {sourceSeq ? `来自消息 #${sourceSeq}。` : ''}任务只在任务面板里显示，不会发到群聊。
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-3 px-5 py-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="task-title" className="text-[13px] font-medium text-body">
                标题
              </Label>
              <Input id="task-title" autoFocus value={title} maxLength={TITLE_MAX} onChange={(e) => setTitle(e.target.value)} placeholder="要做什么" />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="flex min-w-0 flex-col gap-1.5">
                <Label className="text-[13px] font-medium text-body">负责人</Label>
                <Select value={assignee} onValueChange={setAssignee}>
                  <SelectTrigger className="w-full bg-background" aria-label="负责人">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent position="popper">
                    {options.map((o) => (
                      <SelectItem key={o.id} value={o.id}>
                        {o.name}
                      </SelectItem>
                    ))}
                    <SelectItem value={NONE}>不指派</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="flex min-w-0 flex-col gap-1.5">
                <Label className="text-[13px] font-medium text-body">状态</Label>
                <Select value={status} onValueChange={(v) => setStatus(v as TaskStatus)}>
                  <SelectTrigger className="w-full bg-background" aria-label="状态">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent position="popper">
                    {TASK_STATUSES.map((s) => (
                      <SelectItem key={s} value={s}>
                        {STATUS_LABEL[s]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="task-note" className="text-[13px] font-medium text-body">
                备注<span className="ml-1 font-normal text-muted-foreground">（可选）</span>
              </Label>
              <Textarea
                id="task-note"
                rows={4}
                value={note}
                maxLength={NOTE_MAX}
                onChange={(e) => setNote(e.target.value)}
                placeholder="进展、卡在哪里、相关链接"
                className="max-h-60 min-h-0"
              />
            </div>
          </div>
          <DialogFooter className="mx-0 mb-0 border-t border-border px-5 py-3">
            <Button type="button" variant="ghost" onClick={onClose}>
              取消
            </Button>
            <Button type="submit" disabled={!clean || busy}>
              {draft?.mode === 'edit' ? '保存' : '创建'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
