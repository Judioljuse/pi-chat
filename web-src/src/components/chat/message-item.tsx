import { memo, useEffect, useState, type ReactNode } from 'react'
import { useReducedMotion } from 'motion/react'
import { AlertCircle, Copy, CornerUpLeft, CheckCheck, Ellipsis, FileText, MessageSquareX, RotateCcw, Trash2 } from 'lucide-react'
import {
  Message,
  MessageAvatar,
  MessageBubble,
  MessageBubbleContent,
  MessageContent,
  MessageFooter,
  MessageHeader,
} from '@/components/agents/message'
import { AgentTag, PrincipalAvatar } from '@/components/pichat/avatar'
import { ProfileTrigger, type ProfileSeed } from '@/components/pichat/profile-card'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { fileUrl } from '@/lib/api'
import { EASE_OUT } from '@/lib/ease'
import { copy, fmtSize, fmtTime } from '@/lib/format'
import { MarkdownText, type MentionTarget, type SeqRefOptions } from '@/lib/markdown'
import type { FileRef, Member, Message as Msg, Via } from '@/lib/types'
import { cn } from '@/lib/utils'
import { INLINE_IMG, RECALL_MS, snippet, type Pending } from './util'

export interface MessageItemProps {
  m: Msg
  grouped: boolean
  meId: string
  members: Member[]
  mentions: MentionTarget[]
  quoted?: Msg | null
  animate: boolean
  pending?: Pending
  /** Last message of a run from one author: carries the read receipt. */
  lastInRun?: boolean
  /** Clickable "#12" references in the text. */
  seqRefs?: SeqRefOptions
  /** Touch devices: the "⋯" button opens the action sheet (a long press stays native text selection). */
  onActions?: (m: Msg, canRecall: boolean) => void
  onReply: (m: Msg) => void
  onJump: (seq: number) => void
  onRecall: (m: Msg) => void
  onRetry: (opKey: string) => void
  onDiscard: (opKey: string) => void
}

const ENTER = {
  initial: { opacity: 0, y: 8, filter: 'blur(1px)' },
  animate: { opacity: 1, y: 0, filter: 'blur(0px)' },
  transition: { duration: 0.3, ease: EASE_OUT },
}

export function useRecallable(m: Msg, mine: boolean) {
  const left = RECALL_MS - (Date.now() - m.createdAt)
  const can = mine && m.seq > 0 && !m.recalled && left > 0
  const [, tick] = useState(0)
  useEffect(() => {
    if (!can) return
    const t = setTimeout(() => tick((n) => n + 1), left + 50)
    return () => clearTimeout(t)
  }, [can, left])
  return can
}

function Action({ label, onClick, children, danger }: { label: string; onClick: () => void; children: ReactNode; danger?: boolean }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={label}
          onClick={onClick}
          className={cn('text-muted-foreground hover:bg-accent', danger && 'hover:bg-destructive/10 hover:text-destructive')}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="bottom">{label}</TooltipContent>
    </Tooltip>
  )
}

function Files({ files, align }: { files: FileRef[]; align: 'start' | 'end' }) {
  const images = files.filter((f) => INLINE_IMG.test(f.mime))
  const others = files.filter((f) => !INLINE_IMG.test(f.mime))
  return (
    <div className={cn('flex max-w-full flex-col gap-1.5', align === 'end' ? 'items-end' : 'items-start')}>
      {images.length > 0 && (
        <div className={cn('flex flex-wrap gap-1.5', align === 'end' && 'justify-end')}>
          {images.map((f) => (
            <a key={f.id} href={fileUrl(f.id)} target="_blank" rel="noopener noreferrer" className="block overflow-hidden rounded-card shadow-hairline">
              <img src={fileUrl(f.id)} alt={f.name} loading="lazy" className="max-h-60 max-w-[min(100%,320px)] object-cover" />
            </a>
          ))}
        </div>
      )}
      {others.map((f) => (
        <a
          key={f.id}
          href={fileUrl(f.id)}
          download={f.name}
          className="flex max-w-full items-center gap-2 rounded-card border border-border bg-card px-3 py-2 transition-colors hover:bg-accent"
        >
          <span className="grid size-8 shrink-0 place-items-center rounded-control bg-tag text-brand">
            <FileText className="size-4" />
          </span>
          <span className="min-w-0">
            <span className="block truncate text-[13px] font-medium text-foreground">{f.name}</span>
            <span className="block text-[11.5px] text-muted-foreground">{fmtSize(f.size)}</span>
          </span>
        </a>
      ))}
    </div>
  )
}

