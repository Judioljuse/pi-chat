import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from 'react'
import { ArrowDown, Copy, CornerUpLeft, ListPlus, LoaderCircle, MessageSquareX } from 'lucide-react'
import { useReducedMotion } from 'motion/react'
import { useStickToBottomContext, type StickToBottomContext } from 'use-stick-to-bottom'
import { MessageMarker } from '@/components/agents/message'
import { Conversation, ConversationContent } from '@/components/ai-elements/conversation'
import { WorkingIndicator, type WorkingAgent } from '@/components/pichat/working-indicator'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { copy, fmtTime } from '@/lib/format'
import type { MentionTarget } from '@/lib/markdown'
import { fail, loadOlder, loadQuote, notify, setState, useStore, type ChatState } from '@/lib/store'
import type { Group, Message } from '@/lib/types'
import { cn } from '@/lib/utils'
import { MessageItem } from './message-item'
import { SeqRefContext, useSeqRefs } from './seq-ref'
import { buildRows, GROUP_GAP_MS, snippet, type Pending } from './util'

export interface MessageListProps {
  gid: string
  info: Group
  chat: ChatState
  meId: string
  mentions: MentionTarget[]
  pending: Pending[]
  /** seq -> stable row key, so a confirmed optimistic message keeps its row. */
  keys: Map<number, string>
  workingAgents: WorkingAgent[]
  onReply: (m: Message) => void
  /** 转为任务: open the new-task dialog prefilled from the message. */
  onToTask?: (m: Message) => void
  onRecall: (m: Message) => void
  onRetry: (opKey: string) => void
  onDiscard: (opKey: string) => void
  /** Bumped by the parent after the user sends, to jump to the bottom. */
  sentTick: number
}

const LOAD_OLDER_PX = 300

function pendingMessage(p: Pending, gid: string, me: { id: string; name: string }): Message {
  return {
    seq: 0,
    groupId: gid,
    kind: 'text',
    author: { id: me.id, kind: 'human', name: me.name },
    text: p.text,
    files: p.files,
    replyTo: p.replyTo ?? null,
    createdAt: p.createdAt,
    recalled: false,
  }
}

export function MessageList(props: MessageListProps) {
  const ctx = useRef<StickToBottomContext>(null)
  // With an unread divider, start unlocked so the first paint can land on it instead of the bottom.
  const [landOnDivider] = useState(() => {
    const { firstUnread, msgs } = props.chat
    return firstUnread !== null && msgs[0]?.seq !== firstUnread
  })
  return (
    <Conversation
      contextRef={ctx}
      initial={landOnDivider ? false : 'instant'}
      resize="smooth"
      className="min-h-0 flex-1"
      aria-label="消息"
    >
      <ConversationContent scrollClassName="overflow-y-auto" className="chat-column gap-0 px-4 pt-3 pb-6">
        <Rows {...props} ctx={ctx} />
      </ConversationContent>
      <NewMessagesPill gid={props.gid} chat={props.chat} meId={props.meId} sentTick={props.sentTick} />
    </Conversation>
  )
}

