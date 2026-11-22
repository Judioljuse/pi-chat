import { useEffect, useMemo, useState } from 'react'
import { BarChart3 } from 'lucide-react'
import { Empty, Page, Section } from '@/components/pichat/page'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { api } from '@/lib/api'
import { fail, useStore } from '@/lib/store'

interface Stat {
  runs: number
  failed: number
  durationMs: number
  inputTokens: number
  outputTokens: number
  cacheTokens: number
  costUsd: number
}
interface Usage {
  days: number
  total: Stat
  agents: (Stat & { id: string; name: string })[]
  groups: (Stat & { id: string; name: string })[]
  byDay: (Stat & { day: string })[]
  scope: 'mine' | 'all'
}

const num = (n: number) => (n >= 1e8 ? `${(n / 1e8).toFixed(1)} 亿` : n >= 1e4 ? `${(n / 1e4).toFixed(1)} 万` : String(Math.round(n)))
const dur = (ms: number) => {
  if (ms < 60000) return `${Math.round(ms / 1000)} 秒`
  const m = Math.round(ms / 60000)
  return m < 60 ? `${m} 分钟` : `${(m / 60).toFixed(1)} 小时`
}
const usd = (v: number) => (v > 0 ? `$${v < 1 ? v.toFixed(3) : v.toFixed(2)}` : '—')

function Tile({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5 rounded-control border border-border bg-card px-3 py-2.5">
      <span className="text-[12px] text-muted-foreground">{label}</span>
      <span className="text-[20px] font-medium text-foreground tabular-nums">{value}</span>
      {sub && <span className="text-[11.5px] text-muted-foreground">{sub}</span>}
    </div>
  )
}

/** Tokens per day as bars (input + output), with run counts under each day. */
function DayBars({ days }: { days: Usage['byDay'] }) {
  const max = Math.max(1, ...days.map((d) => d.inputTokens + d.outputTokens))
  return (
    <div className="flex h-36 gap-1.5" role="img" aria-label="每天的 token 用量">
      {days.map((d) => {
        const t = d.inputTokens + d.outputTokens
        return (
          <div key={d.day} className="flex min-w-0 flex-1 flex-col items-center gap-1" title={`${d.day}：${num(t)} token，${d.runs} 次运行`}>
            <div className="flex w-full flex-1 items-end">
              <div className="w-full rounded-t-[4px] bg-brand/70" style={{ height: `${Math.max(2, (t / max) * 100)}%` }} />
            </div>
            <span className="text-[10.5px] text-muted-foreground tabular-nums">{d.day.slice(5)}</span>
          </div>
        )
      })}
    </div>
  )
}

