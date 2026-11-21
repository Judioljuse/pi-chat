import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { ChevronRight, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useConfirm } from '@/components/pichat/confirm'
import { Field, Section } from '@/components/pichat/page'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { api, errorText } from '@/lib/api'
import { fmtDateTime } from '@/lib/format'
import { fail, notify } from '@/lib/store'
import { cn } from '@/lib/utils'

/** GET /api/v1/admin/tools. Secrets never come back; keyHint is the last 4 characters. */
export interface AdminTools {
  builtin: { name: string; enabled: boolean; description?: string }[]
  search: { provider: string; keySet: boolean; keyHint?: string; url?: string; source: 'db' | 'env' | 'default' }
  servers: McpServer[]
}

export interface McpServer {
  id: string
  name: string
  url: string
  headerName: string
  keySet: boolean
  keyHint?: string
  enabled: boolean
  tools: { name: string; description?: string }[]
  error?: string
  updatedAt?: number
}

/** Runtime part of GET /api/v1 (`tools`). */
type ToolsInfo = {
  mcp: string
  limits?: { searchesPerAgentPerHour?: number; fetchesPerAgentPerHour?: number }
  searchError?: string
}

const PROVIDERS: { value: string; label: string; key: boolean; url: boolean }[] = [
  { value: 'duckduckgo', label: 'DuckDuckGo（免 key）', key: false, url: false },
  { value: 'brave', label: 'Brave Search', key: true, url: false },
  { value: 'tavily', label: 'Tavily', key: true, url: false },
  { value: 'bocha', label: '博查', key: true, url: false },
  { value: 'searxng', label: 'SearXNG（自建）', key: false, url: true },
]

const BUILTIN_DESC: Record<string, string> = {
  view_attachment: '查看群里的附件（图片、文本、PDF 等）',
  web_fetch: '读取网页正文',
  web_search: '用下面配置的搜索服务搜索网页',
  search_messages: '搜索群聊的历史消息（只能搜 agent 所在的群）',
}

const SOURCE: Record<AdminTools['search']['source'], string> = { db: '网页设置', env: '环境变量', default: '默认' }

const NAME_RE = /^(?!.*__)(?!.*_$)[a-z0-9_-]{1,32}$/

const hint = (keySet: boolean, keyHint?: string) => (keySet ? `已设置 ${keyHint ? `…${keyHint.replace(/^…/, '')}` : ''}`.trim() : '')