function Quote({ seq, quoted, onJump, fill }: { seq: number; quoted?: Msg | null; onJump: (seq: number) => void; fill: boolean }) {
  return (
    <button
      type="button"
      onClick={() => onJump(seq)}
      className={cn(
        'relative flex min-w-0 items-center gap-1.5 rounded-control bg-accent/70 py-1 pr-2 pl-3 text-left text-[12px] text-muted-foreground transition-colors hover:bg-accent',
        'before:absolute before:inset-y-1.5 before:left-1 before:w-0.5 before:rounded-full before:bg-brand/45',
        // Humans: as wide as the bubble below (the grid column), never wider. Agents: up to 80% of the column.
        fill ? 'w-0 min-w-full' : 'w-fit max-w-[80%]',
      )}
    >
      <span className="shrink-0 font-medium text-body">{quoted?.author?.name ?? `#${seq}`}</span>
      <span className="min-w-0 truncate">{quoted ? snippet(quoted) : '…'}</span>
    </button>
  )
}

/** "后台会话 1a2b3c4d · project · host" under an agent message, plus a warning when it isn't the agent's main session. */
function Provenance({ via, mainSession }: { via: Via; mainSession?: string | null }) {
  const sid = via.session || ''
  const short = sid.slice(0, 8)
  const host = via.host || ''
  const parts =
    via.kind === 'run'
      ? ['后台会话' + (short ? ` ${short}` : ''), via.tier, host]
      : sid
        ? [`终端会话 ${short}`, host]
        : ['外部调用', host]
  const label = parts.filter(Boolean).join(' · ')
  const offMain = !!sid && !!mainSession && sid !== mainSession
  const body = (
    <span className="inline-flex min-w-0 items-center gap-1.5 text-[11.5px] text-muted-foreground">
      <span className="min-w-0 truncate font-mono tabular-nums">{label}</span>
      {offMain && (
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="shrink-0 rounded-chip bg-mention-me-light px-1 font-sans text-[10.5px] leading-4 font-medium text-warning">非主会话</span>
          </TooltipTrigger>
          <TooltipContent side="bottom" className="max-w-72">
            这条消息不是由该 agent 的主会话发出的（主会话 {mainSession.slice(0, 8)}）
          </TooltipContent>
        </Tooltip>
      )}
    </span>
  )
  if (!sid) return body
  return (
    <button
      type="button"
      title={`会话 ${sid}（点击复制）`}
      onClick={() => copy(sid, '已复制会话 ID')}
      className="-mx-1 flex max-w-full min-w-0 rounded-chip px-1 text-left transition-colors hover:bg-accent"
    >
      {body}
    </button>
  )
}

function Readers({ readers, className }: { readers: Member[]; className?: string }) {
  if (!readers.length) return null
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className={cn('inline-flex cursor-default items-center gap-0.5 text-[11px] text-muted-foreground', className)}>
          <CheckCheck className="size-3" />
          {readers.length} 人已读
        </span>
      </TooltipTrigger>
      <TooltipContent side="bottom" className="max-w-64">
        已读：{readers.map((x) => x.nick || x.displayName).join('、')}
      </TooltipContent>
    </Tooltip>
  )
}