function Rows({
  gid,
  info,
  chat,
  meId,
  mentions,
  pending,
  keys,
  workingAgents,
  onReply,
  onToTask,
  onRecall,
  onRetry,
  onDiscard,
  ctx,
}: MessageListProps & { ctx: RefObject<StickToBottomContext | null> }) {
  const { scrollRef, stopScroll } = useStickToBottomContext()
  const reduce = useReducedMotion()
  const msgs = chat.msgs
  const firstSeq = msgs[0]?.seq
  // Messages present at first render don't animate; later arrivals do.
  const baseline = useRef<number | null>(null)
  if (baseline.current === null) baseline.current = msgs.at(-1)?.seq ?? 0

  const keyOf = useCallback((seq: number) => keys.get(seq) ?? `m${seq}`, [keys])
  const rows = useMemo(() => buildRows(msgs, chat.firstUnread, keyOf), [msgs, chat.firstUnread, keyOf])
  const bySeq = useMemo(() => new Map(msgs.map((m) => [m.seq, m])), [msgs])
  const [sheet, setSheet] = useState<{ m: Message; canRecall: boolean } | null>(null)
  const onActions = useCallback((m: Message, canRecall: boolean) => setSheet({ m, canRecall }), [])
  const seqRefs = useSeqRefs(gid)
  const me = useMemo(() => ({ id: meId, name: info.members.find((x) => x.id === meId)?.nick || '我' }), [meId, info.members])

  // Fetch reply targets that fall outside the loaded window.
  useEffect(() => {
    for (const m of msgs) if (m.replyTo && !bySeq.has(m.replyTo)) loadQuote(gid, m.replyTo)
  }, [gid, msgs, bySeq])

  // First paint: land on the unread divider instead of the bottom.
  // The scroll container's ref attaches after this child's layout effects, so use the divider's own ref.
  const landed = useRef(false)
  const divider = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    if (landed.current) return
    landed.current = true
    if (divider.current) {
      stopScroll()
      divider.current.scrollIntoView({ block: 'center' })
    }
  }, [stopScroll])

  // Keep the viewport still when older messages are prepended.
  const snap = useRef<{ h: number; top: number; first?: number }>({ h: 0, top: 0 })
  const el = scrollRef.current
  if (el && snap.current.first !== firstSeq) snap.current = { h: el.scrollHeight, top: el.scrollTop, first: snap.current.first }
  useLayoutEffect(() => {
    const el = scrollRef.current
    const prev = snap.current
    if (el && prev.first !== undefined && firstSeq !== undefined && firstSeq < prev.first) {
      el.scrollTop = prev.top + (el.scrollHeight - prev.h)
    }
    snap.current = { h: el?.scrollHeight ?? 0, top: el?.scrollTop ?? 0, first: firstSeq }
  }, [firstSeq, scrollRef])

  const chatRef = useRef(chat)
  chatRef.current = chat
  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const onScroll = () => {
      const c = chatRef.current
      if (el.scrollTop < LOAD_OLDER_PX && c.hasMore && !c.loading) loadOlder(gid).catch(fail)
    }
    el.addEventListener('scroll', onScroll, { passive: true })
    return () => el.removeEventListener('scroll', onScroll)
  }, [gid, scrollRef])

  const jump = useCallback(
    async (seq: number) => {
      for (let i = 0; i < 20; i++) {
        const c = chatRef.current
        if (!c.hasMore || (c.msgs[0]?.seq ?? 0) <= seq) break
        await loadOlder(gid).catch(() => {})
        await new Promise((r) => requestAnimationFrame(r))
      }
      requestAnimationFrame(() => {
        const t = scrollRef.current?.querySelector<HTMLElement>(`[data-seq="${seq}"]`)
        if (!t) {
          notify('找不到这条消息')
          return
        }
        ctx.current?.stopScroll()
        t.scrollIntoView({ block: 'center', behavior: reduce ? 'auto' : 'smooth' })
        t.animate(
          [{ backgroundColor: 'color-mix(in oklab, var(--mention-me-light) 90%, transparent)' }, { backgroundColor: 'transparent' }],
          { duration: reduce ? 1 : 1600, easing: 'ease-out' },
        )
      })
    },
    [gid, scrollRef, ctx, reduce],
  )

  const refCtx = useMemo(() => ({ gid, jump }), [gid, jump])

  // Desktop notification click or a "#/g/<id>?seq=" link: scroll to that message once this chat is on screen.
  const focus = useStore((s) => s.focus)
  useEffect(() => {
    if (!focus || focus.gid !== gid) return
    setState({ focus: null })
    jump(focus.seq)
  }, [focus, gid, jump])

  return (
    <SeqRefContext.Provider value={refCtx}>
      <div className="flex h-8 items-center justify-center text-[12px] text-muted-foreground">
        {chat.hasMore ? (
          chat.loading ? (
            <LoaderCircle className="size-4 animate-spin" aria-label="加载中" />
          ) : (
            '上滑加载更早的消息'
          )
        ) : msgs.length ? (
          '没有更早的消息了'
        ) : null}
      </div>
      {!msgs.length && !pending.length && (
        <div className="py-16 text-center text-[13px] text-muted-foreground">还没有消息，说点什么吧</div>
      )}
      {[...rows.map((r, i) => {
        if (r.type === 'divider')
          return (
            <div key={r.key} ref={divider} data-unread-divider className="my-4 flex items-center gap-3 text-[12px] font-medium text-brand">
              <span className="h-px flex-1 bg-brand/30" />
              以下为新消息
              <span className="h-px flex-1 bg-brand/30" />
            </div>
          )
        if (r.type === 'sys')
          return (
            <MessageMarker
              key={r.key}
              data-seq={r.seqs[0]}
              className={cn('my-3 bg-transparent px-2 py-0.5 text-[12px]', r.seqs[0] > (baseline.current ?? 0) && 'animate-fade-up')}
            >
              <span>{r.text}</span>
              <span className="tabular-nums opacity-70">{fmtTime(r.createdAt)}</span>
            </MessageMarker>
          )
        const m = r.m
        return (
          <MessageItem
            key={r.key}
            m={m}
            grouped={r.grouped}
            meId={meId}
            members={info.members}
            mentions={mentions}
            quoted={m.replyTo ? (bySeq.get(m.replyTo) ?? chat.quotes[m.replyTo]) : undefined}
            animate={m.seq > (baseline.current ?? 0) && !keys.has(m.seq)}
            lastInRun={!(rows[i + 1]?.type === 'msg' && (rows[i + 1] as { grouped: boolean }).grouped)}
            seqRefs={seqRefs}
            onActions={onActions}
            onReply={onReply}
            onToTask={onToTask}
            onJump={jump}
            onRecall={onRecall}
            onRetry={onRetry}
            onDiscard={onDiscard}
          />
        )
      }),
      ...pending.map((p, i) => {
        const m = pendingMessage(p, gid, me)
        const lastReal = msgs.at(-1)
        const grouped =
          !p.replyTo &&
          (i > 0 ||
            (lastReal?.kind === 'text' && lastReal.author?.id === meId && p.createdAt - lastReal.createdAt < GROUP_GAP_MS))
        return (
          <MessageItem
            key={`p:${p.opKey}`}
            m={m}
            grouped={grouped}
            meId={meId}
            members={info.members}
            mentions={mentions}
            quoted={p.replyTo ? (bySeq.get(p.replyTo) ?? chat.quotes[p.replyTo]) : undefined}
            animate
            pending={p}
            seqRefs={seqRefs}
            onReply={onReply}
            onJump={jump}
            onRecall={onRecall}
            onRetry={onRetry}
            onDiscard={onDiscard}
          />
        )
      })]}
      {workingAgents.length > 0 && (
        <div className="mt-5">
          <WorkingIndicator agents={workingAgents} layoutKey={msgs.length + pending.length} />
        </div>
      )}
      <Sheet open={!!sheet} onOpenChange={(o) => !o && setSheet(null)}>
        <SheetContent side="bottom" showCloseButton={false} className="gap-0 rounded-t-[14px] bg-card px-2 pt-2 pb-[max(env(safe-area-inset-bottom),0.75rem)]">
          <div aria-hidden="true" className="mx-auto mb-2 h-1 w-9 rounded-full bg-border" />
          <SheetHeader className="px-3 pt-0 pb-2">
            <SheetTitle className="text-[13px] font-medium">{sheet?.m.author?.name}</SheetTitle>
            <SheetDescription className="truncate text-[12px]">{sheet ? snippet(sheet.m, 80) : ''}</SheetDescription>
          </SheetHeader>
          {sheet && (
            <div className="flex flex-col">
              {sheet.m.text && (
                <SheetAction icon={<Copy />} onClick={() => (copy(sheet.m.text), setSheet(null))}>
                  复制
                </SheetAction>
              )}
              <SheetAction icon={<CornerUpLeft />} onClick={() => (onReply(sheet.m), setSheet(null))}>
                回复
              </SheetAction>
              {onToTask && sheet.m.kind === 'text' && (
                <SheetAction icon={<ListPlus />} onClick={() => (onToTask(sheet.m), setSheet(null))}>
                  转为任务
                </SheetAction>
              )}
              {sheet.canRecall && (
                <SheetAction danger icon={<MessageSquareX />} onClick={() => (onRecall(sheet.m), setSheet(null))}>
                  撤回（2 分钟内）
                </SheetAction>
              )}
            </div>
          )}
        </SheetContent>
      </Sheet>
    </SeqRefContext.Provider>
  )
}