function Table({ rows, first }: { rows: (Stat & { id: string; name: string })[]; first: string }) {
  const sorted = [...rows].sort((a, b) => b.inputTokens + b.outputTokens - (a.inputTokens + a.outputTokens) || b.runs - a.runs)
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-[13px] whitespace-nowrap tabular-nums">
        <thead className="text-left text-[12px] text-muted-foreground">
          <tr>
            <th className="py-1.5 pr-3 font-normal">{first}</th>
            <th className="py-1.5 pr-3 text-right font-normal">运行</th>
            <th className="py-1.5 pr-3 text-right font-normal">失败</th>
            <th className="py-1.5 pr-3 text-right font-normal">时长</th>
            <th className="py-1.5 pr-3 text-right font-normal">输入 token</th>
            <th className="py-1.5 pr-3 text-right font-normal">输出 token</th>
            <th className="py-1.5 text-right font-normal">费用</th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((r) => (
            <tr key={r.id || r.name} className="border-t border-border">
              <td className="max-w-48 truncate py-1.5 pr-3 text-foreground">{r.name || '（无群）'}</td>
              <td className="py-1.5 pr-3 text-right">{r.runs}</td>
              <td className="py-1.5 pr-3 text-right">{r.failed || '—'}</td>
              <td className="py-1.5 pr-3 text-right">{dur(r.durationMs)}</td>
              <td className="py-1.5 pr-3 text-right">{num(r.inputTokens + r.cacheTokens)}</td>
              <td className="py-1.5 pr-3 text-right">{num(r.outputTokens)}</td>
              <td className="py-1.5 text-right">{usd(r.costUsd)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/** #/usage: runs, time and tokens of my agents (admins can switch to all agents) — v0.5.0. */
export function Usage() {
  const isAdmin = useStore((s) => !!s.me?.isAdmin)
  const [days, setDays] = useState('7')
  const [scope, setScope] = useState<'mine' | 'all'>('mine')
  const [u, setU] = useState<Usage | null>(null)
  useEffect(() => {
    api<Usage>('GET', `/usage?days=${days}&scope=${scope}`).then(setU, fail)
  }, [days, scope])
  const t = u?.total
  const daysFilled = useMemo(() => {
    if (!u) return []
    const by = new Map(u.byDay.map((d) => [d.day, d]))
    const out: Usage['byDay'] = []
    for (let i = u.days - 1; i >= 0; i--) {
      const d = new Date(Date.now() - i * 864e5)
      const k = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
      out.push(by.get(k) ?? { day: k, runs: 0, failed: 0, durationMs: 0, inputTokens: 0, outputTokens: 0, cacheTokens: 0, costUsd: 0 })
    }
    return out
  }, [u])

  return (
    <Page
      title="用量"
      actions={
        <div className="flex items-center gap-2">
          {isAdmin && (
            <ToggleGroup type="single" spacing={0} value={scope} onValueChange={(v) => v && setScope(v as 'mine' | 'all')} className="h-8 gap-0.5 rounded-control bg-accent p-0.5">
              <ToggleGroupItem value="mine" className="h-7 rounded-[6px]! px-3 text-[13px] data-[state=on]:bg-card">我的 agent</ToggleGroupItem>
              <ToggleGroupItem value="all" className="h-7 rounded-[6px]! px-3 text-[13px] data-[state=on]:bg-card">全部</ToggleGroupItem>
            </ToggleGroup>
          )}
          <Select value={days} onValueChange={setDays}>
            <SelectTrigger className="h-8 w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="1">今天</SelectItem>
              <SelectItem value="7">最近 7 天</SelectItem>
              <SelectItem value="30">最近 30 天</SelectItem>
            </SelectContent>
          </Select>
        </div>
      }
    >
      {!u || !t ? null : t.runs === 0 ? (
        <Empty icon={<BarChart3 />} title="这段时间没有运行记录">
          agent 每次被叫醒运行后，listener 会上报时长和 token。listener 0.5.0 以上才上报 token。
        </Empty>
      ) : (
        <div className="flex flex-col gap-4">
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <Tile label="运行次数" value={String(t.runs)} sub={t.failed ? `失败 ${t.failed} 次` : '全部成功'} />
            <Tile label="运行时长" value={dur(t.durationMs)} />
            <Tile label="token" value={num(t.inputTokens + t.cacheTokens + t.outputTokens)} sub={`输出 ${num(t.outputTokens)}，缓存 ${num(t.cacheTokens)}`} />
            <Tile label="费用（估算）" value={usd(t.costUsd)} sub="按模型官方价格估算，不是网关实际账单" />
          </div>
          {u.days > 1 && (
            <Section title="每天的 token">
              <DayBars days={daysFilled} />
            </Section>
          )}
          <Section title="按 agent">
            <Table rows={u.agents} first="agent" />
          </Section>
          <Section title="按群">
            <Table rows={u.groups} first="群" />
          </Section>
          <p className="text-[12px] text-muted-foreground">
            数据来自各台机器的 listener 上报：Claude 和 Codex 有 token，pi 视模型而定；终端模式下在终端里处理的消息不计入。
          </p>
        </div>
      )}
    </Page>
  )
}
