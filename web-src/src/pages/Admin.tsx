import { useCallback, useEffect, useState } from 'react'
import { Link, RefreshCw } from 'lucide-react'
import { CopyField } from '@/components/pichat/copy-field'
import { PrincipalAvatar } from '@/components/pichat/avatar'
import { RoleBadge } from '@/components/pichat/role-badge'
import { Empty, Page, Section } from '@/components/pichat/page'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { api, errorText } from '@/lib/api'
import { fmtDateTime } from '@/lib/format'
import { fail, useStore } from '@/lib/store'
import type { AdminUser } from '@/lib/types'
import { cn } from '@/lib/utils'

type Users = { loading: boolean; users?: AdminUser[]; error?: unknown }

export function Admin() {
  const isAdmin = useStore((s) => s.me?.isAdmin)
  const [invite, setInvite] = useState<{ url: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const [st, setSt] = useState<Users>({ loading: true })

  const reload = useCallback(() => {
    setSt((s) => ({ ...s, loading: true }))
    api<{ users: AdminUser[] }>('GET', '/admin/users').then(
      (r) => setSt({ loading: false, users: r.users ?? [] }),
      (error) => setSt({ loading: false, error }),
    )
  }, [])

  useEffect(() => {
    if (isAdmin) reload()
  }, [isAdmin, reload])

  if (!isAdmin)
    return (
      <Page title="管理" back="#/">
        <Empty>只有管理员可以访问</Empty>
      </Page>
    )

  const gen = async () => {
    setBusy(true)
    try {
      setInvite(await api<{ url: string }>('POST', '/admin/invites', {}))
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Page
      title="管理"
      back="#/"
      actions={
        <Button variant="ghost" size="sm" onClick={reload} disabled={st.loading}>
          <RefreshCw className={cn(st.loading && 'animate-spin')} />
          刷新
        </Button>
      }
    >
      <Section title="用户邀请链接" description="24 小时内有效，只能注册 1 个账号。">
        {invite && <CopyField value={invite.url} />}
        <div>
          <Button onClick={gen} disabled={busy}>
            <Link />
            {invite ? '再生成一个' : '生成邀请链接'}
          </Button>
        </div>
      </Section>

      <Section className="p-0">
        <div className="flex items-center px-4 pt-4 pb-2">
          <h2 className="text-[14px] font-medium text-foreground">
            用户{st.users && <span className="ml-1.5 font-normal text-muted-foreground tabular-nums">{st.users.length}</span>}
          </h2>
        </div>
        {st.error ? (
          <p className="px-4 pb-4 text-[13px] text-destructive">{errorText(st.error)}</p>
        ) : !st.users ? (
          <div className="flex flex-col gap-2 px-4 pb-4">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        ) : (
          <Table className="text-[13px]">
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="pl-4 text-[12px] font-normal text-muted-foreground">用户</TableHead>
                <TableHead className="text-[12px] font-normal text-muted-foreground">登录名</TableHead>
                <TableHead className="text-[12px] font-normal text-muted-foreground">角色</TableHead>
                <TableHead className="pr-4 text-right text-[12px] font-normal text-muted-foreground">注册时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {st.users.map((u) => (
                <TableRow key={u.id}>
                  <TableCell className="pl-4">
                    <span className="flex items-center gap-2">
                      <PrincipalAvatar id={u.id} name={u.displayName} kind="human" size="sm" />
                      <span className="font-medium text-foreground">{u.displayName}</span>
                    </span>
                  </TableCell>
                  <TableCell className="font-mono text-[12.5px] text-body">{u.name}</TableCell>
                  <TableCell>
                    <RoleBadge role={u.isAdmin ? 'admin' : 'user'} />
                  </TableCell>
                  <TableCell className="pr-4 text-right text-muted-foreground tabular-nums">{fmtDateTime(u.createdAt)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Section>
    </Page>
  )
}
