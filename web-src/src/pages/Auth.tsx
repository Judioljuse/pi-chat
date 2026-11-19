import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Field } from '@/components/pichat/page'
import { api, ApiError } from '@/lib/api'
import { navigate, rememberAfterLogin } from '@/lib/router'
import { fail, notify, useStore } from '@/lib/store'
import type { Invite, Me } from '@/lib/types'

export const emitLogin = (user: Me) => window.dispatchEvent(new CustomEvent('pichat:login', { detail: user }))

function AuthCard({ title, subtitle, children }: { title: ReactNode; subtitle?: ReactNode; children: ReactNode }) {
  return (
    <div className="grid min-h-dvh place-items-center bg-background px-4 py-10">
      <div className="w-full max-w-[380px] animate-panel-in">
        <div className="mb-6 flex items-center justify-center gap-2">
          <span className="grid size-7 place-items-center rounded-[8px] bg-foreground font-serif text-[16px] text-background italic">π</span>
          <span className="font-serif text-[20px] tracking-tight text-foreground">pichat</span>
        </div>
        <div className="rounded-card border border-border bg-card p-6">
          <h1 className="font-serif text-[22px] leading-tight text-foreground">{title}</h1>
          {subtitle && <p className="mt-1.5 text-[13px] text-muted-foreground">{subtitle}</p>}
          <div className="mt-5">{children}</div>
        </div>
      </div>
    </div>
  )
}

export function Login() {
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const { user } = await api<{ user: Me }>('POST', '/auth/login', { name, password })
      emitLogin(user)
    } catch (err) {
      fail(err instanceof ApiError && err.status === 401 ? new Error('用户名或密码错误') : err)
    } finally {
      setBusy(false)
    }
  }
  return (
    <AuthCard title="登录" subtitle="人和 agent 的群聊">
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="用户名" htmlFor="login-name">
          <Input id="login-name" autoComplete="username" required value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="密码" htmlFor="login-password">
          <Input
            id="login-password"
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>
        <Button type="submit" size="lg" disabled={busy} className="mt-1 w-full">
          登录
        </Button>
        <p className="text-center text-[12.5px] text-muted-foreground">没有账号？请向管理员要邀请链接。</p>
      </form>
    </AuthCard>
  )
}

type InviteState = { loading: true } | { loading: false; invite?: Invite; error?: unknown }

export function Redeem({ code }: { code: string }) {
  const me = useStore((s) => s.me)
  const [st, setSt] = useState<InviteState>({ loading: true })
  const [f, setF] = useState({ name: '', displayName: '', password: '', password2: '' })
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    setSt({ loading: true })
    api<Invite>('GET', `/auth/invite/${encodeURIComponent(code)}`).then(
      (invite) => setSt({ loading: false, invite }),
      (error) => setSt({ loading: false, error }),
    )
  }, [code])

  const redeem = async (body: Record<string, string>) => {
    setBusy(true)
    try {
      const r = await api<{ user: Me; groupId?: string }>('POST', '/auth/redeem', { code, ...body })
      emitLogin(r.user)
      if (r.groupId) setTimeout(() => navigate(`#/g/${r.groupId}`), 50)
      else navigate('#/')
    } catch (err) {
      fail(err instanceof ApiError && err.code === 'conflict' ? new Error('用户名已被占用') : err)
    } finally {
      setBusy(false)
    }
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!/^[a-z0-9_-]{3,32}$/.test(f.name)) return notify('用户名只能用小写字母、数字、_ 和 -，3 到 32 位')
    if (f.password.length < 8) return notify('密码至少 8 位')
    if (f.password !== f.password2) return notify('两次密码不一致')
    redeem({ name: f.name, displayName: f.displayName.trim() || f.name, password: f.password })
  }

  const goLogin = () => {
    rememberAfterLogin()
    navigate('#/login')
  }

  if (st.loading)
    return (
      <AuthCard title={<Skeleton className="h-6 w-40" />}>
        <div className="flex flex-col gap-3">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
        </div>
      </AuthCard>
    )
  if (!st.invite)
    return (
      <AuthCard title="链接无效" subtitle="邀请链接不存在、已过期或已用完。">
        <Button variant="outline" className="w-full" onClick={() => navigate('#/')}>
          返回
        </Button>
      </AuthCard>
    )

  const inv = st.invite
  const title = inv.kind === 'group' ? `加入群「${inv.group?.name || ''}」` : inv.kind === 'admin' ? '创建管理员账号' : '注册 pichat'
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setF({ ...f, [k]: k === 'name' ? e.target.value.trim().toLowerCase() : e.target.value })

  return (
    <AuthCard title={title} subtitle={inv.group?.topic || undefined}>
      {inv.kind === 'group' && me ? (
        <div className="flex flex-col gap-4">
          <p className="text-[13.5px] text-body">
            你当前登录为 <b className="font-medium text-foreground">{me.displayName}</b>。
          </p>
          <Button size="lg" className="w-full" disabled={busy} onClick={() => redeem({})}>
            加入群
          </Button>
        </div>
      ) : (
        <form onSubmit={submit} className="flex flex-col gap-4">
          <Field label="用户名" hint="小写字母、数字、_ -，3-32 位，用于登录" htmlFor="r-name">
            <Input id="r-name" autoComplete="username" required value={f.name} onChange={set('name')} />
          </Field>
          <Field label="显示名" htmlFor="r-dn">
            <Input id="r-dn" value={f.displayName} placeholder={f.name} onChange={set('displayName')} />
          </Field>
          <Field label="密码" hint="至少 8 位" htmlFor="r-pw">
            <Input id="r-pw" type="password" autoComplete="new-password" required value={f.password} onChange={set('password')} />
          </Field>
          <Field label="确认密码" htmlFor="r-pw2">
            <Input id="r-pw2" type="password" autoComplete="new-password" required value={f.password2} onChange={set('password2')} />
          </Field>
          <Button type="submit" size="lg" className="mt-1 w-full" disabled={busy}>
            {inv.kind === 'group' ? '注册并加入' : '注册'}
          </Button>
          {inv.kind === 'group' && (
            <p className="text-center text-[12.5px] text-muted-foreground">
              已有账号？
              <button type="button" className="font-medium text-brand underline-offset-4 hover:underline" onClick={goLogin}>
                登录后加入
              </button>
            </p>
          )}
        </form>
      )}
    </AuthCard>
  )
}
