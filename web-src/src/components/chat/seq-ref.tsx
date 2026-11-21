import { createContext, useContext, useMemo, type ReactNode } from 'react'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { SeqRefOptions } from '@/lib/markdown'
import { navigate } from '@/lib/router'
import { seqRefSplitter } from '@/lib/seq-refs'
import { loadQuote, useStore } from '@/lib/store'
import { snippet } from './util'

/** The open chat and its scroll-to-message, for "#12" links inside message text. */
export const SeqRefContext = createContext<{ gid: string; jump: (seq: number) => void } | null>(null)

/** Splitter for this chat: bare "#12" is this group; "「群名」#12" / "群名 群 #12" are my other groups. */
export function useSeqRefs(gid: string): SeqRefOptions {
  const groups = useStore((s) => s.groups)
  // Only names matter; a fresh summaries array with the same names keeps the splitter (and the rendered markdown).
  const names = useMemo(
    () => JSON.stringify(groups.filter((g) => g.kind === 'group').map((g) => [g.id, g.name])),
    [groups],
  )
  return useMemo(() => {
    const list = (JSON.parse(names) as [string, string][]).map(([id, name]) => ({ id, name }))
    return { split: seqRefSplitter(list, gid), Link: SeqRefLink }
  }, [names, gid])
}

const EMPTY: never[] = []

/** "#12": jumps to (and flashes) the message, or opens the other group at it. Above lastSeq it is plain text. */
function SeqRefLink({ gid, seq, children }: { gid: string; seq: number; children: ReactNode }) {
  const ctx = useContext(SeqRefContext)
  const here = !!ctx && ctx.gid === gid
  const lastSeq = useStore((s) => {
    const own = s.chats[gid]?.msgs.at(-1)?.seq ?? 0
    const info = s.info[gid]?.lastSeq ?? 0
    const summary = s.groups.find((g) => g.id === gid)?.lastSeq ?? 0
    return Math.max(own, info, summary)
  })
  const msgs = useStore((s) => s.chats[gid]?.msgs ?? EMPTY)
  const quoted = useStore((s) => s.chats[gid]?.quotes[seq])
  const groupName = useStore((s) => (here ? '' : s.groups.find((g) => g.id === gid)?.name || ''))
  if (!ctx || seq > lastSeq) return <>{children}</>
  const target = msgs.find((m) => m.seq === seq) ?? quoted
  const open = () => {
    if (here) ctx.jump(seq)
    else navigate(`#/g/${encodeURIComponent(gid)}?seq=${seq}`)
  }
  return (
    <Tooltip onOpenChange={(o) => o && loadQuote(gid, seq)}>
      <TooltipTrigger asChild>
        <a
          href={`#/g/${encodeURIComponent(gid)}?seq=${seq}`}
          data-seq-ref={seq}
          onClick={(e) => {
            if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return
            e.preventDefault()
            open()
          }}
          className="seq-ref"
        >
          {children}
        </a>
      </TooltipTrigger>
      <TooltipContent side="top" className="max-w-80 flex-col items-start gap-0.5 text-left">
        {groupName && <span className="text-[11px] opacity-70">{groupName}</span>}
        {target ? (
          target.recalled ? (
            <span>这条消息已撤回</span>
          ) : (
            <span className="min-w-0">
              <span className="font-medium">{target.author?.name ?? '系统'}：</span>
              {snippet(target, 80) || '（无文字）'}
            </span>
          )
        ) : (
          <span className="opacity-70">加载中…</span>
        )}
      </TooltipContent>
    </Tooltip>
  )
}