export const MessageItem = memo(function MessageItem({
  m,
  grouped,
  meId,
  members,
  mentions,
  quoted,
  animate,
  pending,
  lastInRun,
  seqRefs,
  onActions,
  onReply,
  onJump,
  onRecall,
  onRetry,
  onDiscard,
}: MessageItemProps) {
  const a = m.author
  const mine = a?.id === meId
  const isAgent = a?.kind === 'agent'
  const canRecall = useRecallable(m, mine)
  const reduce = useReducedMotion()
  // Decided once: a confirmed optimistic row keeps its key, and dropping the props mid-animation freezes it.
  const [entering] = useState(animate)
  const motionProps = entering && !reduce ? ENTER : {}

  if (m.recalled) {
    return (
      <div data-seq={m.seq} className="py-1 text-center text-[12px] text-muted-foreground">
        {mine ? '你' : a?.name || '有人'}撤回了一条消息
      </div>
    )
  }

  const readers =
    lastInRun && m.seq > 0 ? members.filter((x) => x.id !== a?.id && x.id !== meId && (x.lastReadSeq || 0) >= m.seq) : []
  const author = members.find((x) => x.id === a?.id)
  const seed: ProfileSeed | null = a
    ? { id: a.id, kind: a.kind, name: a.name, platform: a.platform ?? author?.platform, ownerName: a.ownerName || author?.ownerName }
    : null
  const avatar = a ? (
    <ProfileTrigger seed={seed!} className="rounded-[9px]">
      <PrincipalAvatar id={a.id} name={a.name} kind={a.kind} platform={a.platform ?? author?.platform} size="md" />
    </ProfileTrigger>
  ) : null
  const authorName = (fallback: string) =>
    seed ? (
      <ProfileTrigger seed={seed} className="text-[13px] font-medium text-foreground hover:underline">
        {a?.name || fallback}
      </ProfileTrigger>
    ) : (
      <span className="text-[13px] font-medium text-foreground">{fallback}</span>
    )

  // Desktop hover row. Touch devices use the "⋯" button and the bottom sheet instead and never show it.
  const actions = !pending && (
    <span
      className={cn(
        'flex items-center gap-1 transition-opacity duration-150 pointer-coarse:hidden',
        'opacity-0 group-hover/message:opacity-100 group-focus-within/message:opacity-100',
        !isAgent && 'absolute bottom-0',
        !isAgent && (mine ? 'right-full mr-1' : 'left-full ml-1'),
      )}
    >
      {m.text && (
        <Action label="复制" onClick={() => copy(m.text)}>
          <Copy />
        </Action>
      )}
      <Action label="回复" onClick={() => onReply(m)}>
        <CornerUpLeft />
      </Action>
      {canRecall && (
        <Action label="撤回（2 分钟内）" danger onClick={() => onRecall(m)}>
          <MessageSquareX />
        </Action>
      )}
    </span>
  )

  // Touch only: opens the action sheet. A long press on the text is left to native selection.
  const more =
    !pending && onActions ? (
      <button
        type="button"
        aria-label="消息操作"
        onClick={() => onActions(m, canRecall)}
        className={cn(
          'hidden size-7 shrink-0 place-items-center rounded-full text-muted-foreground/70 active:bg-accent active:text-foreground pointer-coarse:grid [&_svg]:size-4',
          !isAgent && 'absolute bottom-0',
          !isAgent && (mine ? 'right-full mr-0.5' : 'left-full ml-0.5'),
        )}
      >
        <Ellipsis />
      </button>
    ) : null

  const text = m.text ? (
    <MarkdownText
      text={m.text}
      mentions={mentions}
      seqRefs={seqRefs}
      className={cn('msg-text text-[14px]', mine && 'md-on-dark')}
    />
  ) : null
  const quote = m.replyTo ? <Quote seq={m.replyTo} quoted={quoted} onJump={onJump} fill={!isAgent} /> : null
  const files = m.files?.length ? <Files files={m.files} align={mine ? 'end' : 'start'} /> : null

  return (
    <Message
      from={mine ? 'user' : 'assistant'}
      data-seq={m.seq > 0 ? m.seq : undefined}
      aria-label={`${a?.name ?? ''} ${fmtTime(m.createdAt)}`}
      {...motionProps}
      className={cn(
        'relative',
        grouped ? 'mt-1' : 'mt-4',
        pending?.status === 'sending' && 'opacity-60',
      )}
    >
      {grouped ? (
        <span
          aria-hidden="true"
          className="w-8 shrink-0 pt-1 text-center text-[10.5px] text-muted-foreground tabular-nums opacity-0 transition-opacity group-hover/message:opacity-100"
        >
          {fmtTime(m.createdAt)}
        </span>
      ) : (
        <MessageAvatar className="mt-0.5 size-8 overflow-visible rounded-none bg-transparent">{avatar}</MessageAvatar>
      )}
      <MessageContent className="gap-1">
        {!grouped && (
          <MessageHeader className="gap-1.5 text-[12px]">
            {mine ? (
              <>
                <span className="text-muted-foreground tabular-nums">{fmtTime(m.createdAt)}</span>
                {authorName('我')}
              </>
            ) : (
              <>
                {authorName('?')}
                {isAgent && <AgentTag />}
                {isAgent && (a?.ownerName || author?.ownerName) && (
                  <span className="text-muted-foreground">owner：{a?.ownerName || author?.ownerName}</span>
                )}
                <span className="text-muted-foreground tabular-nums">{fmtTime(m.createdAt)}</span>
              </>
            )}
          </MessageHeader>
        )}
        {isAgent ? (
          <div className="flex w-full min-w-0 flex-col items-start gap-1">
            {m.via && !grouped && <Provenance via={m.via} mainSession={author?.mainSession} />}
            {quote}
            {text && (
              <MessageBubble variant="ghost">
                <MessageBubbleContent className="max-w-full text-foreground">{text}</MessageBubbleContent>
              </MessageBubble>
            )}
            {files}
            {(actions || more || readers.length > 0) && (
              <div className="-mt-0.5 -mb-3 -ml-1.5 flex h-7 items-center gap-2 pointer-coarse:my-0">
                {readers.length > 0 && <Readers readers={readers} className="pl-1.5" />}
                {actions}
                {more}
              </div>
            )}
          </div>
        ) : (
          <div className={cn('relative grid max-w-[82%] min-w-0 gap-1', mine ? 'justify-items-end' : 'justify-items-start', quote && 'min-w-48')}>
            {quote}
            {text && (
              <MessageBubble variant={mine ? 'solid' : 'outline'}>
                <MessageBubbleContent className="max-w-full rounded-[12px] px-3.5 py-2 leading-normal">{text}</MessageBubbleContent>
              </MessageBubble>
            )}
            {files}
            {actions}
            {more}
          </div>
        )}
        {!isAgent && <Footer pending={pending} readers={readers} onRetry={onRetry} onDiscard={onDiscard} />}
        {isAgent && pending && <Footer pending={pending} readers={[]} onRetry={onRetry} onDiscard={onDiscard} />}
      </MessageContent>
    </Message>
  )
})

function Footer({
  pending,
  readers,
  onRetry,
  onDiscard,
}: {
  pending?: Pending
  readers: Member[]
  onRetry: (opKey: string) => void
  onDiscard: (opKey: string) => void
}) {
  if (!pending && !readers.length) return null
  return (
    <MessageFooter className="min-h-0 gap-1.5 px-0">
      {pending?.status === 'error' ? (
        <span className="flex items-center gap-1 text-destructive">
          <AlertCircle className="size-3.5" />
          <span className="text-[12px]">{pending.error || '发送失败'}</span>
          <Action label="重试" onClick={() => onRetry(pending.opKey)}>
            <RotateCcw />
          </Action>
          <Action label="删除" danger onClick={() => onDiscard(pending.opKey)}>
            <Trash2 />
          </Action>
        </span>
      ) : pending ? (
        <span className="text-[11px] text-muted-foreground">发送中…</span>
      ) : (
        <Readers readers={readers} />
      )}
    </MessageFooter>
  )
}