function SheetAction({ icon, danger, onClick, children }: { icon: ReactNode; danger?: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'flex h-12 items-center gap-3 rounded-control px-3 text-left text-[15px] active:bg-accent [&_svg]:size-[18px]',
        danger ? 'text-destructive' : 'text-foreground [&_svg]:text-muted-foreground',
      )}
    >
      {icon}
      {children}
    </button>
  )
}

/** "新消息" pill when messages arrive while scrolled up; otherwise a plain jump-to-bottom button. */
function NewMessagesPill({ chat, meId, sentTick }: { gid: string; chat: ChatState; meId: string; sentTick: number }) {
  const { isAtBottom, scrollToBottom } = useStickToBottomContext()
  const [unseen, setUnseen] = useState(0)
  const last = chat.msgs.at(-1)
  const lastSeq = useRef(last?.seq ?? 0)

  useEffect(() => {
    const seq = last?.seq ?? 0
    if (seq <= lastSeq.current) return
    const n = chat.msgs.filter((m) => m.seq > lastSeq.current && m.author?.id !== meId).length
    lastSeq.current = seq
    if (!isAtBottom && n) setUnseen((u) => u + n)
  }, [last?.seq, chat.msgs, isAtBottom, meId])

  useEffect(() => {
    if (isAtBottom) setUnseen(0)
  }, [isAtBottom])

  useEffect(() => {
    if (sentTick) scrollToBottom()
  }, [sentTick, scrollToBottom])

  if (isAtBottom) return null
  return (
    <button
      type="button"
      onClick={() => scrollToBottom()}
      className={cn(
        'absolute bottom-3 left-1/2 inline-flex h-8 -translate-x-1/2 animate-pop-in items-center gap-1.5 rounded-full bg-popover px-3 text-[12.5px] font-medium text-foreground shadow-overlay transition-colors hover:bg-accent',
        unseen > 0 && 'bg-primary text-primary-foreground hover:bg-primary/90',
      )}
    >
      <ArrowDown className="size-3.5" />
      {unseen > 0 ? `${unseen} 条新消息` : '回到底部'}
    </button>
  )
}
