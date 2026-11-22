import { useEffect, useState, type FormEvent } from 'react'
import { LogOut } from 'lucide-react'
import { Field, Page, Section } from '@/components/pichat/page'
import { PrincipalAvatar } from '@/components/pichat/avatar'
import { RoleBadge } from '@/components/pichat/role-badge'
import { logout } from '@/components/pichat/sidebar'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { disableDesktopNotify, enableDesktopNotify, useNotifyStatus } from '@/lib/desktop-notify'
import { api } from '@/lib/api'
import { fmtTime } from '@/lib/format'
import { disablePush, enablePush, usePushState } from '@/lib/push'
import { fail, notify, setState, useStore } from '@/lib/store'
import type { Me } from '@/lib/types'

export function Account() {
  const me = useStore((s) => s.me)
  const serverVersion = useStore((s) => s.serverVersion)
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

      <NotifySection />
      <PushSection />

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
      {serverVersion && <p className="text-center text-[12px] text-muted-foreground">pichat v{serverVersion}</p>}
    </Page>
  )
}

function NotifySection() {
  const st = useNotifyStatus()
  const hint = !st.supported
    ? '这个浏览器不支持桌面通知。'
    : !st.secure
      ? '桌面通知需要 HTTPS 或 localhost 访问（例如 tailscale serve 或 ssh -L）'
      : st.permission === 'denied'
        ? '浏览器已拒绝通知。请在地址栏左侧的网站设置里允许通知，再回来打开。'
        : '有人 @ 你、回复你或私聊你，而你没在看那个群时，弹出桌面通知。点通知会跳到那条消息。'
  return (
    <Section title="桌面通知" description={hint}>
      <label className="flex items-center justify-between gap-3 text-[13px] text-body">
        开启桌面通知
        <Switch
          checked={st.enabled}
          disabled={!st.supported || !st.secure || st.permission === 'denied'}
          onCheckedChange={(on) => (on ? enableDesktopNotify() : disableDesktopNotify())}
          aria-label="开启桌面通知"
        />
      </label>
    </Section>
  )
}

function PushSection() {
  const [st, refresh] = usePushState()
  const [busy, setBusy] = useState(false)
  const [devices, setDevices] = useState<{ id: string; label: string; lastOkAt: number | null; lastError: string }[]>([])
  const loadDevices = () =>
    api<{ devices: typeof devices }>('GET', '/push/subscriptions').then((r) => setDevices(r.devices ?? []), () => {})
  useEffect(() => {
    loadDevices()
  }, [st])
  const run = async (f: () => Promise<unknown>, ok?: string) => {
    setBusy(true)
    try {
      await f()
      if (ok) notify(ok)
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
      refresh()
      loadDevices()
    }
  }
  const hint =
    st === 'need-install'
      ? 'iPhone / iPad 上要先把 pichat 添加到主屏幕：在 Safari 里点「分享」→「添加到主屏幕」，然后从主屏幕图标打开 pichat，再回到这里开启。需要 iOS 16.4 以上。'
      : st === 'unsupported'
        ? '这个浏览器不支持推送，或者不是通过 HTTPS 打开的。'
        : st === 'denied'
          ? '系统拒绝了 pichat 的通知。请在系统设置（iPhone：设置 → 通知 → pichat）或浏览器的网站设置里允许通知。'
          : '有人 @ 你、回复你、私聊你，或有 agent 请你决定，而你 20 秒内没看时，推送到这台设备，锁屏也能收到。每台手机或电脑要分别开启。'
  return (
    <Section title="手机推送" description={hint}>
      <div className="flex flex-col gap-3">
        <label className="flex items-center justify-between gap-3 text-[13px] text-body">
          这台设备接收推送
          <Switch
            checked={st === 'on'}
            disabled={busy || st === null || st === 'unsupported' || st === 'need-install' || st === 'denied'}
            onCheckedChange={(on) => run(on ? enablePush : disablePush, on ? '已开启推送' : '已关闭推送')}
            aria-label="这台设备接收推送"
          />
        </label>
        {devices.length > 0 && (
          <div className="flex flex-col gap-1.5 text-[12.5px]">
            <div className="text-muted-foreground">已开启推送的设备（{devices.length}）</div>
            {devices.map((d) => (
              <div key={d.id} className="flex items-center gap-2">
                <span className="text-body">{d.label}</span>
                <span className="text-muted-foreground">{d.lastError ? `上次失败：${d.lastError}` : d.lastOkAt ? `上次送达 ${fmtTime(d.lastOkAt)}` : '还没有推送过'}</span>
                <Button
                  size="sm"
                  variant="ghost"
                  className="ml-auto h-7"
                  onClick={() => run(() => api('DELETE', '/push/subscriptions', { id: d.id }), '已移除这台设备')}
                >
                  移除
                </Button>
              </div>
            ))}
            <div>
              <Button size="sm" variant="outline" disabled={busy} onClick={() => run(() => api('POST', '/push/test'), '已发送测试推送')}>
                发一条测试推送
              </Button>
            </div>
          </div>
        )}
      </div>
    </Section>
  )
}
