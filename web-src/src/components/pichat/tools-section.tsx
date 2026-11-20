import { useEffect, useState } from 'react'
import { Section } from '@/components/pichat/page'
import { Skeleton } from '@/components/ui/skeleton'
import { api, errorText } from '@/lib/api'

type ToolsInfo = {
  mcp: string
  available: string[]
  searchProvider: string
  searchError?: string
  limits: { searchesPerAgentPerHour: number; fetchesPerAgentPerHour: number }
}

const PROVIDER: Record<string, string> = {
  duckduckgo: 'DuckDuckGo（免 key）',
  brave: 'Brave Search',
  tavily: 'Tavily',
  bocha: '博查',
  searxng: 'SearXNG',
}

/** Admin page section: read-only view of the server-side agent tools (MCP). */
export function ToolsSection() {
  const [info, setInfo] = useState<ToolsInfo | null>(null)
  const [err, setErr] = useState<unknown>(null)

  useEffect(() => {
    api<{ tools?: ToolsInfo }>('GET', '').then(
      (r) => (r.tools ? setInfo(r.tools) : setErr(new Error('服务器没有返回工具信息'))),
      setErr,
    )
  }, [])

  const kv = (label: string, value: React.ReactNode) => (
    <div className="flex min-w-0 items-center gap-3 py-1.5 text-[13px]">
      <span className="w-20 shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 flex-1 truncate text-body">{value}</span>
    </div>
  )

  return (
    <Section
      title="Agent 工具"
      description="所有 agent 都能通过服务器的 MCP 用这些只读工具。搜索服务用环境变量 PICHAT_SEARCH_PROVIDER、PICHAT_SEARCH_KEY、PICHAT_SEARCH_URL 配置，改了要重启服务。"
    >
      {err ? (
        <p className="text-[13px] text-destructive">{errorText(err)}</p>
      ) : !info ? (
        <Skeleton className="h-5 w-64" />
      ) : (
        <div>
          {kv('工具', <span className="font-mono text-[12.5px]">{info.available.join('、')}</span>)}
          {kv(
            '搜索服务',
            info.searchProvider ? (
              PROVIDER[info.searchProvider] || info.searchProvider
            ) : (
              <span className="text-destructive">未生效：{info.searchError || '配置有误'}</span>
            ),
          )}
          {kv(
            '频率限制',
            <span>
              每个 agent 每小时搜索 <span className="font-mono tabular-nums">{info.limits.searchesPerAgentPerHour}</span> 次、读网页{' '}
              <span className="font-mono tabular-nums">{info.limits.fetchesPerAgentPerHour}</span> 次
            </span>,
          )}
          {kv('MCP 地址', <span className="font-mono text-[12.5px]">{info.mcp}</span>)}
        </div>
      )}
    </Section>
  )
}
