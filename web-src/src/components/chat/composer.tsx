import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { AtSign, ChevronDown, ImageIcon, Paperclip, X } from 'lucide-react'
import { PromptInput } from '@/components/agents/prompt-input'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/motion/dropdown-menu'
import { AttachmentUpload, type AttachmentUploadItem } from '@/components/motion/attachment-upload'
import { AgentTag, PrincipalAvatar } from '@/components/pichat/avatar'
import { AgentStateChip } from '@/components/pichat/agent-state'
import { RoleBadge } from '@/components/pichat/role-badge'
import { Button } from '@/components/ui/button'
import { Command, CommandItem, CommandList } from '@/components/ui/command'
import { Popover, PopoverAnchor, PopoverContent } from '@/components/ui/popover'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { api, errorText } from '@/lib/api'
import { randId } from '@/lib/format'
import { agentStateOf, terminalModeOf, notify } from '@/lib/store'
import type { FileRef, Group, Member, Message } from '@/lib/types'
import { cn } from '@/lib/utils'
import { isCoarse, MAX_FILE_SIZE, MAX_FILES, memberName, snippet } from './util'

export interface Outgoing {
  text: string
  mentions: string[]
  replyTo?: number
  files: FileRef[]
}

export interface ComposerHandle {
  addFiles: (files: File[]) => void
  focus: () => void
}

interface ComposerProps {
  gid: string
  info: Group
  meId: string
  reply: Message | null
  onClearReply: () => void
  onSend: (o: Outgoing) => void
  onShowMembers: () => void
  workingIds: Set<string>
}

type Candidate = { id: string; nick: string; all?: boolean; member?: Member }
type Pop = { q: string; start: number }

const draftKey = (gid: string) => 'draft:' + gid

