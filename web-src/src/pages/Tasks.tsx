import { useEffect, useMemo, useRef, useState } from 'react'
import { ListChecks, LoaderCircle } from 'lucide-react'
import { useConfirm } from '@/components/pichat/confirm'
import { Empty, Page } from '@/components/pichat/page'
import { canDeleteTask } from '@/components/tasks/task-panel'
import { TaskDialog, type TaskDraft } from '@/components/tasks/task-dialog'
import { TaskRow } from '@/components/tasks/task-row'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { api, errorText } from '@/lib/api'
import { navigate } from '@/lib/router'
import { deleteTask, fail, loadInfo, setState, useStore } from '@/lib/store'
import { boardQuery, matchesBoard, normalizeTasks, removeTask, sortTasks, upsertTask, type Task, type TaskPerson } from '@/lib/tasks'

type Scope = 'me' | 'all'
const ANYONE = '__all'

function Segmented({ value, onValueChange, options, label }: { value: string; onValueChange: (v: string) => void; options: { value: string; label: string }[]; label: string }) {
  return (
    <ToggleGroup
      type="single"
      spacing={0}
      value={value}
      aria-label={label}
      onValueChange={(v) => v && onValueChange(v)}
      className="h-8 w-fit gap-0.5 rounded-control bg-accent p-0.5"
    >
      {options.map((o) => (
        <ToggleGroupItem
          key={o.value}
          value={o.value}
          className="h-7 rounded-[6px]! px-3 text-[13px] font-normal text-muted-foreground hover:bg-transparent hover:text-foreground data-[state=on]:bg-card data-[state=on]:text-foreground data-[state=on]:shadow-hairline"
        >
          {o.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}

/** #/tasks: tasks across all my groups. */
export function Tasks() {
  const me = useStore((s) => s.me)
  const meId = me?.id ?? ''
  const info = useStore((s) => s.info)
  const lastEvent = useStore((s) => s.lastTaskEvent)
  const tick = useStore((s) => s.taskTick)
  const [scope, setScope] = useState<Scope>('all')
  const [person, setPerson] = useState(ANYONE)
  const [status, setStatus] = useState<'open' | 'all'>('open')
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [err, setErr] = useState('')
  const [people, setPeople] = useState<Map<string, TaskPerson>>(() => new Map())
  const [draft, setDraft] = useState<TaskDraft | null>(null)
  const [reload, setReload] = useState(0)
  const { ask, dialog } = useConfirm()

  const assignee = scope === 'me' ? 'me' : person === ANYONE ? undefined : person
  const filter = useMemo(() => ({ assignee, status }), [assignee, status])

  useEffect(() => {
    const ac = new AbortController()
    setErr('')
    api<{ tasks?: unknown[] }>('GET', `/tasks?${boardQuery(filter)}`, undefined, { signal: ac.signal }).then(
      (r) => setTasks(normalizeTasks(r.tasks)),
      (e) => {
        if (!ac.signal.aborted) setErr(errorText(e))
      },
    )
    return () => ac.abort()
  }, [filter, reload])

  // Live: apply 'task' events that match the filters; a resync (no event) refetches.
  const seenTick = useRef(tick)
  useEffect(() => {
    if (tick === seenTick.current) return
    seenTick.current = tick
    if (!lastEvent) {
      setReload((n) => n + 1)
      return
    }
    setTasks((list) => {
      if (!list) return list
      const t = lastEvent.task
      if (t && matchesBoard(t, filter, meId)) {
        const prev = list.find((x) => x.id === t.id)
        // SSE task JSON may omit groupName; keep the one we had.
        return upsertTask(list, prev && !t.groupName ? { ...t, groupName: prev.groupName } : t)
      }
      return removeTask(list, lastEvent.taskId)
    })
  }, [tick]) // only on new events; filter/lastEvent are read at that moment

  // Everyone seen as an assignee stays in the person select, whatever the current filter.
  useEffect(() => {
    if (!tasks?.length) return
    setPeople((p) => {
      let changed = false
      const n = new Map(p)
      for (const t of tasks)
        if (t.assignee && !n.has(t.assignee.id)) {
          n.set(t.assignee.id, t.assignee)
          changed = true
        }
      return changed ? n : p
    })
  }, [tasks])
  const personList = useMemo(() => [...people.values()].sort((a, b) => a.name.localeCompare(b.name, 'zh-CN')), [people])

  const sorted = useMemo(() => (tasks ? sortTasks(tasks) : []), [tasks])
  const ensureInfo = (t: Task) => {
    if (!info[t.groupId]) loadInfo(t.groupId).catch(() => {})
  }
  const toGroup = (t: Task) => navigate(`#/g/${encodeURIComponent(t.groupId)}?tasks`)
  const jump = (t: Task) => {
    if (!t.sourceSeq) return
    setState({ focus: { gid: t.groupId, seq: t.sourceSeq } })
    navigate(`#/g/${encodeURIComponent(t.groupId)}`)
  }
  const remove = async (t: Task) => {
    if (!(await ask({ title: '删除这个任务？', description: `「${t.title}」会从任务列表里删除，不能恢复。`, confirmText: '删除', destructive: true }))) return
    deleteTask(t).catch(fail)
  }
  const edit = (t: Task) => {
    ensureInfo(t)
    setDraft({ mode: 'edit', task: t })
  }
  const draftGid = draft?.mode === 'edit' ? draft.task.groupId : draft?.gid

  return (
    <Page title="任务" back="#/" className="gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <Segmented
          label="范围"
          value={scope}
          onValueChange={(v) => setScope(v as Scope)}
          options={[
            { value: 'me', label: '我的' },
            { value: 'all', label: '全部' },
          ]}
        />
        {scope === 'all' && (
          <Select value={person} onValueChange={setPerson}>
            <SelectTrigger size="sm" className="w-40 bg-background" aria-label="负责人">
              <SelectValue />
            </SelectTrigger>
            <SelectContent position="popper">
              <SelectItem value={ANYONE}>所有人</SelectItem>
              {personList.map((p) => (
                <SelectItem key={p.id} value={p.id}>
                  {p.name}
                  {p.id === meId ? '（我）' : ''}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        <span className="ml-auto" />
        <Segmented
          label="状态"
          value={status}
          onValueChange={(v) => setStatus(v as 'open' | 'all')}
          options={[
            { value: 'open', label: '未完成' },
            { value: 'all', label: '全部' },
          ]}
        />
      </div>
      {err && !tasks ? (
        <Empty>{err}</Empty>
      ) : !tasks ? (
        <div className="grid place-items-center py-10">
          <LoaderCircle className="size-4 animate-spin text-muted-foreground" aria-label="加载中" />
        </div>
      ) : !sorted.length ? (
        <Empty icon={<ListChecks />} title="没有任务">
          {status === 'open' ? '这里没有未完成的任务。' : '这里还没有任务。'}在群聊里点「任务」新建，或者在消息上选「转为任务」。
        </Empty>
      ) : (
        <ul className="flex flex-col gap-0.5 rounded-card border border-border bg-card p-1.5">
          {sorted.map((t) => {
            const g = info[t.groupId]
            return (
              <TaskRow
                key={t.id}
                task={t}
                meId={meId}
                members={g?.members}
                canDelete={canDeleteTask(t, meId, g?.myRole)}
                showGroup
                onJump={jump}
                onEdit={edit}
                onDelete={remove}
                onGroup={toGroup}
                onMenu={ensureInfo}
              />
            )
          })}
        </ul>
      )}
      <TaskDialog draft={draft} onClose={() => setDraft(null)} members={draftGid ? info[draftGid]?.members : undefined} meId={meId} />
      {dialog}
    </Page>
  )
}
