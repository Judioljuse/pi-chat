import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type DragEvent } from 'react'
import { ImageUp } from 'lucide-react'
import { ChatHeader } from '@/components/chat/chat-header'
import { Composer, type ComposerHandle, type Outgoing } from '@/components/chat/composer'
import { MemberPanel } from '@/components/chat/member-panel'
import { MessageList } from '@/components/chat/message-list'
import { mentionTargets, memberName, reportWake, type Pending } from '@/components/chat/util'
import { Empty } from '@/components/pichat/page'
import type { WorkingAgent } from '@/components/pichat/working-indicator'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { api, ApiError, errorText } from '@/lib/api'
import { randId } from '@/lib/format'
import { agentStateOf, fail, getState, markRead, mergeMsgs, openChat, patchChat, patchMessage, useStore } from '@/lib/store'
import type { Message, SendResult } from '@/lib/types'

const EMPTY_WORKING: Record<string, number> = {}

function useWide() {
  const q = '(min-width: 1180px)'
  const [wide, setWide] = useState(() => matchMedia(q).matches)
  useEffect(() => {
    const mq = matchMedia(q)
    const f = () => setWide(mq.matches)
    mq.addEventListener('change', f)
    return () => mq.removeEventListener('change', f)
  }, [])
  return wide
}

function sendError(e: unknown) {
  if (e instanceof ApiError && e.code === 'reply_not_found') return '被回复的消息不存在或已撤回'
  return errorText(e)
}