export const Composer = forwardRef<ComposerHandle, ComposerProps>(function Composer(
  { gid, info, meId, reply, onClearReply, onSend, onShowMembers, workingIds },
  ref,
) {
  const [text, setText] = useState(() => sessionStorage.getItem(draftKey(gid)) || '')
  const [items, setItems] = useState<AttachmentUploadItem[]>([])
  const uploaded = useRef(new Map<string, FileRef>())
  const aborts = useRef(new Map<string, AbortController>())
  const [pop, setPop] = useState<Pop | null>(null)
  const [sel, setSel] = useState('')
  const ta = useRef<HTMLTextAreaElement>(null)
  const fileIn = useRef<HTMLInputElement>(null)
  const imageIn = useRef<HTMLInputElement>(null)
  const picked = useRef(new Map<string, string>())
  const coarse = useMemo(isCoarse, [])

  useEffect(() => {
    sessionStorage.setItem(draftKey(gid), text)
  }, [gid, text])

  const itemsRef = useRef(items)
  itemsRef.current = items
  useEffect(
    () => () => {
      for (const it of itemsRef.current) if (it.previewUrl) URL.revokeObjectURL(it.previewUrl)
      for (const ac of aborts.current.values()) ac.abort()
      aborts.current.clear()
    },
    [],
  )

  // ---- attachments ----

  const uploadOne = useCallback(async (id: string, file: File) => {
    aborts.current.get(id)?.abort()
    const ac = new AbortController()
    aborts.current.set(id, ac)
    setItems((xs) => xs.map((x) => (x.id === id ? { ...x, status: 'uploading', error: undefined } : x)))
    try {
      const fd = new FormData()
      fd.append('file', file)
      const { file: f } = await api<{ file: FileRef }>('POST', '/files', fd, { signal: ac.signal })
      if (ac.signal.aborted) return
      aborts.current.delete(id)
      uploaded.current.set(id, f)
      setItems((xs) => xs.map((x) => (x.id === id ? { ...x, status: 'complete' } : x)))
      setTimeout(() => setItems((xs) => xs.map((x) => (x.id === id && x.status === 'complete' ? { ...x, status: 'idle' } : x))), 1000)
    } catch (e) {
      // Removed by the user (or superseded by a retry): the chip is gone, nothing to report.
      if (ac.signal.aborted) return
      aborts.current.delete(id)
      setItems((xs) => xs.map((x) => (x.id === id ? { ...x, status: 'failed', error: errorText(e) } : x)))
    }
  }, [])

  const addFiles = useCallback(
    (files: File[]) => {
      const room = MAX_FILES - itemsRef.current.length
      if (room <= 0) {
        notify(`一条消息最多 ${MAX_FILES} 个附件`)
        return
      }
      if (files.length > room) notify(`一条消息最多 ${MAX_FILES} 个附件`)
      const added: AttachmentUploadItem[] = []
      for (const file of files.slice(0, room)) {
        if (file.size > MAX_FILE_SIZE) {
          notify(`${file.name} 超过 10 MB`)
          continue
        }
        const image = file.type.startsWith('image/')
        added.push({
          id: randId(),
          name: file.name || '图片.png',
          kind: image ? 'image' : 'file',
          size: file.size,
          previewUrl: image ? URL.createObjectURL(file) : undefined,
          status: 'uploading',
          file,
        })
      }
      if (!added.length) return
      setItems((xs) => [...xs, ...added])
      for (const it of added) uploadOne(it.id, it.file!)
    },
    [uploadOne],
  )

  const onItemsChange = (next: AttachmentUploadItem[]) => {
    const keep = new Set(next.map((x) => x.id))
    for (const it of items)
      if (!keep.has(it.id)) {
        aborts.current.get(it.id)?.abort()
        aborts.current.delete(it.id)
        uploaded.current.delete(it.id)
        if (it.previewUrl) URL.revokeObjectURL(it.previewUrl)
      }
    setItems(next)
  }

  useImperativeHandle(ref, () => ({ addFiles, focus: () => ta.current?.focus() }), [addFiles])

  // ---- @ mentions ----

  const canAll = info.kind === 'group' && (info.myRole === 'owner' || info.myRole === 'admin')
  const candidates = useMemo<Candidate[]>(() => {
    if (!pop) return []
    const q = pop.q.toLowerCase()
    const list: Candidate[] = info.members
      .filter((m) => m.id !== meId && ((m.nick || '').toLowerCase().includes(q) || (m.displayName || '').toLowerCase().includes(q)))
      .map((m) => ({ id: m.id, nick: m.nick || m.displayName, member: m }))
    if (canAll && 'all'.startsWith(q)) list.unshift({ id: '*all', nick: 'all', all: true })
    return list.slice(0, 8)
  }, [pop, info.members, meId, canAll])

  useEffect(() => {
    if (candidates.length && !candidates.some((c) => c.id === sel)) setSel(candidates[0].id)
  }, [candidates, sel])

  const detect = (el: HTMLTextAreaElement) => {
    const before = el.value.slice(0, el.selectionStart)
    // @ after start, whitespace or any non-ASCII-word char ("请@"), but not inside an email like a@b.
    const mt = /(?:^|[^A-Za-z0-9_.])@([^\s@]{0,32})$/.exec(before)
    setPop(mt ? { q: mt[1], start: el.selectionStart - mt[1].length - 1 } : null)
  }

  const pick = (c: Candidate) => {
    const el = ta.current
    if (!el || !pop) return
    const end = el.selectionStart
    const ins = `@${c.nick} `
    const v = el.value.slice(0, pop.start) + ins + el.value.slice(end)
    if (!c.all) picked.current.set(c.nick, c.id)
    // Update the DOM synchronously so keys typed right after picking land after the mention.
    el.value = v
    el.focus()
    el.setSelectionRange(pop.start + ins.length, pop.start + ins.length)
    setText(v)
    setPop(null)
  }

  const insertAt = () => {
    const el = ta.current
    if (!el) return
    const s = el.selectionStart
    const lead = s > 0 && !/\s/.test(el.value[s - 1]) ? ' @' : '@'
    const v = el.value.slice(0, s) + lead + el.value.slice(el.selectionEnd)
    el.value = v
    el.focus()
    el.setSelectionRange(s + lead.length, s + lead.length)
    setText(v)
    detect(el)
  }

  /** Insert "@nick " at the caret, e.g. from the agent chip. */
  const mention = (m: Member) => {
    const el = ta.current
    if (!el) return
    const nick = memberName(m)
    const s = el.selectionStart
    const lead = s > 0 && !/\s/.test(el.value[s - 1]) ? ' ' : ''
    const ins = `${lead}@${nick} `
    const v = el.value.slice(0, s) + ins + el.value.slice(el.selectionEnd)
    picked.current.set(nick, m.id)
    el.value = v
    el.focus()
    el.setSelectionRange(s + ins.length, s + ins.length)
    setText(v)
  }

  // ---- send ----

  const uploading = items.some((x) => x.status === 'uploading')
  const ready = items.filter((x) => uploaded.current.has(x.id))
  const failed = items.filter((x) => x.status === 'failed').length
  // Failed chips keep the button enabled so a click explains why nothing was sent.
  const canSend = !!text.trim() || ready.length > 0 || failed > 0

  const send = () => {
    const body = text.trim()
    if (uploading) {
      notify('附件上传中')
      return
    }
    if (failed) {
      notify(`有 ${failed} 个附件上传失败：删除或重试后再发送`)
      return
    }
    if (!body && !ready.length) return
    const mentions = [...picked.current].filter(([nick]) => body.includes('@' + nick)).map(([, id]) => id)
    onSend({ text: body, mentions, replyTo: reply?.seq, files: ready.map((x) => uploaded.current.get(x.id)!) })
    for (const it of items) if (it.previewUrl) URL.revokeObjectURL(it.previewUrl)
    uploaded.current.clear()
    picked.current.clear()
    setItems([])
    setText('')
    setPop(null)
    onClearReply()
  }

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (pop && candidates.length) {
      const i = Math.max(0, candidates.findIndex((c) => c.id === sel))
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault()
        const n = candidates.length
        setSel(candidates[(i + (e.key === 'ArrowDown' ? 1 : n - 1)) % n].id)
        return
      }
      if ((e.key === 'Enter' && !e.nativeEvent.isComposing) || e.key === 'Tab') {
        e.preventDefault()
        pick(candidates[i])
        return
      }
      if (e.key === 'Escape') {
        e.preventDefault()
        setPop(null)
        return
      }
    }
    if (e.key === 'Escape' && reply) onClearReply()
  }

  const agents = info.members.filter((m) => m.kind === 'agent')
  const online = agents.filter((m) => m.online).length

  const header = (
    <>
      {reply && (
        <div className="mx-1 mb-1 flex items-center gap-2 rounded-control bg-accent/70 py-1 pr-1 pl-2.5 text-[12.5px] text-muted-foreground animate-fade-up">
          <span className="min-w-0 flex-1 truncate">
            回复 <b className="font-medium text-body">{reply.author?.name}</b>：{snippet(reply)}
          </span>
          <Button type="button" variant="ghost" size="icon-xs" aria-label="取消回复" onClick={onClearReply}>
            <X />
          </Button>
        </div>
      )}
      {items.length > 0 && (
        <AttachmentUpload
          dropzone={false}
          value={items}
          onValueChange={onItemsChange}
          onRetry={(it) => it.file && uploadOne(it.id, it.file)}
          maxFiles={MAX_FILES}
          maxFileSize={MAX_FILE_SIZE}
          className="mb-1 max-h-48 overflow-y-auto px-1"
          classNames={{ row: 'min-h-11 rounded-control bg-accent/60 p-0.5 [&>div]:rounded-[6px] [&>div]:bg-card' }}
        />
      )}
    </>
  )

  return (
    // Same symmetric scrollbar gutter as the message list, so both columns line up.
    <div className="shrink-0 overflow-y-hidden [scrollbar-gutter:stable_both-edges]">
    <div className="chat-column px-3 pb-3 min-[900px]:px-4 min-[900px]:pb-4">
      <input type="file" multiple hidden ref={fileIn} onChange={(e) => (addFiles([...(e.target.files ?? [])]), (e.target.value = ''))} />
      <input
        type="file"
        accept="image/*"
        multiple
        hidden
        ref={imageIn}
        onChange={(e) => (addFiles([...(e.target.files ?? [])]), (e.target.value = ''))}
      />
      <Popover open={!!pop && candidates.length > 0} onOpenChange={(o) => !o && setPop(null)}>
        <PopoverAnchor asChild>
          <div className="relative">
            <PromptInput
              value={text}
              onValueChange={(v) => {
                setText(v)
                if (ta.current) detect(ta.current)
              }}
              onSubmit={send}
              canSubmit={canSend && !uploading}
              submitOnEnter={!coarse}
              minRows={1}
              maxRows={8}
              textareaRef={ta}
              header={header}
              placeholder={coarse ? '输入消息' : '输入消息，Enter 发送，Shift+Enter 换行，@ 提及'}
              aria-label="消息"
              onKeyDown={onKeyDown}
              onClick={(e) => detect(e.currentTarget)}
              onBlur={() => setTimeout(() => setPop(null), 150)}
              onPaste={(e) => {
                const fs = [...(e.clipboardData?.files ?? [])]
                if (fs.length) {
                  e.preventDefault()
                  addFiles(fs)
                }
              }}
              actions={[
                { value: 'file', label: '上传附件', description: `单个 10 MB 以内，最多 ${MAX_FILES} 个`, icon: <Paperclip /> },
                { value: 'image', label: '上传图片', description: '也可以直接粘贴或拖进来', icon: <ImageIcon /> },
              ]}
              onAction={(v) => (v === 'image' ? imageIn : fileIn).current?.click()}
              leadingAction={
                <>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="rounded-control text-muted-foreground hover:bg-accent"
                    type="button"
                    aria-label="提及成员"
                    title="提及成员"
                    onClick={insertAt}
                  >
                    <AtSign />
                  </Button>
                  {agents.length > 0 && (
                    <DropdownMenu>
                      <DropdownMenuTrigger>
                        <button
                          type="button"
                          className="ml-1 inline-flex h-7 items-center gap-1.5 rounded-control border border-border px-2 text-[12px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground aria-expanded:bg-accent"
                        >
                          <span className={cn('size-1.5 rounded-full', online ? 'bg-online' : 'bg-muted-foreground/40')} />
                          <span className="tabular-nums">
                            {online}/{agents.length}
                          </span>
                          个 agent 在线
                          <ChevronDown className="size-3.5" />
                        </button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent side="top" className="min-w-64">
                        <DropdownMenuLabel>群里的 agent</DropdownMenuLabel>
                        {agents.map((m) => (
                          <DropdownMenuItem key={m.id} textValue={memberName(m)} onSelect={() => mention(m)}>
                            <span className="flex min-w-0 flex-1 items-center gap-2">
                              <PrincipalAvatar
                                id={m.id}
                                name={memberName(m)}
                                kind="agent"
                                platform={m.platform}
                                size="xs"
                                online={m.online && !workingIds.has(m.id)}
                                working={workingIds.has(m.id)}
                              />
                              <span className="min-w-0 truncate">{memberName(m)}</span>
                              <AgentStateChip state={agentStateOf(m, workingIds.has(m.id))} terminal={terminalModeOf(m)} className="ml-auto text-[11.5px]" />
                            </span>
                          </DropdownMenuItem>
                        ))}
                        <DropdownMenuSeparator />
                        <DropdownMenuItem onSelect={onShowMembers}>查看全部成员</DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  )}
                </>
              }
              className="rounded-card border-border bg-card px-2 pt-2 pb-1.5 transition-[border-color,box-shadow] focus-within:border-input focus-within:shadow-[0_2px_10px_-4px_rgb(37_37_34/0.10)]"
            />
            {uploading && (
              // The disabled send button gets no hover events, so this sits over it to carry the tooltip.
              <Tooltip>
                <TooltipTrigger asChild>
                  <span
                    role="img"
                    aria-label="附件上传中"
                    data-testid="send-uploading"
                    className="absolute right-2 bottom-1.5 size-8 cursor-not-allowed rounded-full"
                  />
                </TooltipTrigger>
                <TooltipContent side="top">附件上传中</TooltipContent>
              </Tooltip>
            )}
          </div>
        </PopoverAnchor>
        <PopoverContent
          side="top"
          align="start"
          sideOffset={6}
          onOpenAutoFocus={(e) => e.preventDefault()}
          onCloseAutoFocus={(e) => e.preventDefault()}
          onInteractOutside={(e) => {
            if (e.target === ta.current) e.preventDefault()
          }}
          className="w-(--radix-popper-anchor-width) rounded-card border border-border bg-card p-0 shadow-overlay ring-0 data-open:animate-pop-in sm:w-72"
        >
          <Command value={sel} onValueChange={setSel} shouldFilter={false} className="rounded-card! bg-card p-1">
            <div className="flex items-center justify-between px-2 pt-1 pb-1.5 text-[11.5px] text-muted-foreground">
              <span>提及成员</span>
              {!coarse && (
                <span className="flex items-center gap-1">
                  <Kbd>↑↓</Kbd>
                  <Kbd>Enter</Kbd>
                </span>
              )}
            </div>
            <CommandList>
              {candidates.map((c) => (
                <CommandItem
                  key={c.id}
                  value={c.id}
                  onSelect={() => pick(c)}
                  onMouseDown={(e) => e.preventDefault()}
                  className="gap-2 rounded-control px-2 py-1.5 [&>svg:last-child]:hidden"
                >
                  {c.all ? (
                    <>
                      <span className="grid size-6 place-items-center rounded-[7px] bg-mention-me-light text-mention-me">
                        <AtSign className="size-3.5" />
                      </span>
                      <span className="text-[13.5px] font-medium">all</span>
                      <span className="truncate text-[12px] text-muted-foreground">唤醒群里所有 agent</span>
                    </>
                  ) : (
                    <MemberLine m={c.member!} working={workingIds.has(c.id)} />
                  )}
                </CommandItem>
              ))}
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </div>
    </div>
  )
})

