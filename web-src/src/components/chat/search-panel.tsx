import { useEffect, useRef, useState } from 'react'
import { LoaderCircle, Search, X } from 'lucide-react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { api, errorText } from '@/lib/api'
import { EASE_OUT } from '@/lib/ease'
import { fmtTime } from '@/lib/format'
import { highlightParts, normalizeHit, searchQuery, type SearchHit } from '@/lib/search'
import { setState } from '@/lib/store'

export const SEARCH_PANEL_PX = 320
const DEBOUNCE_MS = 300
const MAX_QUERY = 100

type Result = { q: string; hits: SearchHit[] } | null

/** Input, debounced REST search and the hit list. Clicking a hit jumps to it through the store's `focus`. */
function SearchBody({ gid, autoFocus, onPicked }: { gid: string; autoFocus?: boolean; onPicked?: () => void }) {
  const [q, setQ] = useState('')
  const [res, setRes] = useState<Result>(null)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const input = useRef<HTMLInputElement>(null)

  useEffect(() => {
    setQ('')
    setRes(null)
    setErr('')
  }, [gid])

  useEffect(() => {
    const term = q.trim()
    if (!term) {
      setRes(null)
      setErr('')
      setLoading(false)
      return
    }
    const ac = new AbortController()
    setLoading(true)
    const t = setTimeout(() => {
      api<{ results?: Record<string, unknown>[]; hits?: Record<string, unknown>[] }>(
        'GET',
        `/groups/${encodeURIComponent(gid)}/search?${searchQuery(term, { limit: 50 })}`,
        undefined,
        { signal: ac.signal },
      ).then(
        (r) => {
          const raw = r.results ?? r.hits ?? []
          setRes({ q: term, hits: raw.map(normalizeHit).filter((h): h is SearchHit => !!h) })
          setErr('')
          setLoading(false)
        },
        (e) => {
          if (ac.signal.aborted) return
          setErr(errorText(e))
          setLoading(false)
        },
      )
    }, DEBOUNCE_MS)
    return () => {
      clearTimeout(t)
      ac.abort()
    }
  }, [q, gid])

  const pick = (seq: number) => {
    setState({ focus: { gid, seq } })
    onPicked?.()
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 p-3">
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            ref={input}
            autoFocus={autoFocus}
            value={q}
            maxLength={MAX_QUERY}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && res?.hits[0]) pick(res.hits[0].seq)
            }}
            placeholder="搜索这个群的消息"
            aria-label="搜索消息"
            className="bg-background pr-8 pl-8"
          />
          {loading ? (
            <LoaderCircle className="absolute top-1/2 right-2.5 size-4 -translate-y-1/2 animate-spin text-muted-foreground" aria-label="搜索中" />
          ) : (
            q && (
              <button
                type="button"
                aria-label="清空"
                onClick={() => {
                  setQ('')
                  input.current?.focus()
                }}
                className="absolute top-1/2 right-1.5 grid size-6 -translate-y-1/2 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
              >
                <X className="size-3.5" />
              </button>
            )
          )}
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3" aria-live="polite">
        {err ? (
          <p className="px-2 py-6 text-center text-[13px] text-destructive">{err}</p>
        ) : !q.trim() ? (
          <p className="px-2 py-6 text-center text-[13px] text-muted-foreground">输入关键词，按作者、时间和 #编号 列出匹配的消息</p>
        ) : res && !res.hits.length ? (
          <p className="px-2 py-6 text-center text-[13px] text-muted-foreground">没有找到相关消息</p>
        ) : res ? (
          <>
            <div className="px-2 pb-1 text-[12px] text-muted-foreground">
              <span className="tabular-nums">{res.hits.length}</span> 条结果
            </div>
            <ul className="flex flex-col gap-0.5">
              {res.hits.map((h) => (
                <li key={h.seq}>
                  <button
                    type="button"
                    onClick={() => pick(h.seq)}
                    className="flex w-full flex-col gap-1 rounded-control px-2 py-2 text-left hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
                  >
                    <span className="flex min-w-0 items-baseline gap-2 text-[12.5px]">
                      <span className="min-w-0 truncate font-medium text-foreground">{h.author}</span>
                      <span className="shrink-0 text-muted-foreground tabular-nums">{fmtTime(h.createdAt)}</span>
                      <span className="ml-auto shrink-0 font-mono text-[12px] text-muted-foreground tabular-nums">#{h.seq}</span>
                    </span>
                    <span className="line-clamp-3 text-[13px] break-words text-body">
                      {highlightParts(h.snippet, res.q).map((p, i) =>
                        p.match ? (
                          <mark key={i} className="rounded-[3px] bg-mention-me-light px-0.5 text-foreground">
                            {p.text}
                          </mark>
                        ) : (
                          <span key={i}>{p.text}</span>
                        ),
                      )}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </>
        ) : null}
      </div>
    </div>
  )
}

/** Desktop: a side column in the chat pane (shares the slot with the member panel). Mobile: a full-screen sheet. */
export function SearchPanel({ gid, open, onOpenChange, wide }: { gid: string; open: boolean; onOpenChange: (open: boolean) => void; wide: boolean }) {
  const reduce = useReducedMotion()
  if (wide)
    return (
      <AnimatePresence>
        {open && (
          <motion.aside
            key="search"
            aria-label="搜索消息"
            initial={reduce ? { opacity: 0 } : { opacity: 0, x: 24, filter: 'blur(4px)' }}
            animate={{ opacity: 1, x: 0, filter: 'blur(0px)' }}
            exit={reduce ? { opacity: 0 } : { opacity: 0, x: 16, filter: 'blur(2px)', transition: { duration: 0.15, ease: EASE_OUT } }}
            transition={{ duration: 0.4, ease: EASE_OUT }}
            className="shrink-0 border-l border-border"
            style={{ width: SEARCH_PANEL_PX }}
            onKeyDown={(e) => e.key === 'Escape' && onOpenChange(false)}
          >
            <div className="flex h-full flex-col" style={{ width: SEARCH_PANEL_PX }}>
              <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border pr-2 pl-4">
                <h2 className="flex-1 text-[14px] font-medium text-foreground">搜索消息</h2>
                <Button variant="ghost" size="icon-sm" className="text-muted-foreground" aria-label="关闭" onClick={() => onOpenChange(false)}>
                  <X />
                </Button>
              </div>
              <SearchBody gid={gid} autoFocus />
            </div>
          </motion.aside>
        )}
      </AnimatePresence>
    )
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full max-w-none gap-0 bg-card p-0 data-[side=right]:w-full data-[side=right]:sm:max-w-none">
        <SheetHeader className="border-b border-border px-4 py-3">
          <SheetTitle className="text-[14px]">搜索消息</SheetTitle>
          <SheetDescription className="sr-only">在这个群的历史消息里搜索</SheetDescription>
        </SheetHeader>
        <SearchBody gid={gid} autoFocus onPicked={() => onOpenChange(false)} />
      </SheetContent>
    </Sheet>
  )
}