export function Chat({ gid }: { gid: string }) {
  const me = useStore((s) => s.me)
  const info = useStore((s) => s.info[gid])
  const chat = useStore((s) => s.chats[gid])
  const summary = useStore((s) => s.groups.find((g) => g.id === gid))
  const working = useStore((s) => s.working[gid]) ?? EMPTY_WORKING
  const meId = me?.id ?? ''

  const [err, setErr] = useState('')
  const [reply, setReply] = useState<Message | null>(null)
  const [pending, setPending] = useState<Pending[]>([])
  const [keys, setKeys] = useState(() => new Map<number, string>())
  const [sentTick, setSentTick] = useState(0)
  const [recalling, setRecalling] = useState<Message | null>(null)
  const wide = useWide()
  const [membersOpen, setMembersOpen] = useState(false)
  const [dragging, setDragging] = useState(false)
  const dragDepth = useRef(0)
  const composer = useRef<ComposerHandle>(null)

  useEffect(() => {
    setErr('')
    openChat(gid).catch((e) => setErr(errorText(e)))
  }, [gid])

  const workingIds = useMemo(() => new Set(Object.keys(working)), [working])
  const lastSeq = chat?.msgs.at(-1)?.seq ?? info?.lastSeq ?? 0
  const workingAgents = useMemo<WorkingAgent[]>(() => {
    const rows: WorkingAgent[] = Object.entries(working)
      .sort((a, b) => a[1] - b[1])
      .map(([id, startedAt]) => {
        const m = info?.members.find((x) => x.id === id)
        return { id, name: m ? memberName(m) : 'agent', platform: m?.platform, startedAt }
      })
    // Attached agents with an unread message aimed at them here (@, @all, reply to them, DM):
    // the listener queues that wake until the terminal session ends.
    const msgs = chat?.msgs ?? []
    const bySeq = new Map(msgs.map((x) => [x.seq, x]))
    const aimedAt = (id: string, after: number) =>
      msgs.some(
        (x) =>
          x.seq > after &&
          x.kind === 'text' &&
          !x.recalled &&
          x.author &&
          x.author.id !== id &&
          (info?.kind === 'dm' || x.mentionAll || x.mentions?.includes(id) || (x.replyTo != null && bySeq.get(x.replyTo)?.author?.id === id)),
      )
    for (const m of info?.members ?? [])
      if (m.kind === 'agent' && !working[m.id] && agentStateOf(m) === 'attached' && (m.lastReadSeq || 0) < lastSeq && aimedAt(m.id, m.lastReadSeq || 0))
        rows.push({ id: m.id, name: memberName(m), platform: m.platform, startedAt: 0, attached: true })
    return rows
  }, [working, info?.members, info?.kind, lastSeq, chat?.msgs])
  const mentions = useMemo(() => mentionTargets(info?.members ?? [], meId), [info?.members, meId])

  // ---- send ----

  const deliver = useCallback(
    async (p: Pending) => {
      setPending((ps) => ps.map((x) => (x.opKey === p.opKey ? { ...x, status: 'sending', error: undefined } : x)))
      try {
        const r = await api<SendResult>('POST', `/groups/${gid}/messages`, {
          text: p.text,
          mentions: p.mentions,
          replyTo: p.replyTo || undefined,
          files: p.files.map((f) => f.id),
          opKey: p.opKey,
          ignoreNew: true,
        })
        setKeys((k) => new Map(k).set(r.message.seq, `p:${p.opKey}`))
        patchChat(gid, (c) => ({ msgs: mergeMsgs(c.msgs, [r.message]) }))
        setPending((ps) => ps.filter((x) => x.opKey !== p.opKey))
        const g = getState().info[gid]
        if (g) reportWake(r, g)
        markRead(gid)
      } catch (e) {
        setPending((ps) => ps.map((x) => (x.opKey === p.opKey ? { ...x, status: 'error', error: sendError(e) } : x)))
      }
    },
    [gid],
  )

  const onSend = useCallback(
    (o: Outgoing) => {
      const p: Pending = { ...o, opKey: randId(), createdAt: Date.now(), status: 'sending' }
      setPending((ps) => [...ps, p])
      setSentTick((t) => t + 1)
      deliver(p)
    },
    [deliver],
  )

  const pendingRef = useRef(pending)
  pendingRef.current = pending
  const onRetry = useCallback(
    (opKey: string) => {
      const p = pendingRef.current.find((x) => x.opKey === opKey)
      if (p) deliver(p)
    },
    [deliver],
  )
  const onDiscard = useCallback((opKey: string) => setPending((ps) => ps.filter((x) => x.opKey !== opKey)), [])

  const onReply = useCallback((m: Message) => {
    setReply(m)
    composer.current?.focus()
  }, [])

  const recall = async () => {
    const m = recalling
    setRecalling(null)
    if (!m) return
    try {
      await api('DELETE', `/groups/${gid}/messages/${m.seq}`)
      patchMessage(gid, m.seq, { recalled: true, text: '', files: [] })
      if (reply?.seq === m.seq) setReply(null)
    } catch (e) {
      fail(e instanceof ApiError && e.code === 'recall_expired' ? new Error('超过 2 分钟，不能撤回了') : e)
    }
  }

  // ---- drag & drop ----

  const hasFiles = (e: DragEvent) => [...e.dataTransfer.types].includes('Files')
  const dnd = {
    onDragEnter: (e: DragEvent) => {
      if (!hasFiles(e)) return
      e.preventDefault()
      dragDepth.current++
      setDragging(true)
    },
    onDragOver: (e: DragEvent) => {
      if (!hasFiles(e)) return
      e.preventDefault()
      e.dataTransfer.dropEffect = 'copy'
    },
    onDragLeave: (e: DragEvent) => {
      if (!hasFiles(e)) return
      dragDepth.current = Math.max(0, dragDepth.current - 1)
      if (!dragDepth.current) setDragging(false)
    },
    onDrop: (e: DragEvent) => {
      if (!hasFiles(e)) return
      e.preventDefault()
      dragDepth.current = 0
      setDragging(false)
      composer.current?.addFiles([...e.dataTransfer.files])
    },
  }

  const ready = info && chat?.loaded
  return (
    <div className="flex h-full min-h-0 flex-col" style={{ '--panel': wide && membersOpen && info ? '280px' : '0px' } as CSSProperties}>
      <ChatHeader gid={gid} info={info} summary={summary} onMembers={() => setMembersOpen((o) => !o)} workingIds={workingIds} />
      <div className="flex min-h-0 flex-1">
        <div className="relative flex min-h-0 min-w-0 flex-1 flex-col" {...(ready ? dnd : {})}>
          {!ready ? (
            err ? (
              <Empty>{err}</Empty>
            ) : (
              <ChatSkeleton />
            )
          ) : (
            <>
              <MessageList
                gid={gid}
                info={info}
                chat={chat}
                meId={meId}
                mentions={mentions}
                pending={pending}
                keys={keys}
                workingAgents={workingAgents}
                onReply={onReply}
                onRecall={setRecalling}
                onRetry={onRetry}
                onDiscard={onDiscard}
                sentTick={sentTick}
              />
              <Composer
                ref={composer}
                gid={gid}
                info={info}
                meId={meId}
                reply={reply}
                onClearReply={() => setReply(null)}
                onSend={onSend}
                onShowMembers={() => setMembersOpen(true)}
                workingIds={workingIds}
              />
              {dragging && (
                <div className="pointer-events-none absolute inset-2 z-20 grid place-items-center rounded-card border-2 border-dashed border-brand/50 bg-brand-light/80 animate-fade-in">
                  <div className="flex flex-col items-center gap-2 text-brand">
                    <ImageUp className="size-6" />
                    <span className="text-[13px] font-medium">松开即可添加附件</span>
                  </div>
                </div>
              )}
            </>
          )}
        </div>
        {info && (
          <MemberPanel info={info} meId={meId} workingIds={workingIds} open={membersOpen} onOpenChange={setMembersOpen} wide={wide} />
        )}
      </div>

      <AlertDialog open={!!recalling} onOpenChange={(o) => !o && setRecalling(null)}>
        <AlertDialogContent size="sm">
          <AlertDialogHeader>
            <AlertDialogTitle>撤回这条消息？</AlertDialogTitle>
            <AlertDialogDescription>群里所有人都会看到"你撤回了一条消息"。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={recall}>
              撤回
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function ChatSkeleton() {
  return (
    <div className="mx-auto flex w-full max-w-[760px] flex-1 flex-col justify-end gap-5 px-4 pb-6" aria-label="加载中">
      {[0.6, 0.4, 0.75].map((w, i) => (
        <div key={i} className={i === 1 ? 'flex justify-end' : 'flex gap-2'}>
          {i !== 1 && <Skeleton className="size-8 rounded-full" />}
          <div className="flex flex-col gap-1.5" style={{ width: `${w * 100}%` }}>
            <Skeleton className="h-3 w-24" />
            <Skeleton className="h-10 w-full rounded-[12px]" />
          </div>
        </div>
      ))}
    </div>
  )
}