/** Admin page section: built-in tool switches, web search provider and external MCP servers proxied by pichat. */
export function ToolsSection() {
  const [data, setData] = useState<AdminTools | null>(null)
  const [info, setInfo] = useState<ToolsInfo | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const { ask, dialog } = useConfirm()
  const [editing, setEditing] = useState<McpServer | 'new' | null>(null)

  const load = useCallback(async () => {
    try {
      const [d, root] = await Promise.all([
        api<AdminTools>('GET', '/admin/tools'),
        api<{ tools?: ToolsInfo }>('GET', '').catch(() => ({ tools: undefined })),
      ])
      setData({ builtin: d.builtin ?? [], search: d.search, servers: d.servers ?? [] })
      setInfo(root.tools ?? null)
      setErr(null)
    } catch (e) {
      setErr(e)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const toggleBuiltin = async (name: string, enabled: boolean) => {
    setData((d) => d && { ...d, builtin: d.builtin.map((b) => (b.name === name ? { ...b, enabled } : b)) })
    try {
      await api('PUT', '/admin/tools/builtin', { name, enabled })
      notify(enabled ? `已启用 ${name}` : `已停用 ${name}`)
    } catch (e) {
      fail(e)
    }
    load()
  }

  const patchServer = async (s: McpServer, body: Record<string, unknown>, done?: string) => {
    try {
      await api('PATCH', `/admin/tools/servers/${encodeURIComponent(s.id)}`, body)
      if (done) notify(done)
    } catch (e) {
      fail(e)
    }
    await load()
  }

  const refresh = async (s: McpServer) => {
    try {
      await api('POST', `/admin/tools/servers/${encodeURIComponent(s.id)}/refresh`, {})
      notify(`已刷新 ${s.name} 的工具列表`)
    } catch (e) {
      fail(e)
    }
    await load()
  }

  const remove = async (s: McpServer) => {
    const ok = await ask({
      title: `删除 MCP 服务器 ${s.name}？`,
      description: '删除后 agent 不能再用它的工具，保存的密钥也会一起删除。',
      confirmText: '删除',
      destructive: true,
    })
    if (!ok) return
    try {
      await api('DELETE', `/admin/tools/servers/${encodeURIComponent(s.id)}`)
      notify(`已删除 ${s.name}`)
    } catch (e) {
      fail(e)
    }
    await load()
  }

  return (
    <Section
      title="MCP 工具"
      description="agent 通过服务器的 MCP 使用这些工具，改动立即生效。外部 MCP 服务器的工具可能有副作用，只开放给 project/full 档和长期 agent token，restricted 档只能用内置的只读工具。"
    >
      {err ? (
        <p className="text-[13px] text-destructive">{errorText(err)}</p>
      ) : !data ? (
        <div className="flex flex-col gap-2">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-8 w-full" />
          ))}
        </div>
      ) : (
        <>
          <Group title="内置工具">
            <div className="flex flex-col divide-y divide-border rounded-control border border-border">
              {data.builtin.map((b) => (
                <label key={b.name} className="flex min-h-11 cursor-pointer items-center gap-3 px-3 py-2">
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="font-mono text-[12.5px] text-foreground">{b.name}</span>
                    <span className="truncate text-[12px] text-muted-foreground">{BUILTIN_DESC[b.name] || b.description}</span>
                  </span>
                  <Switch checked={b.enabled} onCheckedChange={(on) => toggleBuiltin(b.name, on)} aria-label={`启用 ${b.name}`} />
                </label>
              ))}
            </div>
            {info && (
              <p className="text-[12px] text-muted-foreground">
                MCP 地址 <span className="font-mono">{info.mcp}</span>
                {info.limits?.searchesPerAgentPerHour != null && (
                  <>
                    {' '}
                    · 每个 agent 每小时搜索 <span className="font-mono tabular-nums">{info.limits.searchesPerAgentPerHour}</span> 次、读网页{' '}
                    <span className="font-mono tabular-nums">{info.limits.fetchesPerAgentPerHour}</span> 次
                  </>
                )}
              </p>
            )}
          </Group>

          <SearchSettings search={data.search} error={info?.searchError} onSaved={load} />

          <Group
            title="外部 MCP 服务器"
            action={
              <Button size="sm" variant="outline" onClick={() => setEditing('new')}>
                <Plus />
                添加 MCP 服务器
              </Button>
            }
          >
            {!data.servers.length ? (
              <p className="rounded-control border border-dashed border-border px-3 py-4 text-center text-[13px] text-muted-foreground">
                还没有外部 MCP 服务器。添加后，它的工具以「服务器名__工具名」出现在 pichat 的 MCP 里。
              </p>
            ) : (
              <div className="flex flex-col divide-y divide-border rounded-control border border-border">
                {data.servers.map((s) => (
                  <ServerRow
                    key={s.id}
                    s={s}
                    onToggle={(on) => patchServer(s, { enabled: on }, on ? `已启用 ${s.name}` : `已停用 ${s.name}`)}
                    onRefresh={() => refresh(s)}
                    onEdit={() => setEditing(s)}
                    onDelete={() => remove(s)}
                  />
                ))}
              </div>
            )}
          </Group>
        </>
      )}
      <ServerDialog
        server={editing}
        onClose={() => setEditing(null)}
        onSaved={async () => {
          setEditing(null)
          await load()
        }}
      />
      {dialog}
    </Section>
  )
}

function Group({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-2 pt-1">
      <div className="flex min-h-8 items-center gap-2">
        <h3 className="flex-1 text-[13px] font-medium text-body">{title}</h3>
        {action}
      </div>
      {children}
    </div>
  )
}

