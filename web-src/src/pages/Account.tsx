import { useState, type FormEvent } from 'react'
import { LogOut } from 'lucide-react'
import { Field, Page, Section } from '@/components/pichat/page'
import { PrincipalAvatar } from '@/components/pichat/avatar'
import { RoleBadge } from '@/components/pichat/role-badge'
import { logout } from '@/components/pichat/sidebar'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'
import { fail, notify, setState, useStore } from '@/lib/store'
import type { Me } from '@/lib/types'

export function Account() {
  const me = useStore((s) => s.me)
  const [dn, setDn] = useState(me?.displayName ?? '')
  const [pw, setPw] = useState({ cur: '', a: '', b: '' })
  const [busy, setBusy] = useState(false)
  if (!me) return null

  const save = async (body: Record<string, string>) => {
    setBusy(true)
    try {
      const r = await api<{ me?: Me }>('PATCH', '/me', body)
      setState((s) => ({ me: r.me ?? (s.me && body.displayName ? { ...s.me, displayName: body.displayName } : s.me) }))
      notify('已保存')
      return true
    } catch (e) {
      fail(e)
      return false
    } finally {
      setBusy(false)
    }
  }

  const saveName = (e: FormEvent) => {
    e.preventDefault()
    if (dn.trim() && dn.trim() !== me.displayName) save({ displayName: dn.trim() })
  }

  const savePassword = async (e: FormEvent) => {
    e.preventDefault()
    if (!pw.cur) return notify('请输入当前密码')
    if (pw.a.length < 8) return notify('密码至少 8 位')
    if (pw.a !== pw.b) return notify('两次密码不一致')
    if (await save({ password: pw.a, currentPassword: pw.cur })) setPw({ cur: '', a: '', b: '' })
  }

  return (
    <Page title="我的账号" back="#/">
      <Section>
        <div className="flex items-center gap-3">
          <PrincipalAvatar id={me.id} name={me.displayName} kind="human" size="lg" />
          <div className="min-w-0">
            <div className="truncate text-[15px] font-medium text-foreground">{me.displayName}</div>
            <div className="text-[12.5px] text-muted-foreground">
              登录名 <span className="font-mono">{me.name}</span>
              {me.isAdmin && <RoleBadge role="admin" className="ml-1.5 align-middle" />}
            </div>
          </div>
        </div>
      </Section>

      <Section title="显示名" description="群里的人看到的名字。">
        <form onSubmit={saveName} className="flex gap-2">
          <Input value={dn} onChange={(e) => setDn(e.target.value)} maxLength={64} aria-label="显示名" />
          <Button type="submit" disabled={busy || !dn.trim() || dn.trim() === me.displayName}>
            保存
          </Button>
        </form>
      </Section>

      <Section title="修改密码" description="修改后，你在其他设备上的登录会全部失效。">
        <form onSubmit={savePassword} className="flex flex-col gap-3">
          <Field label="当前密码" htmlFor="pw-cur">
            <Input
              id="pw-cur"
              type="password"
              autoComplete="current-password"
              value={pw.cur}
              onChange={(e) => setPw({ ...pw, cur: e.target.value })}
            />
          </Field>
          <Field label="新密码" htmlFor="pw-a" hint="至少 8 位">
            <Input id="pw-a" type="password" autoComplete="new-password" value={pw.a} onChange={(e) => setPw({ ...pw, a: e.target.value })} />
          </Field>
          <Field label="确认新密码" htmlFor="pw-b">
            <Input id="pw-b" type="password" autoComplete="new-password" value={pw.b} onChange={(e) => setPw({ ...pw, b: e.target.value })} />
          </Field>
          <div>
            <Button type="submit" disabled={busy || !pw.cur || !pw.a || !pw.b}>
              修改密码
            </Button>
          </div>
        </form>
      </Section>

      <div>
        <Button variant="destructive" onClick={logout}>
          <LogOut />
          退出登录
        </Button>
      </div>
    </Page>
  )
}
