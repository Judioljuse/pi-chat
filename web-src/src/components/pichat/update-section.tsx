import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { ArrowUpCircle, ExternalLink, RefreshCw, Undo2 } from 'lucide-react'
import { Alert, AlertContent, AlertDescription, AlertIcon } from '@/components/motion/alert'
import { Section } from '@/components/pichat/page'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { api, errorText } from '@/lib/api'
import { fmtDateTime } from '@/lib/format'
import { MarkdownText } from '@/lib/markdown'
import { setState } from '@/lib/store'
import type { UpdateStatus } from '@/lib/types'
import { cn } from '@/lib/utils'

const PHASE: Record<string, string> = {
  downloading: '正在下载…',
  verifying: '正在校验…',
  restarting: '正在重启…',
}

const busyPhase = (p?: string) => p === 'downloading' || p === 'verifying' || p === 'restarting'

const mb = (n: number) => (n / (1 << 20)).toFixed(1) + ' MB'

type Pending = { kind: 'install'; tag: string } | { kind: 'rollback' } | { kind: 'clear' }

/** Admin page section: versions, the latest GitHub release, one-click update and rollback. */
export function UpdateSection() {
  const [st, setSt] = useState<UpdateStatus | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const [checking, setChecking] = useState(false)
  const [pending, setPending] = useState<Pending | null>(null)
  const [pw, setPw] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [dialogErr, setDialogErr] = useState('')

  const apply = useCallback((s: UpdateStatus) => {
    setSt(s)
    setErr(null)
    setState({ updateAvailable: s.available })
    if (s.state.phase === 'restarting') setState({ restarting: true })
  }, [])

  const load = useCallback(
    async (refresh = false) => {
      try {
        apply(await api<UpdateStatus>('GET', '/admin/update' + (refresh ? '?refresh=1' : '')))
      } catch (e) {
        setErr(e)
      }
    },
    [apply],
  )

  useEffect(() => {
    load()
  }, [load])

  // Poll progress every second while an install runs.
  const phase = st?.state.phase
  useEffect(() => {
    if (!busyPhase(phase)) return
    const t = setInterval(() => {
      api<UpdateStatus>('GET', '/admin/update').then(apply, () => {
        /* server restarting */
      })
    }, 1000)
    return () => clearInterval(t)
  }, [phase, apply])

  const check = async () => {
    setChecking(true)
    await load(true)
    setChecking(false)
  }

  const open = (p: Pending) => {
    setPw('')
    setDialogErr('')
    setPending(p)
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!pending || !pw) return
    setSubmitting(true)
    setDialogErr('')
    try {
      if (pending.kind === 'install') {
        apply(await api<UpdateStatus>('POST', '/admin/update', { tag: pending.tag, password: pw }))
      } else if (pending.kind === 'clear') {
        await api('POST', '/admin/update/rollback', { password: pw })
        await load()
      } else {
        await api('POST', '/admin/update/rollback', { password: pw })
        setState({ restarting: true })
      }
      setPending(null)
    } catch (e) {
      setDialogErr(errorText(e))
    } finally {
      setSubmitting(false)
    }
  }

  if (err && !st)
    return (
      <Section title="版本与更新">
        <p className="text-[13px] text-destructive">{errorText(err)}</p>
      </Section>
    )
  if (!st)
    return (
      <Section title="版本与更新">
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-5 w-64" />
      </Section>
    )

  const latest = st.latest
  const busy = busyPhase(phase)
  const kv = (label: string, value: React.ReactNode) => (
    <div className="flex min-w-0 items-center gap-3 py-1.5 text-[13px]">
      <span className="w-20 shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 flex-1 truncate text-body">{value}</span>
    </div>
  )

  return (
    <Section
      title="版本与更新"
      description={
        st.enabled
          ? `从 GitHub 上 ${st.repo} 的发布页检查新版本，每 12 小时自动检查一次。`
          : `在线更新已关闭：${st.disabledReason || '未知原因'}`
      }
    >
      {st.installed?.failed && (
        <Alert variant="warning" className="p-3">
          <AlertIcon />
          <AlertContent>
            <AlertDescription className="text-[12.5px]">
              新版本 {st.installed.version} 连续 3 次启动失败，已改用内置版本 {st.builtin}。查看服务器日志后可以重新更新。
            </AlertDescription>
            {st.running === 'builtin' && (
              <div>
                <Button variant="outline" size="sm" className="mt-2" onClick={() => open({ kind: 'clear' })} disabled={busy}>
                  清除这个版本
                </Button>
              </div>
            )}
          </AlertContent>
        </Alert>
      )}

      <div className="flex flex-col divide-y divide-border rounded-control border border-border px-3">
        {kv('当前版本', <span className="font-mono text-[12.5px]">v{st.current}</span>)}
        {kv(
          '运行的程序',
          st.running === 'installed' ? (
            <>
              在线更新安装的版本 <span className="text-muted-foreground">（内置版本 v{st.builtin}）</span>
            </>
          ) : (
            '内置版本'
          ),
        )}
        {kv('平台', <span className="font-mono text-[12.5px]">{st.platform}</span>)}
        {st.checkedAt ? kv('上次检查', fmtDateTime(st.checkedAt)) : null}
      </div>

      {st.enabled && latest && (
        <div className="flex flex-col gap-2 rounded-control border border-border p-3">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[13.5px] font-medium text-foreground">最新版本 {latest.tag}</span>
            {latest.publishedAt && (
              <span className="text-[12px] text-muted-foreground tabular-nums">{fmtDateTime(Date.parse(latest.publishedAt))}</span>
            )}
            {st.available ? (
              <span className="rounded-chip bg-brand-light px-1.5 text-[11.5px] leading-5 text-brand">可以更新</span>
            ) : (
              <span className="rounded-chip bg-tag px-1.5 text-[11.5px] leading-5 text-muted-foreground">已是最新</span>
            )}
            {latest.htmlUrl && (
              <a
                href={latest.htmlUrl}
                target="_blank"
                rel="noreferrer noopener"
                className="ml-auto inline-flex items-center gap-1 text-[12.5px] text-brand hover:underline"
              >
                发布页
                <ExternalLink className="size-3" />
              </a>
            )}
          </div>
          {latest.notes && (
            <div className="max-h-64 overflow-y-auto">
              <MarkdownText text={latest.notes} className="text-[13px]" />
            </div>
          )}
        </div>
      )}

      {st.error && <p className="text-[12.5px] text-destructive">{st.error}</p>}

      {busy && (
        <div className="flex flex-col gap-1.5" aria-live="polite">
          <div className="flex items-center justify-between text-[12.5px]">
            <span className="text-body">{PHASE[phase!]}</span>
            {st.state.total > 0 && (
              <span className="font-mono text-muted-foreground tabular-nums">
                {mb(st.state.bytes)} / {mb(st.state.total)}
              </span>
            )}
          </div>
          <div className="h-1.5 overflow-hidden rounded-full bg-accent">
            <div
              className={cn('h-full rounded-full bg-brand transition-[width] duration-300', !st.state.total && 'w-1/3 animate-pulse')}
              style={st.state.total ? { width: `${Math.min(100, (st.state.bytes / st.state.total) * 100)}%` } : undefined}
            />
          </div>
        </div>
      )}
      {phase === 'error' && st.state.error && <p className="text-[12.5px] text-destructive">更新失败：{st.state.error}</p>}

      <div className="flex flex-wrap gap-2">
        {st.enabled && (
          <Button variant="outline" size="sm" onClick={check} disabled={checking || busy}>
            <RefreshCw className={cn(checking && 'animate-spin')} />
            检查更新
          </Button>
        )}
        {st.enabled && st.available && latest && (
          <Button size="sm" onClick={() => open({ kind: 'install', tag: latest.tag })} disabled={busy}>
            <ArrowUpCircle />
            更新到 {latest.tag}
          </Button>
        )}
        {st.running === 'installed' && (
          <Button variant="outline" size="sm" onClick={() => open({ kind: 'rollback' })} disabled={busy}>
            <Undo2 />
            恢复内置版本
          </Button>
        )}
      </div>

      <p className="text-[12px] text-muted-foreground">pichat v{st.current}</p>

      <AlertDialog open={!!pending} onOpenChange={(o) => !o && !submitting && setPending(null)}>
        <AlertDialogContent>
          <form onSubmit={submit} className="contents">
            <AlertDialogHeader>
              <AlertDialogTitle>
                {pending?.kind === 'install'
                  ? `更新到 ${pending.tag}？`
                  : pending?.kind === 'clear'
                    ? `清除启动失败的 v${st.installed?.version}？`
                    : `恢复内置版本 v${st.builtin}？`}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {pending?.kind === 'install'
                  ? `服务器会下载并运行 GitHub 上 ${st.repo} 发布的程序，期间网页会断开几秒。`
                  : pending?.kind === 'clear'
                    ? '服务器会删除这个版本的程序和启动记录，不会重启。'
                    : '服务器会删除在线更新安装的版本记录并重启到内置版本，期间网页会断开几秒。'}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="update-pw" className="text-[13px] font-medium text-body">
                输入你的登录密码确认
              </Label>
              <Input
                id="update-pw"
                type="password"
                autoComplete="current-password"
                autoFocus
                value={pw}
                onChange={(e) => setPw(e.target.value)}
              />
              {dialogErr && <p className="text-[12.5px] text-destructive">{dialogErr}</p>}
            </div>
            <AlertDialogFooter>
              <AlertDialogCancel type="button" disabled={submitting}>
                取消
              </AlertDialogCancel>
              <Button type="submit" disabled={submitting || !pw}>
                {pending?.kind === 'install' ? '开始更新' : pending?.kind === 'clear' ? '清除' : '恢复'}
              </Button>
            </AlertDialogFooter>
          </form>
        </AlertDialogContent>
      </AlertDialog>
    </Section>
  )
}