function SearchSettings({ search, error, onSaved }: { search: AdminTools['search']; error?: string; onSaved: () => Promise<void> | void }) {
  const [provider, setProvider] = useState(search.provider || 'duckduckgo')
  const [key, setKey] = useState('')
  const [clearKey, setClearKey] = useState(false)
  const [url, setUrl] = useState(search.url || '')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    setProvider(search.provider || 'duckduckgo')
    setUrl(search.url || '')
    setKey('')
    setClearKey(false)
  }, [search.provider, search.url, search.keySet, search.keyHint])

  const p = PROVIDERS.find((x) => x.value === provider)
  // The server keeps the saved key only while the provider stays the same.
  const sameProvider = provider === (search.provider || 'duckduckgo')
  const keySet = sameProvider && search.keySet
  const dirty = provider !== (search.provider || 'duckduckgo') || !!key || clearKey || (p?.url && url !== (search.url || ''))

  const save = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const body: Record<string, string> = { provider }
      if (key) body.key = key
      else if (clearKey && keySet) body.key = ''
      if (p?.url) body.url = url.trim()
      await api('PUT', '/admin/tools/search', body)
      // The reset effect only fires when the saved fields change; a new key with the same last 4 chars must still clear.
      setKey('')
      setClearKey(false)
      notify('搜索设置已保存')
      await onSaved()
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Group
      title="网页搜索"
      action={
        <span className="rounded-chip bg-tag px-1.5 py-0.5 text-[11.5px] text-brand" title="当前配置的来源：网页设置优先，其次环境变量 PICHAT_SEARCH_*，最后是默认值">
          {SOURCE[search.source] ?? search.source}
        </span>
      }
    >
      <form onSubmit={save} className="flex flex-col gap-3 rounded-control border border-border p-3">
        <Field label="搜索服务">
          <Select value={provider} onValueChange={setProvider}>
            <SelectTrigger className="w-full bg-background sm:w-64" aria-label="搜索服务">
              <SelectValue />
            </SelectTrigger>
            <SelectContent position="popper">
              {PROVIDERS.map((x) => (
                <SelectItem key={x.value} value={x.value}>
                  {x.label}
                </SelectItem>
              ))}
              {!PROVIDERS.some((x) => x.value === provider) && <SelectItem value={provider}>{provider}</SelectItem>}
            </SelectContent>
          </Select>
        </Field>
        {p?.key !== false && (
          <Field
            label="API key"
            hint={
              keySet && !key ? (
                <>
                  留空则保留原来的 key。
                  <button type="button" className="ml-1 text-brand underline-offset-2 hover:underline" onClick={() => setClearKey((c) => !c)}>
                    {clearKey ? '不清除' : '清除 key'}
                  </button>
                </>
              ) : (
                (sameProvider ? '' : '换了搜索服务，需要重新填写 key。') + 'key 加密保存在服务器上，不会再显示出来。'
              )
            }
          >
            <Input
              type="password"
              autoComplete="new-password"
              value={key}
              onChange={(e) => {
                setKey(e.target.value)
                if (e.target.value) setClearKey(false)
              }}
              placeholder={clearKey && keySet ? '保存后清除' : (keySet && hint(search.keySet, search.keyHint)) || '未设置'}
              className={cn('bg-background', clearKey && 'placeholder:text-destructive')}
            />
          </Field>
        )}
        {p?.url && (
          <Field label="SearXNG 地址" hint="需要开启 JSON 输出（settings.yml 的 search.formats 加上 json）。">
            <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://searx.example.com" className="bg-background font-mono text-[12.5px]" />
          </Field>
        )}
        {error && <p className="text-[12.5px] text-destructive">未生效：{error}</p>}
        <div>
          <Button type="submit" size="sm" disabled={busy || !dirty}>
            保存
          </Button>
        </div>
      </form>
    </Group>
  )
}

