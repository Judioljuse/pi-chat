import { useEffect, useState, type ReactNode } from 'react'
import { Copy, MessageCircle, Settings2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { api, ApiError } from '@/lib/api'
import { startDm } from '@/lib/dm'
import { copy, fmtTime, PLATFORM, PLATFORM_LIMITS } from '@/lib/format'
import { useMedia } from '@/lib/hooks/use-media'
import { navigate } from '@/lib/router'
import { agentStateOf, fail, terminalModeOf, useStore, workingAgentIds } from '@/lib/store'
import type { PrincipalProfile } from '@/lib/types'
import { cn } from '@/lib/utils'
import { AgentStateChip } from './agent-state'
import { AgentTag, PrincipalAvatar } from './avatar'

/** What the caller already knows about the principal, shown while GET /principals/{id} loads. */
export interface ProfileSeed {
  id: string
  kind: 'human' | 'agent'
  name: string
  platform?: string | null
  ownerName?: string
}

/**
 * Wraps an avatar or name so a click opens the principal's profile card:
 * a popover on desktop, a bottom sheet on narrow or touch screens.
 */
export function ProfileTrigger({
  seed,
  children,
  className,
  label,
}: {
  seed: ProfileSeed
  children: ReactNode
  className?: string
  label?: string
}) {
  const [open, setOpen] = useState(false)
  const desktop = useMedia('(min-width: 900px) and (pointer: fine)')
  const trigger = (
    <button
      type="button"
      aria-label={label ?? `查看 ${seed.name} 的资料`}
      className={cn('cursor-pointer rounded-chip text-left outline-none focus-visible:ring-2 focus-visible:ring-ring', className)}
      onClick={desktop ? undefined : () => setOpen(true)}
    >
      {children}
    </button>
  )
  if (desktop)
    return (
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>{trigger}</PopoverTrigger>
        <PopoverContent
          align="start"
          sideOffset={6}
          className="w-80 rounded-card border border-border bg-card p-0 shadow-overlay ring-0 data-open:animate-pop-in"
        >
          {open && <ProfileBody seed={seed} onDone={() => setOpen(false)} />}
        </PopoverContent>
      </Popover>
    )
  return (
    <>
      {trigger}
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent side="bottom" className="max-h-[85dvh] gap-0 overflow-y-auto rounded-t-card bg-card p-0">
          <SheetHeader className="sr-only">
            <SheetTitle>{seed.name}</SheetTitle>
            <SheetDescription>资料卡</SheetDescription>
          </SheetHeader>
          {open && <ProfileBody seed={seed} onDone={() => setOpen(false)} />}
        </SheetContent>
      </Sheet>
    </>
  )
}

function ProfileBody({ seed, onDone }: { seed: ProfileSeed; onDone: () => void }) {
  const meId = useStore((s) => s.me?.id)
  const working = useStore((s) => workingAgentIds(s).has(seed.id))
  const [p, setP] = useState<PrincipalProfile | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    const ac = new AbortController()
    api<{ principal?: PrincipalProfile } & Partial<PrincipalProfile>>('GET', `/principals/${encodeURIComponent(seed.id)}`, undefined, { signal: ac.signal })
      .then((r) => setP(r.principal ?? (r as PrincipalProfile)))
      .catch((e) => {
        if (ac.signal.aborted) return
        setErr(e instanceof ApiError && (e.status === 403 || e.status === 404) ? '看不到这个人的资料' : '资料加载失败')
      })
    return () => ac.abort()
  }, [seed.id])

  const kind = p?.kind ?? seed.kind
  const name = p?.displayName || seed.name
  const platform = p?.platform ?? seed.platform
  const ownerName = p?.ownerName || seed.ownerName
  const isAgent = kind === 'agent'
  const mine = !!meId && !!p?.ownerId && p.ownerId === meId
  const self = seed.id === meId
  const state = isAgent && p ? agentStateOf(p, working) : null

  const dm = async () => {
    setBusy(true)
    try {
      await startDm(seed.id)
      onDone()
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
    }
  }
  const manage = () => {
    onDone()
    navigate(`#/agents/${encodeURIComponent(seed.id)}`)
  }

  const rows: [string, ReactNode][] = []
  if (p && isAgent) {
    if (ownerName) rows.push(['owner', ownerName])
    if (state) rows.push(['状态', <AgentStateChip state={state} terminal={terminalModeOf(p)} />])
    if (p.mainSession)
      rows.push([
        '主会话',
        <button
          type="button"
          onClick={() => copy(p.mainSession!, '已复制会话 ID')}
          title="复制完整会话 ID"
          className="-mx-1 inline-flex items-center gap-1 rounded-chip px-1 font-mono text-[12px] transition-colors hover:bg-accent"
        >
          {p.mainSession.slice(0, 8)}
          <Copy className="size-3 text-muted-foreground" />
        </button>,
      ])
    if (p.host) rows.push(['主机', <span className="font-mono text-[12px]">{p.host}</span>])
    if (p.dirs?.main) rows.push(['工作目录', <span className="font-mono text-[12px]" title={p.dirs.main}>{p.dirs.main}</span>])
    if (state === 'offline') rows.push(['最后在线', p.lastSeenAt ? fmtTime(p.lastSeenAt) : '从未上线'])
  }
  const common = p?.commonGroups ?? []

  return (
    <div className="flex flex-col">
      <div className="flex items-start gap-3 p-4 pb-3">
        <PrincipalAvatar
          id={seed.id}
          name={name}
          kind={kind}
          platform={platform}
          size="lg"
          working={state === 'running'}
          online={state === 'idle' || state === 'attached'}
        />
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            <span className="min-w-0 truncate text-[15px] font-medium text-foreground">{name}</span>
            {isAgent && <AgentTag />}
            {isAgent && platform && (
              <span className="rounded-chip bg-tag px-1.5 py-px text-[11px] text-brand">{PLATFORM[platform] || platform}</span>
            )}
          </div>
          {!isAgent && p?.name && <span className="truncate text-[12.5px] text-muted-foreground">@{p.name}</span>}
          {isAgent && ownerName && !p && <span className="truncate text-[12.5px] text-muted-foreground">owner：{ownerName}</span>}
        </div>
      </div>

      <div className="flex flex-col gap-3 px-4 pb-3">
        {!p && !err && (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-3 w-48" />
            <Skeleton className="h-3 w-32" />
          </div>
        )}
        {err && <p className="text-[12.5px] text-muted-foreground">{err}</p>}
        {p?.intro && <p className="text-[13px] leading-relaxed whitespace-pre-wrap break-words text-body">{p.intro}</p>}
        {isAgent && platform && PLATFORM_LIMITS[platform] && (
          <p className="text-[12px] leading-relaxed text-muted-foreground">{PLATFORM_LIMITS[platform]}</p>
        )}
        {rows.length > 0 && (
          <dl className="flex flex-col gap-1.5 text-[12.5px]">
            {rows.map(([k, v]) => (
              <div key={k} className="flex min-w-0 items-center gap-3">
                <dt className="w-14 shrink-0 text-muted-foreground">{k}</dt>
                <dd className="min-w-0 flex-1 truncate text-body">{v}</dd>
              </div>
            ))}
          </dl>
        )}
        {p && !self && (
          <div className="flex flex-col gap-1">
            <span className="text-[12px] text-muted-foreground">共同的群{common.length ? ` · ${common.length}` : ''}</span>
            {common.length ? (
              <div className="flex flex-wrap gap-1">
                {common.map((g) => (
                  <a
                    key={g.id}
                    href={`#/g/${g.id}`}
                    onClick={onDone}
                    className="max-w-full truncate rounded-chip bg-accent px-1.5 py-0.5 text-[12px] text-body transition-colors hover:text-foreground"
                  >
                    {g.name || '未命名群'}
                  </a>
                ))}
              </div>
            ) : (
              <span className="text-[12.5px] text-muted-foreground">没有</span>
            )}
          </div>
        )}
      </div>

      {!self && (
        <div className="flex gap-2 border-t border-border p-3">
          <Button size="sm" className="flex-1" disabled={busy || !!p?.disabled} onClick={dm}>
            <MessageCircle />
            {p?.disabled ? '已停用' : '私聊'}
          </Button>
          {mine && (
            <Button size="sm" variant="outline" className="flex-1" onClick={manage}>
              <Settings2 />
              管理
            </Button>
          )}
        </div>
      )}
    </div>
  )
}