function Kbd({ children }: { children: string }) {
  return <kbd className="rounded-[4px] border border-border bg-background px-1 font-sans text-[10.5px] leading-4">{children}</kbd>
}

export function MemberLine({ m, working, stacked }: { m: Member; working?: boolean; stacked?: boolean }) {
  const name = memberName(m)
  const state = m.kind === 'agent' ? agentStateOf(m, working) : null
  const avatar = (
    <PrincipalAvatar
      id={m.id}
      name={name}
      kind={m.kind}
      platform={m.platform}
      size={stacked ? 'md' : 'sm'}
      online={state === 'idle' || state === 'attached'}
      working={state === 'running'}
    />
  )
  if (stacked) {
    return (
      <>
        {avatar}
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="min-w-0 truncate text-[13.5px] font-medium text-foreground">{name}</span>
            {m.kind === 'agent' && <AgentTag className="shrink-0" />}
            {m.role !== 'member' && <RoleBadge role={m.role} className="shrink-0" />}
          </span>
          {m.kind === 'agent' && (
            <span className="flex min-w-0 items-center gap-1.5 text-[12px] text-muted-foreground">
              {state && <AgentStateChip state={state} terminal={terminalModeOf(m)} />}
              {m.ownerName && <span className="min-w-0 truncate">· owner：{m.ownerName}</span>}
              {m.notice && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span
                      role="img"
                      aria-label={m.notice}
                      tabIndex={0}
                      className="size-2 shrink-0 rounded-full bg-warning outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    />
                  </TooltipTrigger>
                  <TooltipContent side="bottom" className="max-w-72">
                    {m.notice}
                  </TooltipContent>
                </Tooltip>
              )}
            </span>
          )}
        </span>
      </>
    )
  }
  return (
    <>
      {avatar}
      <span className="max-w-[60%] shrink-0 truncate text-[13.5px] font-medium text-foreground">{name}</span>
      {m.kind === 'agent' && <AgentTag className="shrink-0" />}
      {m.kind === 'agent' && m.ownerName && <span className="min-w-0 truncate text-[12px] text-muted-foreground">owner：{m.ownerName}</span>}
      {state && state !== 'idle' && <AgentStateChip state={state} className="shrink-0" />}
      {m.role !== 'member' && <RoleBadge role={m.role} className="ml-auto" />}
    </>
  )
}