function ServerRow({
  s,
  onToggle,
  onRefresh,
  onEdit,
  onDelete,
}: {
  s: McpServer
  onToggle: (on: boolean) => void
  onRefresh: () => Promise<void>
  onEdit: () => void
  onDelete: () => void
}) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const tools = s.tools ?? []
  return (
    <div className="flex flex-col gap-1.5 px-3 py-2.5">
      <div className="flex min-w-0 items-center gap-2">
        <div className="flex min-w-0 flex-1 flex-col">
          <span className={cn('font-mono text-[13px] text-foreground', !s.enabled && 'text-muted-foreground')}>{s.name}</span>
          <span className="truncate font-mono text-[12px] text-muted-foreground" title={s.url}>
            {s.url}
          </span>
        </div>
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground"
          aria-label="刷新工具列表"
          title="刷新工具列表"
          disabled={busy}
          onClick={async () => {
            setBusy(true)
            await onRefresh()
            setBusy(false)
          }}
        >
          <RefreshCw className={cn(busy && 'animate-spin')} />
        </Button>
        <Button variant="ghost" size="icon-sm" className="text-muted-foreground" aria-label="编辑" title="编辑" onClick={onEdit}>
          <Pencil />
        </Button>
        <Button variant="ghost" size="icon-sm" className="text-muted-foreground hover:text-destructive" aria-label="删除" title="删除" onClick={onDelete}>
          <Trash2 />
        </Button>
        <Switch checked={s.enabled} onCheckedChange={onToggle} aria-label={`启用 ${s.name}`} className="ml-1" />
      </div>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-muted-foreground">
        <button
          type="button"
          disabled={!tools.length}
          onClick={() => setOpen((o) => !o)}
          aria-expanded={open}
          className="inline-flex items-center gap-0.5 hover:text-foreground disabled:hover:text-muted-foreground"
        >
          {tools.length > 0 && <ChevronRight className={cn('size-3.5 transition-transform', open && 'rotate-90')} />}
          <span className="tabular-nums">{tools.length}</span> 个工具
        </button>
        <span>
          {s.headerName || 'Authorization'}
          {s.keySet ? `：${hint(true, s.keyHint)}` : '：未设置'}
        </span>
        {s.updatedAt ? <span className="tabular-nums">更新于 {fmtDateTime(s.updatedAt)}</span> : null}
      </div>
      {s.error && <p className="text-[12px] break-words text-destructive">上次出错：{s.error}</p>}
      {open && tools.length > 0 && (
        <ul className="flex flex-col gap-1 rounded-control bg-background px-2.5 py-2">
          {tools.map((t) => (
            <li key={t.name} className="flex min-w-0 flex-col text-[12px]">
              <span className="font-mono text-foreground">
                {s.name}__{t.name}
              </span>
              {t.description && <span className="line-clamp-2 text-muted-foreground">{t.description}</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function ServerDialog({ server, onClose, onSaved }: { server: McpServer | 'new' | null; onClose: () => void; onSaved: () => Promise<void> }) {
  const isNew = server === 'new'
  const s = server && server !== 'new' ? server : null
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [headerName, setHeaderName] = useState('Authorization')
  const [headerValue, setHeaderValue] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  useEffect(() => {
    if (!server) return
    setName(s?.name ?? '')
    setUrl(s?.url ?? '')
    setHeaderName(s?.headerName || 'Authorization')
    setHeaderValue('')
    setErr('')
  }, [server, s])

  const nameOk = !isNew || NAME_RE.test(name)
  const urlOk = /^https?:\/\//i.test(url.trim())

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!nameOk || !urlOk) return
    setBusy(true)
    setErr('')
    try {
      if (isNew) {
        const body: Record<string, string> = { name, url: url.trim(), headerName: headerName.trim() || 'Authorization' }
        if (headerValue) body.headerValue = headerValue
        await api('POST', '/admin/tools/servers', body)
        notify(`已添加 ${name}`)
      } else if (s) {
        const body: Record<string, string> = {}
        if (url.trim() !== s.url) body.url = url.trim()
        const hn = headerName.trim() || 'Authorization'
        if (hn !== (s.headerName || 'Authorization')) body.headerName = hn
        if (headerValue) body.headerValue = headerValue
        if (Object.keys(body).length) await api('PATCH', `/admin/tools/servers/${encodeURIComponent(s.id)}`, body)
        notify(`已保存 ${s.name}`)
      }
      await onSaved()
    } catch (e) {
      setErr(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={!!server} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>{isNew ? '添加 MCP 服务器' : `编辑 ${s?.name ?? ''}`}</DialogTitle>
            <DialogDescription className="text-[13px]">
              pichat 用 Streamable HTTP 连接这个服务器，把它的工具转给 agent。只支持 https 地址。
            </DialogDescription>
          </DialogHeader>
          {isNew && (
            <Field label="名称" hint="小写字母、数字、_ 和 -，最多 32 个字符，不能有连续两个 _，也不能以 _ 结尾。工具名会变成「名称__工具名」。">
              <Input
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value.toLowerCase())}
                maxLength={32}
                placeholder="github"
                aria-invalid={!!name && !nameOk}
                className="bg-background font-mono text-[12.5px]"
              />
            </Field>
          )}
          <Field label="地址">
            <Input
              autoFocus={!isNew}
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://mcp.example.com/mcp"
              aria-invalid={!!url && !urlOk}
              className="bg-background font-mono text-[12.5px]"
            />
          </Field>
          <Field label="认证请求头">
            <Input value={headerName} onChange={(e) => setHeaderName(e.target.value)} placeholder="Authorization" className="bg-background font-mono text-[12.5px]" />
          </Field>
          <Field label="请求头的值" hint={s?.keySet ? '留空则保留原来的值。' : '例如 Bearer xxx，加密保存，不会再显示出来。'}>
            <Input
              type="password"
              autoComplete="new-password"
              value={headerValue}
              onChange={(e) => setHeaderValue(e.target.value)}
              placeholder={s?.keySet ? hint(true, s.keyHint) : '可选'}
              className="bg-background"
            />
          </Field>
          {err && <p className="text-[12.5px] text-destructive">{err}</p>}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              取消
            </Button>
            <Button type="submit" disabled={busy || !nameOk || !urlOk}>
              {isNew ? '添加' : '保存'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
